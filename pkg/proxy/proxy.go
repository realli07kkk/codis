// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"context"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/redis"
	"github.com/CodisLabs/codis/pkg/utils/rpc"
	"github.com/CodisLabs/codis/pkg/utils/unsafe2"
)

type Proxy struct {
	mu sync.Mutex

	xauth string
	model *models.Proxy

	exit struct {
		C chan struct{}
	}
	online bool
	closed bool

	// ctx is the root context of the proxy. Close cancels it before it closes
	// the listeners, so every background task can observe shutdown without
	// polling. exit.C is closed at the same point and kept for select callers.
	ctx       context.Context
	cancel    context.CancelFunc
	bg        sync.WaitGroup
	closeDone chan struct{}
	releases  []func()

	config         *Config
	router         *Router
	ignore         []byte
	authBruteforce *AuthBruteforceGuard

	lproxy net.Listener
	ladmin net.Listener

	ha struct {
		monitor *redis.Sentinel
		masters map[int]string
		servers []string
	}
	jodis *Jodis
}

var ErrClosedProxy = errors.New("use of closed proxy")

func New(config *Config) (*Proxy, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	if err := models.ValidateProduct(config.ProductName); err != nil {
		return nil, errors.Trace(err)
	}

	s := &Proxy{}
	s.config = config
	s.exit.C = make(chan struct{})
	s.closeDone = make(chan struct{})
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.router = NewRouter(config)
	s.ignore = make([]byte, config.ProxyHeapPlaceholder.Int64())
	s.authBruteforce = newAuthBruteforceGuard(config)

	s.model = &models.Proxy{
		StartTime: time.Now().String(),
	}
	s.model.ProductName = config.ProductName
	s.model.DataCenter = config.ProxyDataCenter
	s.model.Pid = os.Getpid()
	s.model.Pwd, _ = os.Getwd()
	if b, err := exec.Command("uname", "-a").Output(); err != nil {
		log.WarnErrorf(err, "run command uname failed")
	} else {
		s.model.Sys = strings.TrimSpace(string(b))
	}
	s.model.Hostname = utils.Hostname

	if err := s.setup(config); err != nil {
		s.Close()
		return nil, err
	}

	log.Warnf("[%p] create new proxy:\n%s", s, s.model.Encode())

	unsafe2.SetMaxOffheapBytes(config.ProxyMaxOffheapBytes.Int64())

	s.releases = append(s.releases,
		startStatsReporter(),
		startKeepAliveCallbacks(),
	)

	go s.serveAdmin()
	go s.serveProxy()

	s.startMetricsJson()
	s.startMetricsInfluxdb()
	s.startMetricsStatsd()

	return s, nil
}

func (s *Proxy) setup(config *Config) error {
	proto := config.ProtoType
	if l, err := net.Listen(proto, config.ProxyAddr); err != nil {
		return errors.Trace(err)
	} else {
		s.lproxy = l

		x, err := utils.ReplaceUnspecifiedIP(proto, l.Addr().String(), config.HostProxy)
		if err != nil {
			return err
		}
		s.model.ProtoType = proto
		s.model.ProxyAddr = x
	}

	proto = "tcp"
	if l, err := net.Listen(proto, config.AdminAddr); err != nil {
		return errors.Trace(err)
	} else {
		s.ladmin = l

		x, err := utils.ReplaceUnspecifiedIP(proto, l.Addr().String(), config.HostAdmin)
		if err != nil {
			return err
		}
		s.model.AdminAddr = x
	}

	s.model.Token = rpc.NewToken(
		config.ProductName,
		s.lproxy.Addr().String(),
		s.ladmin.Addr().String(),
	)
	s.xauth = rpc.NewXAuth(
		config.ProductName,
		config.ProductAuth,
		s.model.Token,
	)
	if s.router != nil && s.router.hotKeyCacheBroadcast != nil {
		s.router.hotKeyCacheBroadcast.SetSource(s.model.Token, rpc.NewXAuth(config.ProductName))
	}

	if config.JodisAddr != "" {
		c, err := models.NewClient(config.JodisName, config.JodisAddr, config.JodisAuth, config.JodisTimeout.Duration())
		if err != nil {
			return err
		}
		if config.JodisCompatible {
			s.model.JodisPath = filepath.Join("/zk/codis", fmt.Sprintf("db_%s", config.ProductName), "proxy", s.model.Token)
		} else {
			s.model.JodisPath = models.JodisPath(config.ProductName, s.model.Token)
		}
		s.jodis = NewJodis(c, s.model)
	}
	if p, err := newClusterNodesProviderForProxy(config, s.model); err != nil {
		return err
	} else {
		s.router.clusterNodes = p
	}

	return nil
}

func (s *Proxy) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosedProxy
	}
	if s.online {
		return nil
	}
	s.online = true
	s.router.Start()
	if s.jodis != nil {
		s.jodis.Start()
	}
	return nil
}

// Close shuts the proxy down. It is idempotent and safe to call from an API
// handler (the admin server calls it) or from a serving goroutine: it never
// waits for the callers of Close. The order is
//
//  1. mark closed and cancel the root context / close exit.C,
//  2. close the listeners and the router so blocked accept/read users wake up,
//  3. wait for the registered background tasks and release the process-wide
//     workers.
//
// Step 3 only waits for tasks with a definite exit (tickers, accept loop,
// sentinel watchers, metrics reporters); no task is allowed to call Close.
func (s *Proxy) Close() error {
	s.mu.Lock()
	if s.closed {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return nil
	}
	s.closed = true
	// Wake concurrent and future Close callers even if a downstream Close
	// hook panics and the panic is recovered by an HTTP or serving boundary.
	defer close(s.closeDone)
	if s.cancel != nil {
		s.cancel()
	}
	close(s.exit.C)

	if s.jodis != nil {
		s.jodis.Close()
	}
	if s.ladmin != nil {
		s.ladmin.Close()
	}
	if s.lproxy != nil {
		s.lproxy.Close()
	}
	if s.router != nil {
		s.router.Close()
	}
	if s.ha.monitor != nil {
		s.ha.monitor.Cancel()
		s.ha.monitor = nil
	}
	s.mu.Unlock()

	s.bg.Wait()

	for _, release := range s.releases {
		release()
	}
	return nil
}

// startBackground registers a background task with the proxy lifecycle. The
// task must return once Close cancels the root context and closes the
// listeners; Close waits for every registered task, so a task must never need
// Close to make progress. Registering after Close is a no-op, which keeps
// sync.WaitGroup free of Add-during-Wait misuse.
func (s *Proxy) startBackground(scope string, fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startBackgroundLocked(scope, fn)
}

// startBackgroundLocked is startBackground for callers that already hold s.mu.
func (s *Proxy) startBackgroundLocked(scope string, fn func()) bool {
	if s.closed {
		return false
	}
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		// A recovered panic ends this task for good: the panic is logged with
		// its stack and the task is not restarted behind the owner's back.
		defer utils.Recover(scope)
		fn()
	}()
	return true
}

// sleepFor waits for d, returning false if the proxy starts shutting down
// first. It replaces time.Sleep in backoff paths so Close does not have to wait
// for a full backoff window. exit.C is closed together with the root context.
func (s *Proxy) sleepFor(d time.Duration) bool {
	if d <= 0 {
		return !s.IsClosed()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.exit.C:
		return false
	case <-timer.C:
		return true
	}
}

func (s *Proxy) XAuth() string {
	return s.xauth
}

func (s *Proxy) Model() *models.Proxy {
	return s.model
}

func (s *Proxy) SetQPSLimit(config *QPSLimitConfig) error {
	if config == nil {
		return errors.New("missing qps limit config")
	}
	return s.router.qpsLimiter.SetLimit(config.Revision, config.Limit, time.Now())
}

func (s *Proxy) ResetStats() {
	ResetStats()
	if s.router != nil {
		s.router.qpsLimiter.ResetStats()
	}
}

func (s *Proxy) Config() *Config {
	return s.config
}

func (s *Proxy) SetTopomAdminAddr(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.router != nil && s.router.hotKeyCacheBroadcast != nil {
		s.router.hotKeyCacheBroadcast.SetTopomAdminAddr(addr)
	}
}

func (s *Proxy) InvalidateHotKeyCache(database int32, keys [][]byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosedProxy
	}
	return s.router.hotKeyCacheInvalidateRemote(database, keys), nil
}

func (s *Proxy) IsOnline() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.online && !s.closed
}

func (s *Proxy) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Done returns a channel that is closed when the proxy starts shutting down.
func (s *Proxy) Done() <-chan struct{} {
	return s.exit.C
}

func (s *Proxy) HasSwitched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.router.HasSwitched()
}

func (s *Proxy) Slots() []*models.Slot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.router.GetSlots()
}

func (s *Proxy) FillSlot(m *models.Slot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosedProxy
	}
	return s.router.FillSlot(m)
}

func (s *Proxy) FillSlots(slots []*models.Slot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosedProxy
	}
	for _, m := range slots {
		if err := s.router.FillSlot(m); err != nil {
			return err
		}
	}
	return nil
}

func (s *Proxy) SwitchMasters(masters map[int]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosedProxy
	}
	s.ha.masters = masters

	if len(masters) != 0 {
		s.router.SwitchMasters(masters)
	}
	return nil
}

func (s *Proxy) GetSentinels() ([]string, map[int]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil
	}
	return s.ha.servers, s.ha.masters
}

func (s *Proxy) SetSentinels(servers []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosedProxy
	}
	s.ha.servers = servers
	log.Warnf("[%p] set sentinels = %v", s, s.ha.servers)

	s.rewatchSentinels(s.ha.servers)
	return nil
}

func (s *Proxy) RewatchSentinels() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosedProxy
	}
	log.Warnf("[%p] rewatch sentinels = %v", s, s.ha.servers)

	s.rewatchSentinels(s.ha.servers)
	return nil
}

// rewatchSentinels replaces the sentinel monitor and starts one registered
// watcher task for it. Callers must hold s.mu; the previous monitor is canceled
// so its watcher observes a definite exit.
func (s *Proxy) rewatchSentinels(servers []string) {
	if s.ha.monitor != nil {
		s.ha.monitor.Cancel()
		s.ha.monitor = nil
		s.ha.masters = nil
	}
	if len(servers) != 0 {
		s.ha.monitor = redis.NewSentinel(s.config.ProductName, s.config.ProductAuth)
		s.ha.monitor.LogFunc = log.Warnf
		s.ha.monitor.ErrFunc = log.WarnErrorf
		monitor := s.ha.monitor
		s.startBackgroundLocked("sentinel watcher", func() {
			s.watchSentinels(monitor, servers)
		})
	}
}

// watchSentinels keeps the sentinel subscription and the master fetch on two
// goroutines so a slow fetch cannot delay the subscription, and joins them
// before returning: the registered task only completes when both are gone.
func (s *Proxy) watchSentinels(p *redis.Sentinel, servers []string) {
	var wg sync.WaitGroup
	var trigger = make(chan struct{}, 1)

	delayUntil := func(deadline time.Time) bool {
		for !p.IsCanceled() {
			var d = time.Until(deadline)
			if d <= 0 {
				return true
			}
			if !s.sleepFor(min(d, time.Second)) {
				return false
			}
		}
		return false
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// A panic here must not leave the master-fetch goroutine waiting on a
		// trigger that nobody closes.
		defer utils.Recover("sentinel subscribe watcher")
		defer close(trigger)
		callback := func() {
			select {
			case trigger <- struct{}{}:
			default:
			}
		}
		for !p.IsCanceled() && !s.IsClosed() {
			timeout := time.Minute * 15
			retryAt := time.Now().Add(time.Second * 10)
			if !p.Subscribe(servers, timeout, callback) {
				if !delayUntil(retryAt) {
					return
				}
			} else {
				callback()
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer utils.Recover("sentinel masters watcher")
		for range trigger {
			var success int
			for i := 0; i != 10 && !p.IsCanceled() && success != 2; i++ {
				timeout := time.Second * 5
				masters, err := p.Masters(servers, timeout)
				if err != nil {
					log.WarnErrorf(err, "[%p] fetch group masters failed", s)
				} else {
					if !p.IsCanceled() {
						s.SwitchMasters(masters)
					}
					success += 1
				}
				if !delayUntil(time.Now().Add(time.Second * 5)) {
					return
				}
			}
		}
	}()

	wg.Wait()
}

// serveAdmin owns the admin HTTP server and shuts the proxy down if it fails.
// It is not part of the proxy WaitGroup on purpose: the API "shutdown" handler
// calls Close from inside this server, so Close must never wait for it.
func (s *Proxy) serveAdmin() {
	if s.IsClosed() {
		return
	}
	defer s.Close()
	defer utils.Recover("proxy admin server")

	log.Warnf("[%p] admin start service on %s", s, s.ladmin.Addr())

	eh := make(chan error, 1)
	h := http.NewServeMux()
	h.Handle("/", newApiServer(s))
	hs := utils.NewAdminHTTPServer(h)
	go func(l net.Listener) {
		var err error
		defer func() {
			eh <- err
		}()
		defer utils.RecoverAsError("proxy admin http server", &err)
		err = hs.Serve(l)
	}(s.ladmin)

	select {
	case <-s.ctx.Done():
		utils.ShutdownHTTPServer(hs, 5*time.Second)
		log.Warnf("[%p] admin shutdown", s)
	case err := <-eh:
		if !s.IsClosed() {
			log.ErrorErrorf(err, "[%p] admin exit on error", s)
		}
	}
}

// serveProxy owns the client listener and shuts the proxy down if it fails.
// It is not part of the proxy WaitGroup for the same reason as serveAdmin.
func (s *Proxy) serveProxy() {
	if s.IsClosed() {
		return
	}
	defer s.Close()
	defer utils.Recover("proxy server")

	log.Warnf("[%p] proxy start service on %s", s, s.lproxy.Addr())

	eh := make(chan error, 1)
	s.startBackground("proxy accept loop", func() {
		eh <- s.acceptLoop(s.lproxy)
	})

	if d := s.config.BackendPingPeriod.Duration(); d != 0 {
		s.startBackground("backend keepalive", func() {
			s.keepAlive(d)
		})
	}

	select {
	case <-s.ctx.Done():
		log.Warnf("[%p] proxy shutdown", s)
	case err := <-eh:
		if !s.IsClosed() {
			log.ErrorErrorf(err, "[%p] proxy exit on error", s)
		}
	}
}

// acceptLoop accepts client connections until the listener is closed or fails
// with a fatal error. A panic is converted into an error so the owning
// serveProxy shuts the proxy down instead of leaving a listener that looks
// healthy but accepts nothing.
func (s *Proxy) acceptLoop(l net.Listener) (err error) {
	defer utils.RecoverAsError("proxy accept loop", &err)
	for {
		c, err := s.acceptConn(l)
		if err != nil {
			return err
		}
		NewSessionWithAuthBruteforceGuard(c, s.config, s.authBruteforce).Start(s.router)
	}
}

func (s *Proxy) keepAlive(d time.Duration) {
	var ticker = time.NewTicker(max(d, time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.router.KeepAlive()
		}
	}
}

// acceptErrorRetryable classifies accept failures without the deprecated
// net.Error.Temporary. A closed listener (or a canceled proxy) is fatal so
// serveProxy can exit; resource exhaustion, interrupted accepts, accept
// timeouts and connections aborted before accept completed are transient and
// retried with backoff.
func acceptErrorRetryable(err error) bool {
	switch {
	case err == nil:
		return false
	case stderrors.Is(err, net.ErrClosed), stderrors.Is(err, context.Canceled):
		return false
	case stderrors.Is(err, syscall.EINTR):
		return true
	case stderrors.Is(err, syscall.EMFILE), stderrors.Is(err, syscall.ENFILE),
		stderrors.Is(err, syscall.ENOBUFS), stderrors.Is(err, syscall.ENOMEM):
		return true
	case stderrors.Is(err, syscall.ECONNRESET), stderrors.Is(err, syscall.ECONNABORTED):
		// net.OpError has always reported these as temporary for accept.
		return true
	}
	var ne net.Error
	if stderrors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

func (s *Proxy) acceptConn(l net.Listener) (net.Conn, error) {
	var delay = &DelayExp2{
		Min: 10, Max: 500,
		Unit: time.Millisecond,
	}
	for {
		c, err := l.Accept()
		if err == nil {
			return c, nil
		}
		if !acceptErrorRetryable(err) || s.IsClosed() {
			return nil, err
		}
		log.WarnErrorf(err, "[%p] proxy accept new connection failed", s)
		if !s.sleepFor(time.Duration(delay.NextValue()) * delay.Unit) {
			return nil, err
		}
	}
}

type Overview struct {
	Version string         `json:"version"`
	Compile string         `json:"compile"`
	Config  *Config        `json:"config,omitempty"`
	Model   *models.Proxy  `json:"model,omitempty"`
	Stats   *Stats         `json:"stats,omitempty"`
	Slots   []*models.Slot `json:"slots,omitempty"`
}

type Stats struct {
	Online bool `json:"online"`
	Closed bool `json:"closed"`

	Sentinels struct {
		Servers  []string          `json:"servers,omitempty"`
		Masters  map[string]string `json:"masters,omitempty"`
		Switched bool              `json:"switched,omitempty"`
	} `json:"sentinels"`

	Ops struct {
		Total int64 `json:"total"`
		Fails int64 `json:"fails"`
		Redis struct {
			Errors int64 `json:"errors"`
		} `json:"redis"`
		QPS int64      `json:"qps"`
		Cmd []*OpStats `json:"cmd,omitempty"`
	} `json:"ops"`

	Sessions struct {
		Total int64 `json:"total"`
		Alive int64 `json:"alive"`
	} `json:"sessions"`

	Rusage struct {
		Now string       `json:"now"`
		CPU float64      `json:"cpu"`
		Mem int64        `json:"mem"`
		Raw *utils.Usage `json:"raw,omitempty"`
	} `json:"rusage"`

	Backend struct {
		PrimaryOnly bool `json:"primary_only"`
	} `json:"backend"`

	HotKeyCache           *HotKeyCacheStats    `json:"hot_key_cache,omitempty"`
	SessionAuthBruteforce *AuthBruteforceStats `json:"session_auth_bruteforce,omitempty"`
	QPSLimit              *QPSLimitStats       `json:"qps_limit,omitempty"`

	Runtime *RuntimeStats `json:"runtime,omitempty"`
}

type RuntimeStats struct {
	General struct {
		Alloc   uint64 `json:"alloc"`
		Sys     uint64 `json:"sys"`
		Lookups uint64 `json:"lookups"`
		Mallocs uint64 `json:"mallocs"`
		Frees   uint64 `json:"frees"`
	} `json:"general"`

	Heap struct {
		Alloc   uint64 `json:"alloc"`
		Sys     uint64 `json:"sys"`
		Idle    uint64 `json:"idle"`
		Inuse   uint64 `json:"inuse"`
		Objects uint64 `json:"objects"`
	} `json:"heap"`

	GC struct {
		Num          uint32  `json:"num"`
		CPUFraction  float64 `json:"cpu_fraction"`
		TotalPauseMs uint64  `json:"total_pausems"`
	} `json:"gc"`

	NumProcs      int   `json:"num_procs"`
	NumGoroutines int   `json:"num_goroutines"`
	NumCgoCall    int64 `json:"num_cgo_call"`
	MemOffheap    int64 `json:"mem_offheap"`
}

type StatsFlags uint32

func (s StatsFlags) HasBit(m StatsFlags) bool {
	return (s & m) != 0
}

const (
	StatsCmds = StatsFlags(1 << iota)
	StatsSlots
	StatsRuntime

	StatsFull = StatsFlags(^uint32(0))
)

func (s *Proxy) Overview(flags StatsFlags) *Overview {
	o := &Overview{
		Version: utils.Version,
		Compile: utils.Compile,
		Config:  s.Config().Redacted(),
		Model:   s.Model(),
		Stats:   s.Stats(flags),
	}
	if flags.HasBit(StatsSlots) {
		o.Slots = s.Slots()
	}
	return o
}

func (s *Proxy) Stats(flags StatsFlags) *Stats {
	stats := &Stats{}
	stats.Online = s.IsOnline()
	stats.Closed = s.IsClosed()

	servers, masters := s.GetSentinels()
	if servers != nil {
		stats.Sentinels.Servers = servers
	}
	if masters != nil {
		stats.Sentinels.Masters = make(map[string]string)
		for gid, addr := range masters {
			stats.Sentinels.Masters[strconv.Itoa(gid)] = addr
		}
	}
	stats.Sentinels.Switched = s.HasSwitched()

	stats.Ops.Total = OpTotal()
	stats.Ops.Fails = OpFails()
	stats.Ops.Redis.Errors = OpRedisErrors()
	stats.Ops.QPS = OpQPS()

	if flags.HasBit(StatsCmds) {
		stats.Ops.Cmd = GetOpStatsAll()
	}

	stats.Sessions.Total = SessionsTotal()
	stats.Sessions.Alive = SessionsAlive()

	if u := GetSysUsage(); u != nil {
		stats.Rusage.Now = u.Now.String()
		stats.Rusage.CPU = u.CPU
		stats.Rusage.Mem = u.MemTotal()
		stats.Rusage.Raw = u.Usage
	}

	stats.Backend.PrimaryOnly = s.Config().BackendPrimaryOnly
	if hotKeyCache := s.router.hotKeyCache.Stats(); hotKeyCache.Visible() {
		stats.HotKeyCache = &hotKeyCache
	}
	if authBruteforce := s.authBruteforce.Stats(); authBruteforce.Visible() {
		stats.SessionAuthBruteforce = &authBruteforce
	}
	if qpsLimit := s.router.qpsLimiter.Stats(); qpsLimit.Visible() {
		stats.QPSLimit = &qpsLimit
	}

	if flags.HasBit(StatsRuntime) {
		var r runtime.MemStats
		runtime.ReadMemStats(&r)

		stats.Runtime = &RuntimeStats{}
		stats.Runtime.General.Alloc = r.Alloc
		stats.Runtime.General.Sys = r.Sys
		stats.Runtime.General.Lookups = r.Lookups
		stats.Runtime.General.Mallocs = r.Mallocs
		stats.Runtime.General.Frees = r.Frees
		stats.Runtime.Heap.Alloc = r.HeapAlloc
		stats.Runtime.Heap.Sys = r.HeapSys
		stats.Runtime.Heap.Idle = r.HeapIdle
		stats.Runtime.Heap.Inuse = r.HeapInuse
		stats.Runtime.Heap.Objects = r.HeapObjects
		stats.Runtime.GC.Num = r.NumGC
		stats.Runtime.GC.CPUFraction = r.GCCPUFraction
		stats.Runtime.GC.TotalPauseMs = r.PauseTotalNs / uint64(time.Millisecond)
		stats.Runtime.NumProcs = runtime.GOMAXPROCS(0)
		stats.Runtime.NumGoroutines = runtime.NumGoroutine()
		stats.Runtime.NumCgoCall = runtime.NumCgoCall()
		stats.Runtime.MemOffheap = unsafe2.OffheapBytes()
	}
	return stats
}
