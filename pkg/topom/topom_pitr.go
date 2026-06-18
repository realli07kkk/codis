// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	stdcontext "context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	redisutils "github.com/CodisLabs/codis/pkg/utils/redis"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// PITR job states. The order reflects the happy-path progression of the state
// machine in PitrManager.run; failed / cancelled are terminal states reachable
// from any step.
const (
	PitrStatePending             = "pending"
	PitrStateValidating          = "validating"
	PitrStateCheckingPrereq      = "checking_prereq"
	PitrStateCheckingFeasibility = "checking_feasibility"
	PitrStateShuttingDown        = "shutting_down"
	PitrStateSnapshotting        = "snapshotting"
	PitrStateTruncating          = "truncating"
	PitrStateRestarting          = "restarting"
	PitrStateWaitingLoad         = "waiting_load"
	PitrStateResyncingReplicas   = "resyncing_replicas"
	PitrStateSucceeded           = "succeeded"
	PitrStateFailed              = "failed"
	PitrStateCancelled           = "cancelled"
)

func pitrStateIsTerminal(state string) bool {
	switch state {
	case PitrStateSucceeded, PitrStateFailed, PitrStateCancelled:
		return true
	}
	return false
}

// PitrStep records the execution trace of a single state-machine step. Each
// step transitions StartedAt -> FinishedAt with a status and optional error.
const (
	pitrStepStatusPending = "pending"
	pitrStepStatusRunning = "running"
	pitrStepStatusDone    = "done"
	pitrStepStatusFailed  = "failed"
	pitrStepStatusSkipped = "skipped"
)

type PitrStep struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// pitrFileStat is the pre/post truncate snapshot of a single AOF directory
// entry, used to detect redis-check-aof behavior drift (any non-last file
// changing after truncate means our model of the tool no longer holds).
type pitrFileStat struct {
	Size  int64
	Mtime time.Time
}

// PitrJob is the in-process record of one point-in-time recovery attempt. It
// lives only in the dashboard process memory (never in the coordinator) and is
// indexed by a UUID v7 id, mirroring RDBAnalysisJob.
type PitrJob struct {
	ID          string `json:"id"`
	ProductName string `json:"product_name"`
	GroupID     int    `json:"group_id"`
	ServerAddr  string `json:"server_addr"`
	TruncateTs  int64  `json:"truncate_ts"`

	// Located during prereq (CONFIG GET dir / appendfilename / appenddirname).
	AofDir      string `json:"aof_dir"`
	AofManifest string `json:"aof_manifest"`

	// Located during feasibility (read manifest, scan last segment #TS:).
	LastSegmentFile    string   `json:"last_segment_file,omitempty"`
	LastSegmentTsRange [2]int64 `json:"last_segment_ts_range,omitempty"`

	// Located during snapshot (sibling dir of AofDir, same filesystem).
	SnapshotDir string `json:"snapshot_dir,omitempty"`

	// Recorded before truncate, re-checked after, to detect model drift.
	PreStatSnapshot map[string]pitrFileStat `json:"-"`

	// Whether pitr_restart_command was executed (false when unset / not run).
	RestartKicked bool `json:"restart_kicked,omitempty"`

	State     string     `json:"state"`
	Steps     []PitrStep `json:"steps"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Error     string     `json:"error,omitempty"`

	mu     *sync.Mutex
	cancel stdcontext.CancelFunc
}

func (j *PitrJob) Snapshot() *PitrJob {
	j.mu.Lock()
	defer j.mu.Unlock()

	x := *j
	x.Steps = append([]PitrStep(nil), j.Steps...)
	x.PreStatSnapshot = nil
	x.mu = nil
	x.cancel = nil
	return &x
}

// setState records a top-level state transition and optional terminal error.
func (j *PitrJob) setState(state string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.State = state
	j.UpdatedAt = time.Now()
	if err != nil {
		j.Error = err.Error()
	}
}

// step records the execution of a named state-machine step. The callback
// receives the job (under its own lock) and returns an error; the step's
// status and the job state are updated accordingly. A non-nil error from the
// callback marks the step failed and the job state as failed.
func (j *PitrJob) step(name string, fn func() error) error {
	now := time.Now()
	j.mu.Lock()
	idx := len(j.Steps)
	j.Steps = append(j.Steps, PitrStep{
		Name:      name,
		Status:    pitrStepStatusRunning,
		StartedAt: &now,
	})
	j.State = stepToState(name)
	j.UpdatedAt = now
	j.mu.Unlock()

	err := fn()

	finished := time.Now()
	j.mu.Lock()
	j.Steps[idx].FinishedAt = &finished
	if err != nil {
		j.Steps[idx].Status = pitrStepStatusFailed
		j.Steps[idx].Error = err.Error()
		j.State = PitrStateFailed
		j.Error = err.Error()
	} else {
		j.Steps[idx].Status = pitrStepStatusDone
	}
	j.UpdatedAt = finished
	j.mu.Unlock()
	return err
}

// stepToState maps a step name to the state shown while that step is running.
func stepToState(name string) string {
	switch name {
	case "validate":
		return PitrStateValidating
	case "prereq":
		return PitrStateCheckingPrereq
	case "feasibility":
		return PitrStateCheckingFeasibility
	case "shutdown":
		return PitrStateShuttingDown
	case "snapshot":
		return PitrStateSnapshotting
	case "truncate":
		return PitrStateTruncating
	case "restart":
		return PitrStateRestarting
	case "wait-load":
		return PitrStateWaitingLoad
	case "resync":
		return PitrStateResyncingReplicas
	}
	return PitrStateValidating
}

// PitrManager owns the in-process registry of PITR jobs, concurrency limits
// and the per-server lock that prevents overlapping recovery on the same
// target. It mirrors RDBAnalysisManager's lifecycle.
type PitrManager struct {
	mu            sync.Mutex
	jobs          map[string]*PitrJob
	enabled       bool
	redisCheckBin string
	maxConcurrent int
	jobTimeout    time.Duration
	restartCmd    string

	// per-server locks: at most one running job per target server addr.
	serverLocks map[string]*pitrServerLock
}

type pitrServerLock struct {
	owner string // job id currently holding the lock
}

// NewPitrManager constructs the manager from dashboard config. The manager is
// always constructed (even when disabled) so the API can return a precise
// "pitr disabled" error instead of a nil dereference.
func NewPitrManager(config *Config) *PitrManager {
	redisCheckBin := config.PitrRedisCheckAofBin
	if redisCheckBin == "" {
		redisCheckBin = "bin/redis-check-aof"
	}
	if abs, err := filepath.Abs(redisCheckBin); err == nil {
		redisCheckBin = abs
	}
	maxConcurrent := config.PitrMaxConcurrentJobs
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	jobTimeout := config.PitrJobTimeout.Duration()
	if jobTimeout <= 0 {
		jobTimeout = 30 * time.Minute
	}
	return &PitrManager{
		jobs:          make(map[string]*PitrJob),
		enabled:       config.PitrEnabled,
		redisCheckBin: redisCheckBin,
		maxConcurrent: maxConcurrent,
		jobTimeout:    jobTimeout,
		restartCmd:    config.PitrRestartCommand,
		serverLocks:   make(map[string]*pitrServerLock),
	}
}

// Enabled reports whether PITR is enabled in dashboard config.
func (m *PitrManager) Enabled() bool {
	return m.enabled
}

// RedisCheckAofBin reports the resolved absolute path to redis-check-aof.
func (m *PitrManager) RedisCheckAofBin() string {
	return m.redisCheckBin
}

// JobTimeout reports the configured job timeout.
func (m *PitrManager) JobTimeout() time.Duration {
	return m.jobTimeout
}

// Create starts a new PITR job for the given server and truncate timestamp.
// It performs the synchronous part of validation (enabled, concurrent limit,
// per-server lock) and launches the state machine in a goroutine. The returned
// job is a snapshot; callers poll Get for progress.
func (m *PitrManager) Create(productName, serverAddr string, truncateTs int64, deps PitrDeps) (*PitrJob, error) {
	m.mu.Lock()
	if !m.enabled {
		m.mu.Unlock()
		return nil, errors.New("pitr is disabled")
	}
	// Verify redis-check-aof is present and executable before accepting the
	// job. Failing here (synchronously, before any side effect) is far better
	// than failing mid-state-machine after SHUTDOWN. We check it is a regular
	// file with an executable bit — a non-executable file would otherwise be
	// accepted and only fail with EACCES in stepTruncate, after the master was
	// already shut down.
	if m.redisCheckBin == "" {
		m.mu.Unlock()
		return nil, errors.New("pitr redis-check-aof binary not configured")
	}
	if !pitrBinExecutable(m.redisCheckBin) {
		m.mu.Unlock()
		return nil, errors.Errorf("pitr redis-check-aof binary not found or not executable: %s", m.redisCheckBin)
	}
	if err := m.checkConcurrentLimitLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	// Per-server lock: a single job — running OR terminal — holds the lock on
	// its server until it is explicitly Remove'd. PITR is non-idempotent, so a
	// terminal-but-unacknowledged job must still block a new job on the same
	// server (the operator must confirm the recovered state and Remove the old
	// job before starting another). The lock is only cleared by Remove.
	if existing, ok := m.serverLocks[serverAddr]; ok && existing.owner != "" {
		m.mu.Unlock()
		return nil, errors.Errorf("server %s is locked by pitr job %s; Remove that job first", serverAddr, existing.owner)
	}
	id, err := newPitrJobID()
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	// Acquire per-server lock before publishing the job.
	m.serverLocks[serverAddr] = &pitrServerLock{owner: id}
	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	now := time.Now()
	job := &PitrJob{
		ID:          id,
		ProductName: productName,
		ServerAddr:  serverAddr,
		TruncateTs:  truncateTs,
		State:       PitrStatePending,
		Steps:       []PitrStep{},
		CreatedAt:   now,
		UpdatedAt:   now,
		mu:          &sync.Mutex{},
		cancel:      cancel,
	}
	m.jobs[id] = job
	m.mu.Unlock()

	go m.run(ctx, job, deps)
	return job.Snapshot(), nil
}

func newPitrJobID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", errors.Trace(err)
	}
	return id.String(), nil
}

func (m *PitrManager) checkConcurrentLimitLocked() error {
	var n int
	for _, job := range m.jobs {
		job.mu.Lock()
		state := job.State
		job.mu.Unlock()
		if !pitrStateIsTerminal(state) {
			n++
		}
	}
	if n >= m.maxConcurrent {
		return errors.Errorf("too many running pitr jobs")
	}
	return nil
}

func (m *PitrManager) Get(id string) (*PitrJob, error) {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil {
		return nil, errors.Errorf("pitr job %s not found", id)
	}
	return job.Snapshot(), nil
}

// List returns snapshots of all known jobs, newest first.
func (m *PitrManager) List() []*PitrJob {
	m.mu.Lock()
	jobs := make([]*PitrJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	out := make([]*PitrJob, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job.Snapshot())
	}
	return out
}

func (m *PitrManager) Cancel(id string) error {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil {
		return errors.Errorf("pitr job %s not found", id)
	}
	job.mu.Lock()
	cancel := job.cancel
	state := job.State
	job.mu.Unlock()
	if pitrStateIsTerminal(state) {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

// Remove deletes a terminal job from the registry, releases its per-server lock
// (so a new job can target that server) and best-effort removes its snapshot
// directory. A running job cannot be Removed — that would silently abort an
// in-flight truncate and mask the outcome; the caller must Cancel first and let
// the job reach a terminal state. A cleanup failure is logged but does not
// block removal.
func (m *PitrManager) Remove(id string) error {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil {
		return errors.Errorf("pitr job %s not found", id)
	}
	job.mu.Lock()
	state := job.State
	job.mu.Unlock()
	if !pitrStateIsTerminal(state) {
		return errors.Errorf("pitr job %s is not terminal (state=%s); Cancel it first", id, state)
	}
	m.mu.Lock()
	delete(m.jobs, id)
	if job.ServerAddr != "" {
		if lock, ok := m.serverLocks[job.ServerAddr]; ok && lock.owner == id {
			delete(m.serverLocks, job.ServerAddr)
		}
	}
	m.mu.Unlock()
	job.mu.Lock()
	snapshotDir := job.SnapshotDir
	job.mu.Unlock()
	if snapshotDir != "" {
		if err := os.RemoveAll(snapshotDir); err != nil {
			log.WarnErrorf(err, "pitr job %s: remove snapshot dir %s failed", id, snapshotDir)
		}
	}
	return nil
}

func (m *PitrManager) Close() {
	m.mu.Lock()
	jobs := make([]*PitrJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	for _, job := range jobs {
		job.mu.Lock()
		cancel := job.cancel
		job.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// --- PitrDeps implementation wired to Topom ---

// newPitrDeps returns the production PitrDeps backed by the Topom's backend
// redis pool (stats pool, backend-auth-identity aware) and in-memory topology
// snapshot. Group/server lookups reflect the topology captured at newContext
// time, consistent with how slot actions work.
func (s *Topom) newPitrDeps() PitrDeps {
	return &topomPitrDeps{topom: s}
}

// topomPitrDeps implements PitrDeps using the Topom redis pool. Each Redis
// command borrows a client from the stats pool and returns it; the pool handles
// AUTH/SELECT/recycle semantics.
type topomPitrDeps struct {
	topom *Topom
}

// withClient borrows a backend redis client for addr, runs fn, and returns it.
func (d *topomPitrDeps) withClient(addr string, fn func(*redisutils.Client) error) error {
	c, err := d.topom.stats.redisp.GetClient(addr)
	if err != nil {
		return errors.Trace(err)
	}
	defer d.topom.stats.redisp.PutClient(c)
	return fn(c)
}

func (d *topomPitrDeps) ConfigGet(ctx stdcontext.Context, serverAddr, pattern string) (map[string]string, error) {
	var out map[string]string
	err := d.withClient(serverAddr, func(c *redisutils.Client) error {
		v, err := c.Do("CONFIG", "GET", pattern)
		if err != nil {
			return errors.Trace(err)
		}
		// CONFIG GET returns a flat array of alternating key/value bulk
		// strings. Parse it inline rather than depending on unexported redis
		// helpers.
		values, ok := v.([]interface{})
		if !ok {
			return errors.Errorf("unexpected CONFIG GET reply type %T", v)
		}
		out = make(map[string]string, len(values)/2)
		for i := 0; i+1 < len(values); i += 2 {
			key, _ := values[i].(string)
			val, _ := values[i+1].(string)
			out[key] = val
		}
		return nil
	})
	return out, err
}

func (d *topomPitrDeps) GroupOfServer(serverAddr string) (gid int, isMaster bool, err error) {
	ctx, err := d.topom.newContext()
	if err != nil {
		return 0, false, err
	}
	g, index, err := ctx.getGroupByServer(serverAddr)
	if err != nil {
		return 0, false, err
	}
	return g.Id, index == 0, nil
}

func (d *topomPitrDeps) GroupServers(gid int) ([]string, error) {
	ctx, err := d.topom.newContext()
	if err != nil {
		return nil, err
	}
	g, err := ctx.getGroup(gid)
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0, len(g.Servers))
	for _, s := range g.Servers {
		addrs = append(addrs, s.Addr)
	}
	return addrs, nil
}

func (d *topomPitrDeps) Shutdown(ctx stdcontext.Context, serverAddr string) error {
	// Send SHUTDOWN NOSAVE. Any error here is ambiguous (transport close when
	// the server exits vs. a Redis command error like NOAUTH/unknown command
	// while the server is still up), so we MUST verify the server is actually
	// down before returning success — otherwise the caller would proceed to
	// snapshot/truncate an AOF that a live server is still writing, corrupting
	// it. The verification is a fresh-connection probe: if the server still
	// answers, SHUTDOWN did not take effect.
	sendErr := d.withClient(serverAddr, func(c *redisutils.Client) error {
		_, err := c.Do("SHUTDOWN", "NOSAVE")
		return err
	})
	// Probe with a fresh connection (the pooled one was likely closed). If the
	// server still responds to any command, SHUTDOWN failed — surface the
	// original send error (or a generic one) and do NOT let the caller proceed.
	if d.serverStillUp(serverAddr) {
		if sendErr != nil {
			return errors.Errorf("shutdown failed and server is still up: %v", sendErr)
		}
		return errors.New("shutdown sent but server is still up")
	}
	// Server is unreachable: treat as shut down (transport close is the expected
	// path). This is the only success path.
	return nil
}

// serverStillUp reports whether the target server answers a fresh connection.
// Used by Shutdown to verify the server actually went down.
func (d *topomPitrDeps) serverStillUp(serverAddr string) bool {
	c, err := redisutils.NewClientWithAuthIdentity(serverAddr, d.topom.config.BackendAuthIdentity(), 2*time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	if _, err := c.Do("PING"); err != nil {
		return false
	}
	return true
}

func (d *topomPitrDeps) PingInfo(ctx stdcontext.Context, serverAddr string) (map[string]string, error) {
	var out map[string]string
	err := d.withClient(serverAddr, func(c *redisutils.Client) error {
		v, err := c.Do("INFO")
		if err != nil {
			return errors.Trace(err)
		}
		text, ok := v.(string)
		if !ok {
			return errors.Errorf("unexpected INFO reply type %T", v)
		}
		out = parsePitrInfoText(text)
		return nil
	})
	return out, err
}

func (d *topomPitrDeps) ResyncReplica(ctx stdcontext.Context, masterAddr, replicaAddr string) error {
	return d.withClient(replicaAddr, func(c *redisutils.Client) error {
		host, port := splitPitrHostPort(masterAddr)
		// After truncate the master's keyspace (and replication id/offset) was
		// rewound, so a re-attaching replica is forced into a full resync
		// regardless. SLAVEOF host port is sufficient; we do NOT issue
		// SLAVEOF NO ONE first because that needlessly drops the replica into
		// standalone mode if the subsequent command were to fail.
		if _, err := c.Do("SLAVEOF", host, port); err != nil {
			return errors.Trace(err)
		}
		return nil
	})
}

// parsePitrInfoText parses an INFO reply blob into a flat key->value map. It
// mirrors redisutils.Client.Info()'s parser but is local to keep PITR's surface
// independent of that unexported logic.
func parsePitrInfoText(text string) map[string]string {
	info := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		if key := strings.TrimSpace(kv[0]); key != "" {
			info[key] = strings.TrimSpace(kv[1])
		}
	}
	return info
}

// splitPitrHostPort splits "host:port" without erroring on malformed input
// (returns the raw addr as host, empty port) — the subsequent SLAVEOF will
// surface a real error if the addr is bad.
func splitPitrHostPort(addr string) (string, string) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	return addr, ""
}
