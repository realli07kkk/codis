// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	stderrors "errors"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/sync2/atomic2"
)

type opStats struct {
	opstr string
	calls atomic2.Int64
	nsecs atomic2.Int64
	fails atomic2.Int64
	redis struct {
		errors atomic2.Int64
	}
}

func (s *opStats) OpStats() *OpStats {
	o := &OpStats{
		OpStr: s.opstr,
		Calls: s.calls.Int64(),
		Usecs: s.nsecs.Int64() / 1e3,
		Fails: s.fails.Int64(),
	}
	if o.Calls != 0 {
		o.UsecsPercall = o.Usecs / o.Calls
	}
	o.RedisErrType = s.redis.errors.Int64()
	return o
}

type OpStats struct {
	OpStr        string `json:"opstr"`
	Calls        int64  `json:"calls"`
	Usecs        int64  `json:"usecs"`
	UsecsPercall int64  `json:"usecs_percall"`
	Fails        int64  `json:"fails"`
	RedisErrType int64  `json:"redis_errtype"`
}

var cmdstats = struct {
	sync.RWMutex

	opmap map[string]*opStats
	total atomic2.Int64
	fails atomic2.Int64
	redis struct {
		errors atomic2.Int64
	}

	qps atomic2.Int64
}{
	opmap: make(map[string]*opStats, 128),
}

var errStatsReporterStopped = stderrors.New("stats reporter stopped")

// statsReporter owns the two process-wide sampling loops behind OpQPS and
// GetSysUsage. Both loops are explicit tasks with a stop channel so a Proxy can
// shut them down and tests can drive an isolated reporter.
type statsReporter struct {
	qpsPeriod time.Duration
	sysSample time.Duration
	sysRetry  time.Duration

	mu   sync.Mutex
	stop chan struct{}
	wg   sync.WaitGroup
}

func newStatsReporter() *statsReporter {
	return &statsReporter{
		qpsPeriod: time.Second,
		sysSample: time.Second,
		sysRetry:  5 * time.Second,
	}
}

// Start launches the sampling loops. It is a no-op while they are running, so
// a shared reporter can be started by several owners.
func (r *statsReporter) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop != nil {
		return
	}
	stop := make(chan struct{})
	r.stop = stop

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer utils.Recover("proxy op qps reporter")
		r.loopQPS(stop)
	}()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer utils.Recover("proxy sys usage reporter")
		r.loopSysUsage(stop)
	}()
}

// Close stops the sampling loops and waits for them to exit. It is safe to call
// without Start and safe to call more than once. The mutex is held across stop
// and wait so a concurrent Start cannot attach new loops to the WaitGroup this
// Close is waiting on.
func (r *statsReporter) Close() {
	r.mu.Lock()
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
		r.wg.Wait()
	}
	r.mu.Unlock()
}

func (r *statsReporter) running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stop != nil
}

// loopQPS samples cmdstats.total on its own ticker instead of sleeping in a
// bare for loop, so Close wakes it immediately.
func (r *statsReporter) loopQPS(stop <-chan struct{}) {
	var ticker = time.NewTicker(r.qpsPeriod)
	defer ticker.Stop()

	var last = time.Now()
	var total = cmdstats.total.Int64()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			cur := cmdstats.total.Int64()
			delta := cur - total
			normalized := math.Max(0, float64(delta)) * float64(time.Second) / float64(now.Sub(last))
			cmdstats.qps.Set(int64(normalized + 0.5))
			last, total = now, cur
		}
	}
}

func (r *statsReporter) loopSysUsage(stop <-chan struct{}) {
	for {
		err := r.sampleSysUsage(stop)
		switch {
		case err == errStatsReporterStopped:
			return
		case err != nil:
			select {
			case <-stop:
				return
			case <-time.After(r.sysRetry):
			}
		}
	}
}

// sampleSysUsage mirrors utils.CPUUsage but waits for the sample window with a
// timer that Close can interrupt. It returns errStatsReporterStopped when the
// reporter is shutting down.
func (r *statsReporter) sampleSysUsage(stop <-chan struct{}) error {
	start := time.Now()

	b, err := utils.GetUsage()
	if err != nil {
		lastSysUsage.Store(&SysUsage{Now: time.Now()})
		return err
	}

	timer := time.NewTimer(r.sysSample)
	defer timer.Stop()
	select {
	case <-stop:
		return errStatsReporterStopped
	case <-timer.C:
	}

	e, err := utils.GetUsage()
	if err != nil {
		lastSysUsage.Store(&SysUsage{Now: time.Now()})
		return err
	}

	usage := e.CPUTotal() - b.CPUTotal()
	lastSysUsage.Store(&SysUsage{
		Now:   time.Now(),
		CPU:   float64(usage) / float64(time.Since(start)),
		Usage: e,
	})
	return nil
}

var opStatsReporter = newSharedWorker(newStatsReporter(),
	(*statsReporter).Start, (*statsReporter).Close)

// startStatsReporter starts the process-wide stats loops if needed and returns
// the release hook for the caller (a Proxy) lifecycle.
func startStatsReporter() func() {
	return opStatsReporter.acquire()
}

func OpTotal() int64 {
	return cmdstats.total.Int64()
}

func OpFails() int64 {
	return cmdstats.fails.Int64()
}

func OpRedisErrors() int64 {
	return cmdstats.redis.errors.Int64()
}

func OpQPS() int64 {
	return cmdstats.qps.Int64()
}

func getOpStats(opstr string, create bool) *opStats {
	cmdstats.RLock()
	s := cmdstats.opmap[opstr]
	cmdstats.RUnlock()

	if s != nil || !create {
		return s
	}

	cmdstats.Lock()
	s = cmdstats.opmap[opstr]
	if s == nil {
		s = &opStats{opstr: opstr}
		cmdstats.opmap[opstr] = s
	}
	cmdstats.Unlock()
	return s
}

// sortOpStats orders per-command snapshots by operation name so the stats API
// returns them in a deterministic order.
func sortOpStats(all []*OpStats) {
	slices.SortFunc(all, func(a, b *OpStats) int {
		return strings.Compare(a.OpStr, b.OpStr)
	})
}

func GetOpStatsAll() []*OpStats {
	var all = make([]*OpStats, 0, 128)
	cmdstats.RLock()
	for _, s := range cmdstats.opmap {
		all = append(all, s.OpStats())
	}
	cmdstats.RUnlock()
	sortOpStats(all)
	return all
}

func ResetStats() {
	cmdstats.Lock()
	cmdstats.opmap = make(map[string]*opStats, 128)
	cmdstats.Unlock()

	cmdstats.total.Set(0)
	cmdstats.fails.Set(0)
	cmdstats.redis.errors.Set(0)
	sessions.total.Set(SessionsAlive())
}

func incrOpTotal(n int64) {
	cmdstats.total.Add(n)
}

func incrOpFails(n int64) {
	cmdstats.fails.Add(n)
}

func incrOpStats(e *opStats) {
	s := getOpStats(e.opstr, true)
	s.calls.Add(e.calls.Swap(0))
	s.nsecs.Add(e.nsecs.Swap(0))
	if n := e.fails.Swap(0); n != 0 {
		s.fails.Add(n)
		cmdstats.fails.Add(n)
	}
	if n := e.redis.errors.Swap(0); n != 0 {
		s.redis.errors.Add(n)
		cmdstats.redis.errors.Add(n)
	}
}

var sessions struct {
	total atomic2.Int64
}

func incrSessions() {
	sessions.total.Incr()
}

func SessionsTotal() int64 {
	return sessions.total.Int64()
}

func SessionsAlive() int64 {
	return clientSessions.count()
}

type SysUsage struct {
	Now time.Time
	CPU float64
	*utils.Usage
}

var lastSysUsage atomic.Value

func GetSysUsage() *SysUsage {
	if p := lastSysUsage.Load(); p != nil {
		return p.(*SysUsage)
	}
	return nil
}
