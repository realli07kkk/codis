// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/migration"
	"github.com/CodisLabs/codis/pkg/models"
)

// The manager tests re-execute this test binary as the codis-shake worker:
// the manager spawns <test binary> -test.run=^TestDataMigrationHelperProcess$ -- <spec>
// and the test below behaves like a RedisShake worker (status HTTP server on
// the spec's status port, graceful exit on SIGTERM, exit 3 when the task name
// contains "fail", natural exit 0 for one-shot modes).
func TestDataMigrationHelperProcess(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "-test.run=^TestDataMigrationHelperProcess$") {
		t.Skip("data migration helper process")
	}
	specPath := ""
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			specPath = os.Args[i+1]
		}
	}
	if specPath == "" {
		os.Exit(2)
	}
	spec, err := migration.LoadSpec(specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(2)
	}
	if strings.Contains(spec.Name, "fail") {
		fmt.Fprintln(os.Stderr, "simulated worker crash: dial failed. address=[127.0.0.1:1]")
		os.Exit(3)
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(specPath), migration.LogFileName),
		[]byte("INF codis-shake start. id=["+spec.ID+"]\nINF start syncing...\n"), 0644)
	fmt.Println("helper worker started", spec.ID)

	var hits int64
	phases := []string{"receiving rdb", "syncing rdb", "syncing aof"}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		status := map[string]any{
			"consistent": false,
			"total_entries_count": map[string]any{
				"read_count": uint64(n * 100), "read_ops": 100.5,
				"write_count": uint64(n * 90), "write_ops": 90.5,
			},
			"writer": map[string]any{"name": "writer_target", "unanswered_entries": 0, "unanswered_bytes": 0},
		}
		switch spec.Mode {
		case migration.ModeScan:
			status["reader"] = map[string]any{"name": "reader_src", "scan_finished": n >= 2}
			status["consistent"] = n >= 3
		default:
			idx := int(n - 1)
			if idx >= len(phases) {
				idx = len(phases) - 1
			}
			lag := int64(300 - n*100)
			if lag < 0 {
				lag = 0
			}
			status["reader"] = []any{map[string]any{
				"name": "reader_src", "status": phases[idx],
				"rdb_file_size_bytes": 1000, "rdb_received_bytes": 1000, "rdb_sent_bytes": 1000,
				"aof_received_offset": n * 1000, "aof_sent_offset": n*1000 - lag,
			}}
			status["consistent"] = lag == 0
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", spec.Advanced.StatusPort))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(2)
	}
	go func() { _ = http.Serve(ln, mux) }()

	if spec.OneShot() {
		time.Sleep(1500 * time.Millisecond)
		fmt.Println("all done")
		os.Exit(0)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	fmt.Println("got signal, exiting")
	os.Exit(0)
}

func newTestDataMigrationManager(t *testing.T) (*Topom, *DataMigrationManager) {
	t.Helper()
	s := openTopom(t)
	c := *s.config
	c.DataMigrationEnabled = true
	c.DataMigrationWorkspace = t.TempDir()
	c.DataMigrationShakeBin = mustTestBinary(t)
	c.DataMigrationMaxConcurrentJobs = 2
	c.DataMigrationMaxJobsRetained = 10
	s.config = &c
	m := newDataMigrationManager(&c, s)
	m.pollInterval = 200 * time.Millisecond
	m.stopGrace = 3 * time.Second
	m.startTimeout = 5 * time.Second
	m.spawnArgs = func(specPath string) []string {
		return []string{"-test.run=^TestDataMigrationHelperProcess$", "--", specPath}
	}
	t.Cleanup(m.Close)
	return s, m
}

func mustTestBinary(t *testing.T) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

func waitForDataMigrationState(t *testing.T, m *DataMigrationManager, id string, terminal bool) *DataMigrationJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		job, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if dataMigrationStateIsTerminal(job.State) == terminal {
			return job
		}
		time.Sleep(100 * time.Millisecond)
	}
	job, _ := m.Get(id)
	t.Fatalf("job %s did not reach terminal=%v state, now %s (%s)", id, terminal, job.State, job.Phase)
	return nil
}

func waitDataMigrationPhase(t *testing.T, m *DataMigrationManager, id, phase string) *DataMigrationJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		job, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Phase == phase {
			return job
		}
		if dataMigrationStateIsTerminal(job.State) {
			t.Fatalf("job reached %s before phase %s", job.State, phase)
		}
		time.Sleep(100 * time.Millisecond)
	}
	job, _ := m.Get(id)
	t.Fatalf("job %s did not reach phase %q, now %q", id, phase, job.Phase)
	return nil
}

func standaloneEndpoint(address string) DataMigrationEndpointRequest {
	return DataMigrationEndpointRequest{Kind: migration.EndpointStandalone, Address: address}
}

func TestDataManagerSyncLifecycle(t *testing.T) {
	_, m := newTestDataMigrationManager(t)
	job, err := m.Create(&DataMigrationCreateRequest{
		Name:   "standalone to standalone",
		Mode:   migration.ModeSync,
		Source: standaloneEndpoint("127.0.0.1:6379"),
		Target: standaloneEndpoint("127.0.0.1:6380"),
		Filter: migration.Filter{BlockCommand: []string{"FLUSHALL"}},

		// 0 means "do not override the RedisShake default", so pass the
		// console defaults explicitly to check that they round-trip.
		TargetRedisMaxQPS:  300000,
		PipelineCountLimit: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != DataMigrationStateStarting && job.State != DataMigrationStatePending {
		t.Fatalf("initial state = %s", job.State)
	}
	if job.PID == 0 || job.StartedAt == nil {
		t.Fatalf("pid / started_at not recorded: %+v", job)
	}
	if job.Source.Kind != migration.EndpointStandalone || job.Source.Address != "127.0.0.1:6379" || job.Source.HasPassword {
		t.Fatalf("source view = %+v", job.Source)
	}
	if job.RestoreBehavior != migration.RestoreRewrite || job.TargetRedisMaxQPS != 300000 || job.PipelineCountLimit != 1024 {
		t.Fatalf("restore/qps/pipeline not recorded: behavior=%q qps=%d pipeline=%d", job.RestoreBehavior, job.TargetRedisMaxQPS, job.PipelineCountLimit)
	}

	running := waitDataMigrationPhase(t, m, job.ID, "syncing aof")
	if running.State != DataMigrationStateRunning {
		t.Fatalf("state = %s, want running", running.State)
	}
	if running.Stats.Shards != 1 || running.Stats.RdbTotalBytes != 1000 || running.Stats.RdbReceivedBytes != 1000 {
		t.Fatalf("stats = %+v", running.Stats)
	}
	if running.Stats.AofLagBytes == nil || *running.Stats.AofLagBytes != 0 {
		t.Fatalf("aof lag = %v, want 0 once consistent", running.Stats.AofLagBytes)
	}
	if !running.Stats.Consistent || running.Stats.ConsistentShards != 1 {
		t.Fatalf("consistent = %v shards = %d", running.Stats.Consistent, running.Stats.ConsistentShards)
	}
	if running.Stats.WriteCount == 0 || running.Stats.Phase != "syncing aof" {
		t.Fatalf("stats = %+v", running.Stats)
	}
	if len(running.Reader) == 0 || running.Reader[0] != '[' {
		t.Fatalf("reader status = %s", running.Reader)
	}

	if len(m.List()) != 1 || m.List()[0].ID != job.ID {
		t.Fatalf("List = %+v", m.List())
	}

	page, err := m.Logs(job.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.File != migration.LogFileName || !strings.Contains(page.Text, "start syncing") {
		t.Fatalf("log page = %+v", page)
	}
	if page.NextOffset != page.Size || page.Truncated {
		t.Fatalf("log offsets = %+v", page)
	}
	second, err := m.Logs(job.ID, page.NextOffset, 0)
	if err != nil || second.Text != "" || second.NextOffset != page.Size {
		t.Fatalf("log tail = %+v, err = %v", second, err)
	}
	reset, err := m.Logs(job.ID, 1<<40, 0)
	if err != nil || reset.Offset != 0 || !strings.Contains(reset.Text, "start syncing") {
		t.Fatalf("log beyond size = %+v, err = %v", reset, err)
	}
	small, err := m.Logs(job.ID, 0, 10)
	if err != nil || len(small.Text) != 10 || !small.Truncated {
		t.Fatalf("small page = %+v, err = %v", small, err)
	}

	if err := m.Remove(job.ID); err == nil {
		t.Fatal("removing a running job must fail")
	}
	if err := m.Stop(job.ID); err != nil {
		t.Fatal(err)
	}
	stopped := waitForDataMigrationState(t, m, job.ID, true)
	if stopped.State != DataMigrationStateStopped || stopped.Error != "" {
		t.Fatalf("stopped job = %s (%q)", stopped.State, stopped.Error)
	}
	if stopped.ExitCode == nil || *stopped.ExitCode != 0 || stopped.FinishedAt == nil || stopped.PID != 0 {
		t.Fatalf("stopped job fields = %+v", stopped)
	}
	detail, err := m.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.ConsoleTail, "helper worker started") || !strings.Contains(detail.ConsoleTail, "got signal") {
		t.Fatalf("console tail = %q", detail.ConsoleTail)
	}
	jobDir := detail.Dir
	if _, err := os.Stat(filepath.Join(jobDir, migration.SpecFileName)); err != nil {
		t.Fatalf("spec.json missing: %v", err)
	}
	if err := m.Remove(job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jobDir); !os.IsNotExist(err) {
		t.Fatalf("job dir still exists after Remove: %v", err)
	}
	if _, err := m.Get(job.ID); err == nil {
		t.Fatal("Get after Remove must fail")
	}
	if _, err := os.Stat(filepath.Join(m.Workspace(), dataMigrationStoreFileName)); err != nil {
		t.Fatalf("jobs.json not persisted: %v", err)
	}
}

func TestDataManagerOneShotSucceeds(t *testing.T) {
	_, m := newTestDataMigrationManager(t)
	job, err := m.Create(&DataMigrationCreateRequest{
		Name:   "scan once",
		Mode:   migration.ModeScan,
		Source: DataMigrationEndpointRequest{Kind: migration.EndpointCluster, Address: "127.0.0.1:7000"},
		Target: DataMigrationEndpointRequest{Kind: migration.EndpointStandalone, Address: "127.0.0.1:6380", Password: "pw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := waitForDataMigrationState(t, m, job.ID, true)
	if done.State != DataMigrationStateSucceeded {
		t.Fatalf("state = %s, error = %q", done.State, done.Error)
	}
	if done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("exit code = %v", done.ExitCode)
	}
	if done.Stats.AofLagBytes != nil {
		t.Fatalf("one-shot jobs must not report aof lag: %+v", done.Stats)
	}
	if !done.Target.HasPassword {
		t.Fatalf("target view = %+v", done.Target)
	}
	// Omitted advanced limits must stay 0: that is the documented "use the
	// RedisShake default" marker, so the manager must not invent values.
	if done.TargetRedisMaxQPS != 0 || done.PipelineCountLimit != 0 {
		t.Fatalf("advanced limits = qps %d pipeline %d, want 0/0", done.TargetRedisMaxQPS, done.PipelineCountLimit)
	}
}

func TestDataManagerWorkerFailure(t *testing.T) {
	_, m := newTestDataMigrationManager(t)
	job, err := m.Create(&DataMigrationCreateRequest{
		Name:   "fail-fast task",
		Mode:   migration.ModeRDB,
		Source: standaloneEndpoint("127.0.0.1:6379"),
		Target: standaloneEndpoint("127.0.0.1:6380"),
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForDataMigrationState(t, m, job.ID, true)
	if failed.State != DataMigrationStateFailed {
		t.Fatalf("state = %s", failed.State)
	}
	if failed.ExitCode == nil || *failed.ExitCode != 3 {
		t.Fatalf("exit code = %v, want 3", failed.ExitCode)
	}
	if !strings.Contains(failed.Error, "code 3") {
		t.Fatalf("error = %q", failed.Error)
	}
	detail, _ := m.Get(job.ID)
	if !strings.Contains(detail.ConsoleTail, "simulated worker crash") {
		t.Fatalf("console tail = %q", detail.ConsoleTail)
	}
	// Stopping a terminal job is a no-op.
	if err := m.Stop(job.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDataManagerConcurrentLimit(t *testing.T) {
	_, m := newTestDataMigrationManager(t)
	m.maxConcurrent = 1
	first, err := m.Create(&DataMigrationCreateRequest{
		Name: "first", Mode: migration.ModeSync,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(&DataMigrationCreateRequest{
		Name: "second", Mode: migration.ModeSync,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	}); err == nil || !strings.Contains(err.Error(), "too many running") {
		t.Fatalf("expected concurrency error, got %v", err)
	}
	if m.RunningCount() != 1 {
		t.Fatalf("running count = %d", m.RunningCount())
	}
	if err := m.Stop(first.ID); err != nil {
		t.Fatal(err)
	}
	waitForDataMigrationState(t, m, first.ID, true)
	if m.RunningCount() != 0 {
		t.Fatalf("running count after stop = %d", m.RunningCount())
	}
}

func TestDataManagerRejectsInvalidRequests(t *testing.T) {
	_, m := newTestDataMigrationManager(t)
	cases := []struct {
		name string
		req  *DataMigrationCreateRequest
		want string
	}{
		{"missing name", &DataMigrationCreateRequest{
			Mode: migration.ModeSync, Source: standaloneEndpoint("a:1"), Target: standaloneEndpoint("b:2"),
		}, "missing name"},
		{"invalid mode", &DataMigrationCreateRequest{
			Name: "x", Mode: "copy", Source: standaloneEndpoint("a:1"), Target: standaloneEndpoint("b:2"),
		}, `invalid mode "copy"`},
		{"missing address", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync, Source: standaloneEndpoint(""), Target: standaloneEndpoint("b:2"),
		}, "source address"},
		{"bad kind", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync, Source: DataMigrationEndpointRequest{Kind: "codis", Address: "a:1"}, Target: standaloneEndpoint("b:2"),
		}, "no group owns any slot"},
		{"same address", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync, Source: standaloneEndpoint("A:1"), Target: standaloneEndpoint("a:1"),
		}, "same address"},
		{"shared shard address", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync,
			Source: DataMigrationEndpointRequest{Kind: "codis", CodisScope: "manual", Shards: []DataMigrationShardRequest{{Address: "b:2"}}},
			Target: standaloneEndpoint("B:2"),
		}, "both contain server b:2"},
		{"manual target uncovered", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync, Source: standaloneEndpoint("a:1"),
			Target: DataMigrationEndpointRequest{Kind: "codis", CodisScope: "manual",
				Shards: []DataMigrationShardRequest{{Address: "c:3", SlotStart: 0, SlotEnd: 10}}},
		}, "not covered"},
		{"manual source without shards", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync,
			Source: DataMigrationEndpointRequest{Kind: "codis", CodisScope: "manual"},
			Target: standaloneEndpoint("b:2"),
		}, "at least one shard"},
		{"bad restore behavior", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync, RestoreBehavior: "ignore",
			Source: standaloneEndpoint("a:1"), Target: standaloneEndpoint("b:2"),
		}, `invalid restore_behavior`},
		{"bad codis scope", &DataMigrationCreateRequest{
			Name: "x", Mode: migration.ModeSync,
			Source: DataMigrationEndpointRequest{Kind: "codis", CodisScope: "whatever"},
			Target: standaloneEndpoint("b:2"),
		}, "invalid codis_scope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.Create(tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
	if len(m.List()) != 0 {
		t.Fatalf("rejected requests must not create jobs: %+v", m.List())
	}
}

func TestDataMigrationCodisTopology(t *testing.T) {
	s, m := newTestDataMigrationManager(t)
	if err := s.CreateGroup(1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroup(2); err != nil {
		t.Fatal(err)
	}
	if err := s.GroupAddServer(1, "", "10.0.0.1:6379"); err != nil {
		t.Fatal(err)
	}
	if err := s.GroupAddServer(1, "", "10.0.0.2:6379"); err != nil {
		t.Fatal(err)
	}
	if err := s.GroupAddServer(2, "", "10.0.0.3:6379"); err != nil {
		t.Fatal(err)
	}
	var slots []*models.SlotMapping
	for sid := 0; sid < models.MaxSlotNum; sid++ {
		gid := 1
		if sid >= 512 {
			gid = 2
		}
		if sid == 1000 {
			continue // leave one slot unassigned
		}
		slots = append(slots, &models.SlotMapping{Id: sid, GroupId: gid})
	}
	if err := s.SlotsAssignGroup(slots); err != nil {
		t.Fatal(err)
	}

	topology, err := m.Topology()
	if err != nil {
		t.Fatal(err)
	}
	if !topology.Online || topology.Complete || topology.UnassignedSlots != 1 || topology.SlotActions != 0 {
		t.Fatalf("topology = %+v", topology)
	}
	if len(topology.Groups) != 2 || topology.Groups[0].Master != "10.0.0.1:6379" || topology.Groups[0].SlotCount != 512 {
		t.Fatalf("groups = %+v", topology.Groups)
	}
	if r := topology.Groups[1].Slots; len(r) != 2 || r[0].Start != 512 || r[0].End != 999 || r[1].Start != 1001 || r[1].End != 1023 {
		t.Fatalf("slot ranges = %+v", r)
	}

	// Source: groups owning slots, masters only, backend auth from config.
	endpoint, err := m.resolveEndpoint("source", DataMigrationEndpointRequest{Kind: "codis"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Kind != migration.EndpointStatic || len(endpoint.Shards) != 2 || endpoint.Shards[0].Address != "10.0.0.1:6379" {
		t.Fatalf("source endpoint = %+v", endpoint)
	}
	if endpoint.Username != s.config.BackendAuthUsername || endpoint.Password != s.config.ProductAuth {
		t.Fatalf("backend auth not applied: %+v", endpoint)
	}
	// Target: every slot must be assigned.
	if _, err := m.resolveEndpoint("target", DataMigrationEndpointRequest{Kind: "codis"}, true); err == nil ||
		!strings.Contains(err.Error(), "not assigned to any group") {
		t.Fatalf("expected unassigned slot error, got %v", err)
	}
	// Manual target needs full coverage; manual source only needs addresses.
	manual, err := m.resolveEndpoint("target", DataMigrationEndpointRequest{Kind: "codis", CodisScope: "manual", Username: "u", Password: "p",
		Shards: []DataMigrationShardRequest{{Address: "x:1", SlotStart: 0, SlotEnd: 1023}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.ValidateSlotCoverage(manual.Shards); err != nil {
		t.Fatalf("manual target coverage: %v", err)
	}
	manualSrc, err := m.resolveEndpoint("source", DataMigrationEndpointRequest{Kind: "codis", CodisScope: "manual",
		Shards: []DataMigrationShardRequest{{Address: "x:1"}, {Address: "y:2"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(manualSrc.Shards) != 2 || len(manualSrc.Shards[0].Slots) != 0 {
		t.Fatalf("manual source = %+v", manualSrc)
	}

	// Snapshot-level rules: slot actions, missing masters, no groups.
	snap := &codisTopologySnapshot{byGroup: map[int][]int{}, groups: []DataMigrationTopologyGroup{{GroupID: 1, Master: "a:1"}}}
	if _, err := snap.shards(false); err != nil || len(snap.groups) != 1 {
		t.Fatalf("clean snapshot: %v", err)
	}
	snap.actions = 2
	if _, err := snap.shards(false); err == nil || !strings.Contains(err.Error(), "slot migration action") {
		t.Fatalf("expected slot action error, got %v", err)
	}
	snap.actions = 0
	snap.unassigned = 5
	if _, err := snap.shards(false); err != nil {
		t.Fatalf("unassigned slots only matter for the target: %v", err)
	}
	if _, err := snap.shards(true); err == nil {
		t.Fatal("expected unassigned slot error for target")
	}
	snap.unassigned = 0
	snap.missingMaster = []int{1}
	if _, err := snap.shards(false); err == nil || !strings.Contains(err.Error(), "has no server") {
		t.Fatalf("expected missing master error, got %v", err)
	}
	empty := &codisTopologySnapshot{byGroup: map[int][]int{}}
	if _, err := empty.shards(false); err == nil {
		t.Fatal("expected no group error")
	}
	if empty.topology().Complete {
		t.Fatal("empty topology must not be complete")
	}
}

func TestDataMigrationStatsParsing(t *testing.T) {
	var reply shakeStatusReply
	raw := `{"consistent":true,"total_entries_count":{"read_count":10,"read_ops":1.5,"write_count":9,"write_ops":1.25},
		"reader":[{"name":"reader_a","status":"syncing aof","rdb_file_size_bytes":100,"rdb_received_bytes":100,"rdb_sent_bytes":100,"aof_received_offset":500,"aof_sent_offset":500},
		          {"name":"reader_b","status":"syncing rdb","rdb_file_size_bytes":300,"rdb_received_bytes":300,"rdb_sent_bytes":150,"aof_received_offset":400,"aof_sent_offset":100}],
		"writer":[{"name":"writer_x"}]}`
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	stats := dataMigrationStats(&reply, false)
	if stats.Phase != "syncing aof" || stats.Shards != 2 || stats.ConsistentShards != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.RdbTotalBytes != 400 || stats.RdbReceivedBytes != 400 {
		t.Fatalf("rdb bytes = %+v", stats)
	}
	if stats.AofLagBytes == nil || *stats.AofLagBytes != 300 {
		t.Fatalf("lag = %v", stats.AofLagBytes)
	}
	if !stats.Consistent || stats.ReadCount != 10 || stats.WriteOps != 1.25 {
		t.Fatalf("stats = %+v", stats)
	}
	if string(reply.Writer) == "" {
		t.Fatal("writer status not captured")
	} else if stats.Shards != 2 || stats.Phase != "syncing aof" {
		t.Fatalf("writer status must not be counted as a reader: %+v", stats)
	}

	scan := shakeStatusReply{}
	if err := json.Unmarshal([]byte(`{"reader":{"name":"reader_a","scan_finished":false}}`), &scan); err != nil {
		t.Fatal(err)
	}
	if got := dataMigrationStats(&scan, true); got.Phase != "scanning" || got.Shards != 1 || got.AofLagBytes != nil {
		t.Fatalf("scan stats = %+v", got)
	}
	scan.Reader = json.RawMessage(`{"name":"reader_a","scan_finished":true}`)
	if got := dataMigrationStats(&scan, true); got.Phase != "draining" || got.ConsistentShards != 1 {
		t.Fatalf("draining stats = %+v", got)
	}
	if got := dataMigrationStats(&shakeStatusReply{}, false); got.Phase != "" || got.Shards != 0 {
		t.Fatalf("empty stats = %+v", got)
	}
	if dataMigrationPhase(dataMigrationReaderStats(json.RawMessage(`{"status":"waiting bgsave"}`))) != "waiting bgsave" {
		t.Fatal("single object reader status not parsed")
	}
}

func TestDataMigrationPersistence(t *testing.T) {
	s, m := newTestDataMigrationManager(t)
	c := s.config
	job, err := m.Create(&DataMigrationCreateRequest{
		Name: "persisted", Mode: migration.ModeScan,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForDataMigrationState(t, m, job.ID, true)

	// A new manager on the same workspace restores the terminal job.
	again := newDataMigrationManager(c, s)
	jobs := again.List()
	if len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].State != DataMigrationStateSucceeded {
		t.Fatalf("restored jobs = %+v", jobs)
	}
	if jobs[0].PID != 0 {
		t.Fatalf("restored pid = %d", jobs[0].PID)
	}
	b, err := os.ReadFile(again.storePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), s.config.ProductAuth) {
		t.Fatal("persisted store must not contain credentials")
	}

	// A job that was running when the dashboard went down is marked failed.
	store := dataMigrationStore{Jobs: []*DataMigrationJob{{
		ID: "dm-stale", Name: "stale", Mode: migration.ModeSync, State: DataMigrationStateRunning, PID: 4242,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}}
	b, _ = json.Marshal(store)
	if err := os.WriteFile(again.storePath(), b, 0600); err != nil {
		t.Fatal(err)
	}
	third := newDataMigrationManager(c, s)
	stale, err := third.Get("dm-stale")
	if err != nil {
		t.Fatal(err)
	}
	if stale.State != DataMigrationStateFailed || !strings.Contains(stale.Error, "cannot resume") || stale.PID != 0 || stale.FinishedAt == nil {
		t.Fatalf("stale job = %+v", stale)
	}
	if err := third.Remove("dm-stale"); err != nil {
		t.Fatal(err)
	}
	if len(third.List()) != 0 {
		t.Fatalf("jobs after remove = %+v", third.List())
	}
}

func TestDataMigrationRetentionAndClose(t *testing.T) {
	s, m := newTestDataMigrationManager(t)
	m.maxRetained = 1
	for i := 0; i < 3; i++ {
		job, err := m.Create(&DataMigrationCreateRequest{
			Name: fmt.Sprintf("retention %d", i), Mode: migration.ModeScan,
			Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
		})
		if err != nil {
			t.Fatal(err)
		}
		waitForDataMigrationState(t, m, job.ID, true)
	}
	if len(m.List()) != 1 || m.List()[0].Name != "retention 2" {
		t.Fatalf("retention did not keep only the newest job: %+v", m.List())
	}

	// Close interrupts running workers and refuses new jobs.
	job, err := m.Create(&DataMigrationCreateRequest{
		Name: "interrupted", Mode: migration.ModeSync,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDataMigrationPhase(t, m, job.ID, "receiving rdb")
	m.Close()
	interrupted := waitForDataMigrationState(t, m, job.ID, true)
	if interrupted.State != DataMigrationStateFailed || !strings.Contains(interrupted.Error, "cannot resume") {
		t.Fatalf("interrupted job = %s %q", interrupted.State, interrupted.Error)
	}
	if _, err := m.Create(&DataMigrationCreateRequest{
		Name: "after close", Mode: migration.ModeScan,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	}); err == nil {
		t.Fatal("Create after Close must fail")
	}
	m.Close() // idempotent
	_ = s
}

func TestDataMigrationSlotRanges(t *testing.T) {
	ranges := dataMigrationSlotRanges([]int{0, 1, 2, 5, 7, 8, 1023})
	want := []migration.SlotRange{
		{Start: 0, End: 2}, {Start: 5, End: 5}, {Start: 7, End: 8}, {Start: 1023, End: 1023},
	}
	if len(ranges) != len(want) {
		t.Fatalf("ranges = %+v", ranges)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("ranges = %+v, want %+v", ranges, want)
		}
	}
	if len(dataMigrationSlotRanges(nil)) != 0 {
		t.Fatal("nil slots must yield no ranges")
	}
}

func TestDataMigrationBinPreflight(t *testing.T) {
	_, m := newTestDataMigrationManager(t)
	m.shakeBin = filepath.Join(t.TempDir(), "missing-codis-shake")
	if _, err := m.Create(&DataMigrationCreateRequest{
		Name: "x", Mode: migration.ModeScan,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	}); err == nil || !strings.Contains(err.Error(), "not found or not executable") {
		t.Fatalf("expected binary preflight error, got %v", err)
	}
	m.enabled = false
	if _, err := m.Create(&DataMigrationCreateRequest{
		Name: "x", Mode: migration.ModeScan,
		Source: standaloneEndpoint("127.0.0.1:6379"), Target: standaloneEndpoint("127.0.0.1:6380"),
	}); err == nil || err.Error() != "data migration is disabled" {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

// The helper must really be re-executable as a worker: run it once through
// exec with a spec and make sure it reports the status port.
func TestDataMigrationHelperProcessIsExecable(t *testing.T) {
	dir := t.TempDir()
	port, err := freeLoopbackPort()
	if err != nil {
		t.Fatal(err)
	}
	spec := &migration.Spec{
		ID: "helper-check", Name: "fail on purpose", Mode: migration.ModeSync,
		Source:   migration.Endpoint{Kind: migration.EndpointStandalone, Address: "127.0.0.1:6379"},
		Target:   migration.Endpoint{Kind: migration.EndpointStandalone, Address: "127.0.0.1:6380"},
		Advanced: migration.Advanced{StatusPort: port, RestoreBehavior: migration.RestoreRewrite},
	}
	b, _ := json.Marshal(spec)
	path := filepath.Join(dir, migration.SpecFileName)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(mustTestBinary(t), "-test.run=^TestDataMigrationHelperProcess$", "--", path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit error, output:\n%s", out)
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
		t.Fatalf("exit = %v, output:\n%s", err, out)
	}
	if !strings.Contains(string(out), "simulated worker crash") {
		t.Fatalf("output = %s", out)
	}
}
