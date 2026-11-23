// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/CodisLabs/codis/pkg/migration"
	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// Data migration job states. pending/starting/running are non-terminal.
const (
	DataMigrationStatePending   = "pending"
	DataMigrationStateStarting  = "starting"
	DataMigrationStateRunning   = "running"
	DataMigrationStateSucceeded = "succeeded"
	DataMigrationStateFailed    = "failed"
	DataMigrationStateStopped   = "stopped"
)

// RedisShake reader phases surfaced as DataMigrationJob.Phase.
const (
	dataMigrationPhaseScanning = "scanning"
	dataMigrationPhaseDraining = "draining"
)

const (
	dataMigrationJobsDirName     = "jobs"
	dataMigrationStoreFileName   = "jobs.json"
	dataMigrationConsoleFileName = "console.log"
	dataMigrationDefaultLogBytes = 64 * 1024
	dataMigrationMaxLogBytes     = 1024 * 1024
	dataMigrationConsoleTailSize = 4096
	dataMigrationInterrupted     = "interrupted by dashboard shutdown or restart; RedisShake cannot resume, create a new task"
)

func dataMigrationStateIsTerminal(state string) bool {
	switch state {
	case DataMigrationStateSucceeded, DataMigrationStateFailed, DataMigrationStateStopped:
		return true
	}
	return false
}

// DataMigrationStats is the compact view of the RedisShake status document.
type DataMigrationStats struct {
	ReadCount        uint64  `json:"read_count"`
	WriteCount       uint64  `json:"write_count"`
	ReadOps          float64 `json:"read_ops"`
	WriteOps         float64 `json:"write_ops"`
	Consistent       bool    `json:"consistent"`
	Phase            string  `json:"phase"`
	RdbReceivedBytes uint64  `json:"rdb_received_bytes"`
	RdbTotalBytes    uint64  `json:"rdb_total_bytes"`
	AofLagBytes      *int64  `json:"aof_lag_bytes,omitempty"`
	Shards           int     `json:"shards"`
	ConsistentShards int     `json:"consistent_shards"`
}

// DataMigrationJob is the dashboard-side record of one migration task. The
// definition lives in the task directory (spec.json) and the registry is
// persisted (redacted) in jobs.json under the workspace.
type DataMigrationJob struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Mode   string                 `json:"mode"`
	Source migration.EndpointView `json:"source"`
	Target migration.EndpointView `json:"target"`
	Filter migration.Filter       `json:"filter"`

	RestoreBehavior    string `json:"restore_behavior"`
	EmptyDBBeforeSync  bool   `json:"empty_db_before_sync"`
	TargetRedisMaxQPS  int    `json:"target_redis_max_qps"`
	PipelineCountLimit int    `json:"pipeline_count_limit"`

	State       string             `json:"state"`
	Phase       string             `json:"phase"`
	Stats       DataMigrationStats `json:"stats"`
	Reader      json.RawMessage    `json:"reader,omitempty"`
	Writer      json.RawMessage    `json:"writer,omitempty"`
	PID         int                `json:"pid"`
	CreatedAt   time.Time          `json:"created_at"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	FinishedAt  *time.Time         `json:"finished_at,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Error       string             `json:"error,omitempty"`
	ExitCode    *int               `json:"exit_code,omitempty"`
	Dir         string             `json:"dir"`
	LogFile     string             `json:"log_file"`
	ConsoleFile string             `json:"console_file"`

	// ConsoleTail is only filled by Get for crash diagnostics.
	ConsoleTail string `json:"console_tail,omitempty"`
}

// dataMigrationTask couples the job record with the live worker process.
type dataMigrationTask struct {
	mu            sync.Mutex
	job           *DataMigrationJob
	spec          *migration.Spec
	cmd           *exec.Cmd
	console       *os.File
	statusPort    int
	stopRequested bool
	shutdown      bool
	done          chan struct{}
}

func (t *dataMigrationTask) snapshot() *DataMigrationJob {
	t.mu.Lock()
	defer t.mu.Unlock()
	x := *t.job
	x.Reader = append(json.RawMessage(nil), t.job.Reader...)
	x.Writer = append(json.RawMessage(nil), t.job.Writer...)
	if t.job.StartedAt != nil {
		started := *t.job.StartedAt
		x.StartedAt = &started
	}
	if t.job.FinishedAt != nil {
		finished := *t.job.FinishedAt
		x.FinishedAt = &finished
	}
	if t.job.ExitCode != nil {
		code := *t.job.ExitCode
		x.ExitCode = &code
	}
	x.ConsoleTail = ""
	return &x
}

// DataMigrationManager owns the in-process registry of data migration tasks
// and supervises the codis-shake worker processes. It mirrors the lifecycle
// of PitrManager / RDBAnalysisManager but additionally persists a redacted
// snapshot of the registry in the workspace so the task history survives a
// dashboard restart (running tasks are marked failed: RedisShake 4.x has no
// resume).
type DataMigrationManager struct {
	topom *Topom

	mu     sync.Mutex
	tasks  map[string]*dataMigrationTask
	closed bool

	enabled       bool
	workspace     string
	shakeBin      string
	maxConcurrent int
	maxRetained   int

	startTimeout time.Duration
	pollInterval time.Duration
	stopGrace    time.Duration
	statusClient *http.Client

	// spawnArgs overrides the worker argument list (tests re-execute the test
	// binary as the worker). nil means {"--spec", specPath}.
	spawnArgs func(specPath string) []string
}

func (m *DataMigrationManager) workerArgs(specPath string) []string {
	if m.spawnArgs != nil {
		return m.spawnArgs(specPath)
	}
	return []string{"--spec", specPath}
}

func newDataMigrationManager(config *Config, topom *Topom) *DataMigrationManager {
	shakeBin := config.DataMigrationShakeBin
	if shakeBin == "" {
		shakeBin = "bin/codis-shake"
	}
	if abs, err := filepath.Abs(shakeBin); err == nil {
		shakeBin = abs
	}
	workspace := config.DataMigrationWorkspace
	if workspace == "" {
		workspace = filepath.Join(os.TempDir(), "codis-data-migration")
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	maxConcurrent := config.DataMigrationMaxConcurrentJobs
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	maxRetained := config.DataMigrationMaxJobsRetained
	if maxRetained <= 0 {
		maxRetained = 50
	}
	m := &DataMigrationManager{
		topom:         topom,
		tasks:         make(map[string]*dataMigrationTask),
		enabled:       config.DataMigrationEnabled,
		workspace:     workspace,
		shakeBin:      shakeBin,
		maxConcurrent: maxConcurrent,
		maxRetained:   maxRetained,
		startTimeout:  30 * time.Second,
		pollInterval:  time.Second,
		stopGrace:     15 * time.Second,
		statusClient:  &http.Client{Timeout: 2 * time.Second},
	}
	m.load()
	return m
}

// Enabled reports whether the data integration console is enabled.
func (m *DataMigrationManager) Enabled() bool {
	return m.enabled
}

// Workspace reports the resolved task workspace directory.
func (m *DataMigrationManager) Workspace() string {
	return m.workspace
}

// ShakeBin reports the resolved codis-shake path.
func (m *DataMigrationManager) ShakeBin() string {
	return m.shakeBin
}

func newDataMigrationID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", errors.Trace(err)
	}
	return id.String(), nil
}

// DataMigrationShardRequest is one manually entered Codis shard.
type DataMigrationShardRequest struct {
	Address   string `json:"address"`
	SlotStart int    `json:"slot_start"`
	SlotEnd   int    `json:"slot_end"`
}

// DataMigrationEndpointRequest is one side of a create request.
type DataMigrationEndpointRequest struct {
	Kind     string `json:"kind"`
	Address  string `json:"address"`
	Username string `json:"username"`
	Password string `json:"password"`
	TLS      bool   `json:"tls"`

	// PreferReplica reads from a replica (sync mode, source only).
	PreferReplica bool `json:"prefer_replica"`

	// CodisScope selects how a Codis endpoint is resolved: "current" (default)
	// exports the topology of this dashboard's product, "manual" uses Shards.
	CodisScope string                      `json:"codis_scope"`
	Shards     []DataMigrationShardRequest `json:"shards"`
}

// DataMigrationCreateRequest is the body of PUT /api/topom/data-migration/create.
type DataMigrationCreateRequest struct {
	Name   string                       `json:"name"`
	Mode   string                       `json:"mode"`
	Source DataMigrationEndpointRequest `json:"source"`
	Target DataMigrationEndpointRequest `json:"target"`
	Filter migration.Filter             `json:"filter"`

	RestoreBehavior    string `json:"restore_behavior"`
	TargetRedisMaxQPS  int    `json:"target_redis_max_qps"`
	PipelineCountLimit int    `json:"pipeline_count_limit"`
	EmptyDBBeforeSync  bool   `json:"empty_db_before_sync"`
}

func (r *DataMigrationCreateRequest) normalizeMode() string {
	switch strings.ToLower(strings.TrimSpace(r.Mode)) {
	case "", migration.ModeSync:
		return migration.ModeSync
	case migration.ModeScan:
		return migration.ModeScan
	case migration.ModeRDB:
		return migration.ModeRDB
	}
	return strings.ToLower(strings.TrimSpace(r.Mode))
}

// DataMigrationTopology is the Codis topology preview of the current product.
type DataMigrationTopology struct {
	Groups          []DataMigrationTopologyGroup `json:"groups"`
	UnassignedSlots int                          `json:"unassigned_slots"`
	SlotActions     int                          `json:"slot_actions"`
	Complete        bool                         `json:"complete"`
	Online          bool                         `json:"online"`
}

// DataMigrationTopologyGroup is one Group Master with its slot ranges.
type DataMigrationTopologyGroup struct {
	GroupID   int                   `json:"group_id"`
	Master    string                `json:"master"`
	SlotCount int                   `json:"slot_count"`
	Slots     []migration.SlotRange `json:"slots"`
}

// codisTopologySnapshot exports the Group Master / slot ownership snapshot.
type codisTopologySnapshot struct {
	groups        []DataMigrationTopologyGroup
	byGroup       map[int][]int
	unassigned    int
	actions       int
	missingMaster []int
}

func dataMigrationSlotRanges(sorted []int) []migration.SlotRange {
	ranges := make([]migration.SlotRange, 0)
	for i := 0; i < len(sorted); {
		j := i
		for j+1 < len(sorted) && sorted[j+1] == sorted[j]+1 {
			j++
		}
		ranges = append(ranges, migration.SlotRange{Start: sorted[i], End: sorted[j]})
		i = j + 1
	}
	return ranges
}

// Topology returns the Group Master / slot ownership snapshot of the current
// product. It is what the console shows before creating a Codis-side task.
func (m *DataMigrationManager) Topology() (*DataMigrationTopology, error) {
	snap, err := m.topom.codisTopologySnapshot()
	if err != nil {
		return nil, err
	}
	return snap.topology(), nil
}

func (s *Topom) codisTopologySnapshot() (*codisTopologySnapshot, error) {
	ctx, err := s.newContext()
	if err != nil {
		return nil, err
	}
	snap := &codisTopologySnapshot{byGroup: make(map[int][]int)}
	for _, slot := range ctx.slots {
		if slot.Action.State != models.ActionNothing {
			snap.actions++
		}
		if slot.GroupId == 0 {
			snap.unassigned++
			continue
		}
		snap.byGroup[slot.GroupId] = append(snap.byGroup[slot.GroupId], slot.Id)
	}
	gids := make([]int, 0, len(snap.byGroup))
	for gid := range snap.byGroup {
		gids = append(gids, gid)
	}
	sort.Ints(gids)
	for _, gid := range gids {
		slots := snap.byGroup[gid]
		sort.Ints(slots)
		master := ctx.getGroupMaster(gid)
		if master == "" {
			snap.missingMaster = append(snap.missingMaster, gid)
		}
		snap.groups = append(snap.groups, DataMigrationTopologyGroup{
			GroupID:   gid,
			Master:    master,
			SlotCount: len(slots),
			Slots:     dataMigrationSlotRanges(slots),
		})
	}
	return snap, nil
}

func (s *codisTopologySnapshot) topology() *DataMigrationTopology {
	return &DataMigrationTopology{
		Groups:          append([]DataMigrationTopologyGroup{}, s.groups...),
		UnassignedSlots: s.unassigned,
		SlotActions:     s.actions,
		Complete:        s.unassigned == 0 && s.actions == 0 && len(s.missingMaster) == 0 && len(s.groups) > 0,
		Online:          true,
	}
}

func (s *codisTopologySnapshot) shards(isTarget bool) ([]migration.Shard, error) {
	if s.actions > 0 {
		return nil, errors.Errorf("%d slot migration action(s) in progress; wait for them to finish before migrating data to or from Codis", s.actions)
	}
	if isTarget && s.unassigned > 0 {
		return nil, errors.Errorf("%d slots are not assigned to any group; assign every slot before migrating data into Codis", s.unassigned)
	}
	if len(s.groups) == 0 {
		return nil, errors.New("no group owns any slot in the current product")
	}
	if len(s.missingMaster) > 0 {
		return nil, errors.Errorf("group %v has no server", s.missingMaster)
	}
	shards := make([]migration.Shard, 0, len(s.groups))
	for _, g := range s.groups {
		shards = append(shards, migration.Shard{Address: g.Master, Slots: g.Slots})
	}
	return shards, nil
}

func (m *DataMigrationManager) resolveEndpoint(side string, r DataMigrationEndpointRequest, isTarget bool) (migration.Endpoint, error) {
	kind := strings.ToLower(strings.TrimSpace(r.Kind))
	if kind == "" {
		kind = migration.EndpointStandalone
	}
	e := migration.Endpoint{
		Username: r.Username,
		Password: r.Password,
		TLS:      r.TLS,
	}
	switch kind {
	case "codis":
		scope := strings.ToLower(strings.TrimSpace(r.CodisScope))
		if scope == "" {
			scope = "current"
		}
		switch scope {
		case "current":
			snap, err := m.topom.codisTopologySnapshot()
			if err != nil {
				return e, errors.Errorf("%s: resolve codis topology failed: %v", side, err)
			}
			shards, err := snap.shards(isTarget)
			if err != nil {
				return e, errors.Errorf("%s: %v", side, err)
			}
			auth := m.topom.config.BackendAuthIdentity()
			e.Kind = migration.EndpointStatic
			e.Username = auth.Username
			e.Password = auth.Password
			e.Shards = shards
			return e, nil
		case "manual":
			e.Kind = migration.EndpointStatic
			for _, s := range r.Shards {
				shard := migration.Shard{Address: strings.TrimSpace(s.Address)}
				if isTarget || s.SlotStart != 0 || s.SlotEnd != 0 {
					shard.Slots = []migration.SlotRange{{Start: s.SlotStart, End: s.SlotEnd}}
				}
				e.Shards = append(e.Shards, shard)
			}
			return e, nil
		default:
			return e, errors.Errorf("%s: invalid codis_scope %q", side, r.CodisScope)
		}
	case migration.EndpointStandalone, migration.EndpointCluster:
		e.Kind = kind
		e.Address = strings.TrimSpace(r.Address)
		e.PreferReplica = r.PreferReplica
		return e, nil
	}
	return e, errors.Errorf("%s: invalid kind %q", side, r.Kind)
}

// buildSpec turns the create request into a validated worker spec.
func (m *DataMigrationManager) buildSpec(req *DataMigrationCreateRequest) (*migration.Spec, error) {
	if req == nil {
		return nil, errors.New("missing request body")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("missing name")
	}
	mode := req.normalizeMode()
	source, err := m.resolveEndpoint("source", req.Source, false)
	if err != nil {
		return nil, err
	}
	target, err := m.resolveEndpoint("target", req.Target, true)
	if err != nil {
		return nil, err
	}
	if source.Kind != migration.EndpointStatic && target.Kind != migration.EndpointStatic &&
		source.Address != "" && strings.EqualFold(source.Address, target.Address) {
		return nil, errors.Errorf("source and target are the same address %s", source.Address)
	}
	if shared := sharedEndpointAddress(source, target); shared != "" {
		return nil, errors.Errorf("source and target both contain server %s; migrating a server into itself is not allowed", shared)
	}
	restore := strings.ToLower(strings.TrimSpace(req.RestoreBehavior))
	if restore == "" {
		restore = migration.RestoreRewrite
	}
	spec := &migration.Spec{
		Name:   name,
		Mode:   mode,
		Source: source,
		Target: target,
		Filter: req.Filter,
		Advanced: migration.Advanced{
			RestoreBehavior:    restore,
			TargetRedisMaxQPS:  req.TargetRedisMaxQPS,
			PipelineCountLimit: req.PipelineCountLimit,
			EmptyDBBeforeSync:  req.EmptyDBBeforeSync,
			LogLevel:           "info",
		},
		ParentPID: os.Getpid(),
	}
	// Create assigns the id and status port and validates the spec before any
	// side effect; request errors are surfaced by resolveEndpoint above.
	return spec, nil
}

func (m *DataMigrationManager) binExecutable() error {
	if !pitrBinExecutable(m.shakeBin) {
		return errors.Errorf("codis-shake binary not found or not executable: %s (build it with `make codis-shake` and set data_migration_shake_bin)", m.shakeBin)
	}
	return nil
}

// sharedEndpointAddress returns the first server address (case-insensitive)
// present on both sides: the single address of standalone/cluster endpoints
// plus every shard address of static (Codis) endpoints.
func sharedEndpointAddress(source, target migration.Endpoint) string {
	addresses := func(e migration.Endpoint) map[string]bool {
		set := make(map[string]bool)
		if a := strings.ToLower(strings.TrimSpace(e.Address)); a != "" {
			set[a] = true
		}
		for _, shard := range e.Shards {
			if a := strings.ToLower(strings.TrimSpace(shard.Address)); a != "" {
				set[a] = true
			}
		}
		return set
	}
	seen := addresses(source)
	for a := range addresses(target) {
		if seen[a] {
			return a
		}
	}
	return ""
}

func (m *DataMigrationManager) checkConcurrentLocked() error {
	n := 0
	for _, t := range m.tasks {
		t.mu.Lock()
		terminal := dataMigrationStateIsTerminal(t.job.State)
		t.mu.Unlock()
		if !terminal {
			n++
		}
	}
	if n >= m.maxConcurrent {
		return errors.Errorf("too many running data migration jobs (limit %d)", m.maxConcurrent)
	}
	return nil
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, errors.Trace(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Create validates the request, resolves the Codis topology when needed,
// writes spec.json and starts the codis-shake worker. The returned job is a
// snapshot; callers poll Get for progress.
func (m *DataMigrationManager) Create(req *DataMigrationCreateRequest) (*DataMigrationJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosedTopom
	}
	if !m.enabled {
		return nil, errors.New("data migration is disabled")
	}
	if err := m.binExecutable(); err != nil {
		return nil, err
	}
	if err := m.checkConcurrentLocked(); err != nil {
		return nil, err
	}
	spec, err := m.buildSpec(req)
	if err != nil {
		return nil, err
	}
	id, err := newDataMigrationID()
	if err != nil {
		return nil, err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	spec.ID = id
	spec.Advanced.StatusPort = port
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	jobDir := filepath.Join(m.workspace, dataMigrationJobsDirName, id)
	// The directory holds spec.json (credentials) and the worker logs, which
	// may contain key names: keep it private to the dashboard user even though
	// RedisShake creates shake.log with 0666&~umask.
	if err := os.MkdirAll(jobDir, 0700); err != nil {
		return nil, errors.Trace(err)
	}
	abort := func(err error, console *os.File) (*DataMigrationJob, error) {
		if console != nil {
			_ = console.Close()
		}
		_ = os.RemoveAll(jobDir)
		return nil, err
	}
	specPath := filepath.Join(jobDir, migration.SpecFileName)
	specBytes, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return abort(errors.Trace(err), nil)
	}
	if err := os.WriteFile(specPath, specBytes, 0600); err != nil {
		return abort(errors.Trace(err), nil)
	}
	consolePath := filepath.Join(jobDir, dataMigrationConsoleFileName)
	console, err := os.OpenFile(consolePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return abort(errors.Trace(err), nil)
	}

	now := time.Now()
	job := &DataMigrationJob{
		ID:                 id,
		Name:               spec.Name,
		Mode:               spec.Mode,
		Source:             spec.Source.View(),
		Target:             spec.Target.View(),
		Filter:             spec.Filter,
		RestoreBehavior:    spec.Advanced.RestoreBehavior,
		EmptyDBBeforeSync:  spec.Advanced.EmptyDBBeforeSync,
		TargetRedisMaxQPS:  spec.Advanced.TargetRedisMaxQPS,
		PipelineCountLimit: spec.Advanced.PipelineCountLimit,
		State:              DataMigrationStatePending,
		Stats:              DataMigrationStats{},
		CreatedAt:          now,
		UpdatedAt:          now,
		Dir:                jobDir,
		LogFile:            filepath.Join(jobDir, migration.LogFileName),
		ConsoleFile:        consolePath,
	}

	cmd := exec.Command(m.shakeBin, m.workerArgs(specPath)...)
	cmd.Dir = jobDir
	cmd.Stdout = console
	cmd.Stderr = console
	if err := cmd.Start(); err != nil {
		return abort(errors.Errorf("start %s failed: %v", m.shakeBin, err), console)
	}
	job.PID = cmd.Process.Pid
	job.State = DataMigrationStateStarting
	job.StartedAt = &now

	task := &dataMigrationTask{
		job:        job,
		spec:       spec,
		cmd:        cmd,
		console:    console,
		statusPort: port,
		done:       make(chan struct{}),
	}
	m.tasks[id] = task
	m.persistLocked()
	log.Warnf("data migration job %s (%s) started: %s %s -> %s, pid=%d, status_port=%d",
		id, spec.Name, spec.Mode, migrationEndpointText(spec.Source), migrationEndpointText(spec.Target), job.PID, port)
	go m.supervise(task)
	return task.snapshot(), nil
}

func migrationEndpointText(e migration.Endpoint) string {
	switch e.Kind {
	case migration.EndpointStatic:
		return fmt.Sprintf("%d shards", len(e.Shards))
	default:
		return e.Address
	}
}

func (m *DataMigrationManager) Get(id string) (*DataMigrationJob, error) {
	t, err := m.task(id)
	if err != nil {
		return nil, err
	}
	job := t.snapshot()
	job.ConsoleTail = dataMigrationConsoleTail(job.ConsoleFile)
	return job, nil
}

// List returns snapshots of all known jobs, newest first.
func (m *DataMigrationManager) List() []*DataMigrationJob {
	m.mu.Lock()
	tasks := make([]*dataMigrationTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	m.mu.Unlock()
	jobs := make([]*DataMigrationJob, 0, len(tasks))
	for _, t := range tasks {
		jobs = append(jobs, t.snapshot())
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})
	return jobs
}

func (m *DataMigrationManager) task(id string) (*dataMigrationTask, error) {
	m.mu.Lock()
	t := m.tasks[id]
	m.mu.Unlock()
	if t == nil {
		return nil, errors.Errorf("data migration job %s not found", id)
	}
	return t, nil
}

// Stop asks a running worker to stop gracefully and waits up to the stop
// grace period before killing it. Stopping a terminal job is a no-op.
func (m *DataMigrationManager) Stop(id string) error {
	t, err := m.task(id)
	if err != nil {
		return err
	}
	return m.stopTask(t, m.stopGrace)
}

func (m *DataMigrationManager) stopTask(t *dataMigrationTask, grace time.Duration) error {
	t.mu.Lock()
	if dataMigrationStateIsTerminal(t.job.State) {
		t.mu.Unlock()
		return nil
	}
	t.stopRequested = true
	proc := t.cmd.Process
	t.mu.Unlock()
	if proc != nil {
		_ = proc.Signal(syscall.SIGTERM)
	}
	select {
	case <-t.done:
		return nil
	case <-time.After(grace):
	}
	if proc != nil {
		t.mu.Lock()
		id := t.job.ID
		t.mu.Unlock()
		log.Warnf("data migration job %s did not stop within %s, killing pid=%d", id, grace, proc.Pid)
		_ = proc.Kill()
	}
	select {
	case <-t.done:
	case <-time.After(5 * time.Second):
	}
	return nil
}

// Remove deletes a terminal job, its worker directory and its persisted
// snapshot. A running job must be stopped first.
func (m *DataMigrationManager) Remove(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return errors.Errorf("data migration job %s not found", id)
	}
	t.mu.Lock()
	state := t.job.State
	dir := t.job.Dir
	t.mu.Unlock()
	if !dataMigrationStateIsTerminal(state) {
		m.mu.Unlock()
		return errors.Errorf("data migration job %s is not terminal (state=%s); stop it first", id, state)
	}
	delete(m.tasks, id)
	m.persistLocked()
	m.mu.Unlock()
	if dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			log.WarnErrorf(err, "remove data migration job dir %s failed", dir)
		}
	}
	return nil
}

// DataMigrationLog is the incremental log page returned by Logs.
type DataMigrationLog struct {
	File       string `json:"file"`
	Text       string `json:"text"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Size       int64  `json:"size"`
	Truncated  bool   `json:"truncated"`
}

// Logs reads at most maxBytes of the worker log starting at offset. It
// prefers the RedisShake log and falls back to the console capture.
func (m *DataMigrationManager) Logs(id string, offset int64, maxBytes int) (*DataMigrationLog, error) {
	t, err := m.task(id)
	if err != nil {
		return nil, err
	}
	job := t.snapshot()
	if maxBytes <= 0 {
		maxBytes = dataMigrationDefaultLogBytes
	}
	maxBytes = min(maxBytes, dataMigrationMaxLogBytes)
	path := job.LogFile
	name := migration.LogFileName
	if _, err := os.Stat(path); err != nil {
		path = job.ConsoleFile
		name = dataMigrationConsoleFileName
	}
	page := &DataMigrationLog{File: name, Offset: offset}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return page, nil
		}
		return nil, errors.Trace(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, errors.Trace(err)
	}
	page.Size = fi.Size()
	if offset < 0 || offset > page.Size {
		offset = 0
		page.Offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, errors.Trace(err)
	}
	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, errors.Trace(err)
	}
	page.Text = string(buf[:n])
	page.NextOffset = offset + int64(n)
	page.Truncated = page.NextOffset < page.Size
	return page, nil
}

func dataMigrationConsoleTail(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return ""
	}
	size := min(fi.Size(), int64(dataMigrationConsoleTailSize))
	if _, err := f.Seek(-size, io.SeekEnd); err != nil {
		return ""
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ""
	}
	return string(buf[:n])
}

// supervise polls the worker status port until the process exits.
func (m *DataMigrationManager) supervise(t *dataMigrationTask) {
	defer close(t.done)
	waitCh := make(chan error, 1)
	go func() { waitCh <- t.cmd.Wait() }()
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	startDeadline := time.Now().Add(m.startTimeout)
	for {
		select {
		case err := <-waitCh:
			m.finalize(t, err)
			return
		case <-ticker.C:
			m.pollStatus(t)
			t.mu.Lock()
			starting := t.job.State == DataMigrationStateStarting && t.job.StartedAt != nil
			t.mu.Unlock()
			if starting && time.Now().After(startDeadline) {
				t.mu.Lock()
				id, console := t.job.ID, t.job.ConsoleFile
				t.mu.Unlock()
				log.Warnf("data migration job %s has not published a status on port %d yet; check %s",
					id, t.statusPort, console)
				startDeadline = time.Now().Add(m.startTimeout)
			}
		}
	}
}

// shakeStatusReply mirrors the RedisShake status document.
type shakeStatusReply struct {
	Consistent bool `json:"consistent"`
	Total      struct {
		ReadCount  uint64  `json:"read_count"`
		ReadOps    float64 `json:"read_ops"`
		WriteCount uint64  `json:"write_count"`
		WriteOps   float64 `json:"write_ops"`
	} `json:"total_entries_count"`
	Reader json.RawMessage `json:"reader"`
	Writer json.RawMessage `json:"writer"`
}

// shakeReaderStat mirrors syncStandaloneReaderStat / scanStandaloneReader stat.
type shakeReaderStat struct {
	Name              string `json:"name"`
	Status            string `json:"status"`
	ScanFinished      bool   `json:"scan_finished"`
	RdbFileSizeBytes  uint64 `json:"rdb_file_size_bytes"`
	RdbReceivedBytes  uint64 `json:"rdb_received_bytes"`
	RdbSentBytes      uint64 `json:"rdb_sent_bytes"`
	AofReceivedOffset int64  `json:"aof_received_offset"`
	AofSentOffset     int64  `json:"aof_sent_offset"`
}

func (m *DataMigrationManager) fetchStatus(port int) (*shakeStatusReply, error) {
	resp, err := m.statusClient.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("status endpoint replied %s", resp.Status)
	}
	var reply shakeStatusReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// dataMigrationReaderStats accepts the single-object (standalone) and the
// array (cluster / Codis static) reader status shapes.
func dataMigrationReaderStats(raw json.RawMessage) []shakeReaderStat {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '[' {
		var stats []shakeReaderStat
		if json.Unmarshal(raw, &stats) != nil {
			return nil
		}
		return stats
	}
	var stat shakeReaderStat
	if json.Unmarshal(raw, &stat) != nil {
		return nil
	}
	return []shakeReaderStat{stat}
}

// dataMigrationPhase summarises the reader statuses into one phase label.
func dataMigrationPhase(stats []shakeReaderStat) string {
	for _, s := range stats {
		switch {
		case s.Status != "":
			return s.Status
		case s.ScanFinished:
			return dataMigrationPhaseDraining
		case s.Name != "":
			return dataMigrationPhaseScanning
		}
	}
	return ""
}

func dataMigrationStats(reply *shakeStatusReply, oneShot bool) DataMigrationStats {
	stats := DataMigrationStats{
		ReadCount:  reply.Total.ReadCount,
		WriteCount: reply.Total.WriteCount,
		ReadOps:    reply.Total.ReadOps,
		WriteOps:   reply.Total.WriteOps,
		Consistent: reply.Consistent,
	}
	readers := dataMigrationReaderStats(reply.Reader)
	stats.Shards = len(readers)
	stats.Phase = dataMigrationPhase(readers)
	var lag int64
	for _, r := range readers {
		stats.RdbTotalBytes += r.RdbFileSizeBytes
		stats.RdbReceivedBytes += max(r.RdbReceivedBytes, r.RdbSentBytes)
		lag += max(r.AofReceivedOffset-r.AofSentOffset, 0)
		if (r.Status == "syncing aof" && r.AofReceivedOffset != 0 && r.AofReceivedOffset == r.AofSentOffset) || r.ScanFinished {
			stats.ConsistentShards++
		}
	}
	if !oneShot {
		stats.AofLagBytes = &lag
	}
	return stats
}

func (m *DataMigrationManager) pollStatus(t *dataMigrationTask) {
	t.mu.Lock()
	port := t.statusPort
	oneShot := t.spec.OneShot()
	t.mu.Unlock()
	reply, err := m.fetchStatus(port)
	if err != nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.job.State == DataMigrationStateStarting || t.job.State == DataMigrationStatePending {
		t.job.State = DataMigrationStateRunning
		if t.job.StartedAt == nil {
			t.job.StartedAt = &now
		}
	}
	t.job.Stats = dataMigrationStats(reply, oneShot)
	t.job.Phase = t.job.Stats.Phase
	t.job.Reader = append(json.RawMessage(nil), reply.Reader...)
	t.job.Writer = append(json.RawMessage(nil), reply.Writer...)
	t.job.UpdatedAt = now
	t.mu.Unlock()
}

// applyFinalStatus fills the job stats from the snapshot the worker writes
// right before it exits. A one-shot task can drain between two polls (or before
// the first one), and the last live poll is always slightly stale, so the
// worker's own final document is the authoritative end state. Missing files
// (worker killed, or it died before publishing) leave the last poll in place.
func (m *DataMigrationManager) applyFinalStatus(t *dataMigrationTask) {
	t.mu.Lock()
	if t.spec == nil {
		t.mu.Unlock()
		return
	}
	dir := t.job.Dir
	t.mu.Unlock()
	if dir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(dir, migration.StatusFileName))
	if err != nil {
		return
	}
	var reply shakeStatusReply
	if err := json.Unmarshal(b, &reply); err != nil {
		log.WarnErrorf(err, "decode final status of data migration job %s failed", dir)
		return
	}
	now := time.Now()
	t.mu.Lock()
	t.job.Stats = dataMigrationStats(&reply, t.spec.OneShot())
	t.job.Phase = t.job.Stats.Phase
	t.job.Reader = append(json.RawMessage(nil), reply.Reader...)
	t.job.Writer = append(json.RawMessage(nil), reply.Writer...)
	t.job.UpdatedAt = now
	t.mu.Unlock()
}

func (m *DataMigrationManager) finalize(t *dataMigrationTask, waitErr error) {
	m.applyFinalStatus(t)
	exitCode := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if stderrors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	now := time.Now()
	t.mu.Lock()
	job := t.job
	oneShot := t.spec.OneShot()
	switch {
	case t.shutdown:
		job.State = DataMigrationStateFailed
		job.Error = dataMigrationInterrupted
	case t.stopRequested:
		job.State = DataMigrationStateStopped
		if exitCode != 0 {
			job.Error = fmt.Sprintf("worker stopped with exit code %d", exitCode)
		}
	case exitCode == 0 && oneShot:
		job.State = DataMigrationStateSucceeded
	case exitCode == 0:
		job.State = DataMigrationStateStopped
		job.Error = "worker exited on its own; create a new task to resume replication"
	default:
		job.State = DataMigrationStateFailed
		job.Error = fmt.Sprintf("codis-shake exited with code %d; check the task log", exitCode)
	}
	job.PID = 0
	job.ExitCode = &exitCode
	job.FinishedAt = &now
	job.UpdatedAt = now
	console := t.console
	id := job.ID
	t.mu.Unlock()
	if console != nil {
		_ = console.Close()
	}
	log.Warnf("data migration job %s finished: state=%s exit=%d", id, job.State, exitCode)

	m.mu.Lock()
	m.persistLocked()
	m.enforceRetentionLocked()
	m.mu.Unlock()
}

// --- persistence -----------------------------------------------------------

type dataMigrationStore struct {
	Jobs []*DataMigrationJob `json:"jobs"`
}

func (m *DataMigrationManager) storePath() string {
	return filepath.Join(m.workspace, dataMigrationStoreFileName)
}

// persistLocked writes the redacted registry. Caller holds m.mu.
func (m *DataMigrationManager) persistLocked() {
	store := &dataMigrationStore{Jobs: []*DataMigrationJob{}}
	for _, t := range m.tasks {
		store.Jobs = append(store.Jobs, t.snapshot())
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		log.WarnErrorf(err, "encode data migration store failed")
		return
	}
	if err := os.MkdirAll(m.workspace, 0700); err != nil {
		log.WarnErrorf(err, "create data migration workspace %s failed", m.workspace)
		return
	}
	tmp := m.storePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		log.WarnErrorf(err, "write data migration store failed")
		return
	}
	if err := os.Rename(tmp, m.storePath()); err != nil {
		log.WarnErrorf(err, "rename data migration store failed")
	}
}

// enforceRetentionLocked drops the oldest terminal jobs above the retention
// limit. Caller holds m.mu.
func (m *DataMigrationManager) enforceRetentionLocked() {
	type entry struct {
		id    string
		finat time.Time
		dir   string
	}
	var terminal []entry
	for id, t := range m.tasks {
		t.mu.Lock()
		if dataMigrationStateIsTerminal(t.job.State) {
			finat := t.job.CreatedAt
			if t.job.FinishedAt != nil {
				finat = *t.job.FinishedAt
			}
			terminal = append(terminal, entry{id: id, finat: finat, dir: t.job.Dir})
		}
		t.mu.Unlock()
	}
	if len(terminal) <= m.maxRetained {
		return
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].finat.Before(terminal[j].finat) })
	for _, e := range terminal[:len(terminal)-m.maxRetained] {
		delete(m.tasks, e.id)
		if e.dir != "" {
			_ = os.RemoveAll(e.dir)
		}
		log.Warnf("data migration job %s removed by retention limit %d", e.id, m.maxRetained)
	}
	m.persistLocked()
}

// load restores the persisted registry. Jobs that were running when the
// dashboard stopped are marked failed: workers were terminated with the
// dashboard and RedisShake cannot resume.
func (m *DataMigrationManager) load() {
	b, err := os.ReadFile(m.storePath())
	if err != nil {
		return
	}
	var store dataMigrationStore
	if err := json.Unmarshal(b, &store); err != nil {
		log.WarnErrorf(err, "decode data migration store %s failed", m.storePath())
		return
	}
	now := time.Now()
	for _, job := range store.Jobs {
		if job == nil || job.ID == "" {
			continue
		}
		job.ConsoleTail = ""
		if !dataMigrationStateIsTerminal(job.State) {
			job.State = DataMigrationStateFailed
			job.Error = dataMigrationInterrupted
			job.FinishedAt = &now
			job.UpdatedAt = now
			code := -1
			job.ExitCode = &code
		}
		job.PID = 0
		if job.Stats.Phase == "" {
			job.Stats.Phase = job.Phase
		}
		t := &dataMigrationTask{job: job, done: make(chan struct{})}
		close(t.done)
		m.tasks[job.ID] = t
	}
	if len(store.Jobs) > 0 {
		log.Warnf("restored %d data migration job(s) from %s", len(m.tasks), m.storePath())
	}
}

// Close stops every running worker and marks it interrupted. It is called
// from Topom.Close; it never blocks longer than a few seconds per worker.
func (m *DataMigrationManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	var running []*dataMigrationTask
	for _, t := range m.tasks {
		t.mu.Lock()
		if !dataMigrationStateIsTerminal(t.job.State) {
			t.shutdown = true
			running = append(running, t)
		}
		t.mu.Unlock()
	}
	m.mu.Unlock()
	// Stop every worker at once so shutdown costs one grace period, not one
	// per job (Topom.Close holds its lock while we run).
	var wg sync.WaitGroup
	for _, t := range running {
		wg.Add(1)
		go func(t *dataMigrationTask) {
			defer wg.Done()
			_ = m.stopTask(t, 5*time.Second)
		}(t)
	}
	wg.Wait()
}

// RunningCount reports the number of non-terminal jobs (tests).
func (m *DataMigrationManager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.tasks {
		t.mu.Lock()
		if !dataMigrationStateIsTerminal(t.job.State) {
			n++
		}
		t.mu.Unlock()
	}
	return n
}
