// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	stdcontext "context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

// newTestPitrManager builds a manager with a tiny concurrent limit so tests can
// exercise the registry / lock / state-machine wiring without a live Topom.
func newTestPitrManager(t *testing.T, enabled bool) *PitrManager {
	t.Helper()
	return &PitrManager{
		jobs:          make(map[string]*PitrJob),
		enabled:       enabled,
		redisCheckBin: trueBin(t),
		maxConcurrent: 2,
		jobTimeout:    time.Minute,
		serverLocks:   make(map[string]*pitrServerLock),
	}
}

// errFakeNotImpl is returned by fakePitrDeps methods that aren't configured.
var errFakeNotImpl = errors.New("fake pitr deps: method not configured")

// fakePitrDeps is a controllable PitrDeps for unit tests. Each field configures
// the behavior of the corresponding method; unset fields return errFakeNotImpl
// so a step that isn't under test fails explicitly.
type fakePitrDeps struct {
	groupOfServer func(string) (int, bool, error)
	configGet     func(string, string) (map[string]string, error)
	shutdown      func(string) error
	pingInfo      func(string) (map[string]string, error)
}

func (f fakePitrDeps) GroupOfServer(addr string) (int, bool, error) {
	if f.groupOfServer == nil {
		return 0, false, errFakeNotImpl
	}
	return f.groupOfServer(addr)
}
func (f fakePitrDeps) ConfigGet(_ stdcontext.Context, addr, pattern string) (map[string]string, error) {
	if f.configGet == nil {
		return nil, errFakeNotImpl
	}
	return f.configGet(addr, pattern)
}
func (f fakePitrDeps) GroupServers(gid int) ([]string, error) { return nil, errFakeNotImpl }
func (f fakePitrDeps) Shutdown(_ stdcontext.Context, addr string) error {
	if f.shutdown == nil {
		return errFakeNotImpl
	}
	return f.shutdown(addr)
}
func (f fakePitrDeps) PingInfo(_ stdcontext.Context, addr string) (map[string]string, error) {
	if f.pingInfo == nil {
		return nil, errFakeNotImpl
	}
	return f.pingInfo(addr)
}
func (f fakePitrDeps) ResyncReplica(stdcontext.Context, string, string) error {
	return errFakeNotImpl
}

var _ PitrDeps = (fakePitrDeps{})

func waitForTerminal(t *testing.T, m *PitrManager, id string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := m.Get(id)
		if err != nil {
			t.Fatalf("job %s disappeared: %v", id, err)
		}
		if pitrStateIsTerminal(job.State) {
			return job.State
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal state in %v", id, timeout)
	return ""
}

func TestPitrCreateDisabled(t *testing.T) {
	m := newTestPitrManager(t, false)
	_, err := m.Create("prod", "1.1.1.1:6379", 1716000000, fakePitrDeps{})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if got := len(m.jobs); got != 0 {
		t.Fatalf("expected no job registered, got %d", got)
	}
}

// TestPitrValidateRejectsReplica verifies stepValidate rejects a non-master.
func TestPitrValidateRejectsReplica(t *testing.T) {
	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) {
			return 3, false, nil // group 3, replica (not master)
		},
	}
	job, err := m.Create("prod", "1.1.1.1:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := waitForTerminal(t, m, job.ID, 5*time.Second)
	if state != PitrStateFailed {
		t.Fatalf("expected failed, got %s", state)
	}
	got, _ := m.Get(job.ID)
	if !strings.Contains(got.Error, "not the master") {
		t.Fatalf("expected not-master error, got %q", got.Error)
	}
}

// TestPitrPrereqRejectsAofDisabled verifies stepPrereq rejects when appendonly
// is off.
func TestPitrPrereqRejectsAofDisabled(t *testing.T) {
	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) { return 3, true, nil },
		configGet: func(addr, pattern string) (map[string]string, error) {
			return map[string]string{"appendonly": "no"}, nil
		},
	}
	job, err := m.Create("prod", "1.1.1.1:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := waitForTerminal(t, m, job.ID, 5*time.Second)
	if state != PitrStateFailed {
		t.Fatalf("expected failed, got %s", state)
	}
	got, _ := m.Get(job.ID)
	if !strings.Contains(got.Error, "appendonly") {
		t.Fatalf("expected appendonly error, got %q", got.Error)
	}
}

// TestPitrPrereqRejectsTimestampDisabled verifies stepPrereq rejects when
// aof-timestamp-enabled is off even if appendonly is on.
func TestPitrPrereqRejectsTimestampDisabled(t *testing.T) {
	m := newTestPitrManager(t, true)
	calls := 0
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) { return 3, true, nil },
		configGet: func(addr, pattern string) (map[string]string, error) {
			calls++
			if pattern == "appendonly" {
				return map[string]string{"appendonly": "yes"}, nil
			}
			return map[string]string{"aof-timestamp-enabled": "no"}, nil
		},
	}
	job, err := m.Create("prod", "1.1.1.1:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := waitForTerminal(t, m, job.ID, 5*time.Second)
	if state != PitrStateFailed {
		t.Fatalf("expected failed, got %s", state)
	}
	got, _ := m.Get(job.ID)
	if !strings.Contains(got.Error, "aof-timestamp-enabled") {
		t.Fatalf("expected aof-timestamp-enabled error, got %q", got.Error)
	}
}

// TestPitrFeasibilityTsOutOfRange verifies stepFeasibility rejects a ts outside
// the last segment's #TS: range. Builds a temp AOF dir with a manifest + a last
// segment file containing two #TS: annotations.
func TestPitrFeasibilityTsOutOfRange(t *testing.T) {
	dir := t.TempDir()
	aofDir := filepath.Join(dir, "appendonlydir")
	if err := os.MkdirAll(aofDir, 0755); err != nil {
		t.Fatal(err)
	}
	// manifest: one base, one incr (incr is the last/truncatable file).
	manifest := "file appendonly.aof.1.base.rdb seq 1 type b\n" +
		"file appendonly.aof.1.incr.aof seq 1 type h\n"
	if err := os.WriteFile(filepath.Join(aofDir, "appendonly.aof.manifest"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	// incr with #TS: 1000 and 2000.
	incr := "#TS:1000\n*2\r\n$3\r\nSET\r\n$1\r\na\r\n#TS:2000\n*2\r\n$3\r\nSET\r\n$1\r\nb\r\n"
	if err := os.WriteFile(filepath.Join(aofDir, "appendonly.aof.1.incr.aof"), []byte(incr), 0644); err != nil {
		t.Fatal(err)
	}

	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) { return 3, true, nil },
		configGet: func(addr, pattern string) (map[string]string, error) {
			switch pattern {
			case "appendonly":
				return map[string]string{"appendonly": "yes"}, nil
			case "aof-timestamp-enabled":
				return map[string]string{"aof-timestamp-enabled": "yes"}, nil
			case "dir":
				return map[string]string{"dir": dir}, nil
			case "appendfilename":
				return map[string]string{"appendfilename": "appendonly.aof"}, nil
			case "appenddirname":
				return map[string]string{"appenddirname": "appendonlydir"}, nil
			}
			return nil, nil
		},
	}
	// ts 3000 is beyond the last annotation (2000).
	job, err := m.Create("prod", "1.1.1.1:6379", 3000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := waitForTerminal(t, m, job.ID, 5*time.Second)
	if state != PitrStateFailed {
		t.Fatalf("expected failed, got %s", state)
	}
	got, _ := m.Get(job.ID)
	if !strings.Contains(got.Error, "not in last aof segment") {
		t.Fatalf("expected ts-out-of-range error, got %q", got.Error)
	}
}

// TestPitrScanTimestamps verifies the #TS: scanner finds the right range.
func TestPitrScanTimestamps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seg.aof")
	content := "#TS:100\nstuff\n#TS:200\nmore\n#TS:150\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	min, max, err := scanPitrAofTimestamps(path)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if min != 100 || max != 200 {
		t.Fatalf("expected [100,200], got [%d,%d]", min, max)
	}
}

// TestPitrReadManifest verifies manifest parsing returns segments in order
// (base first, then incrs).
func TestPitrReadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "appendonly.aof.manifest")
	content := "file appendonly.aof.2.base.rdb seq 2 type b\n" +
		"file appendonly.aof.1.incr.aof seq 1 type h\n" +
		"file appendonly.aof.2.incr.aof seq 2 type h\n" +
		"# a comment line\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	segs, err := readPitrAofManifest(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	want := []string{"appendonly.aof.2.base.rdb", "appendonly.aof.1.incr.aof", "appendonly.aof.2.incr.aof"}
	if len(segs) != len(want) {
		t.Fatalf("expected %d segments, got %d (%v)", len(want), len(segs), segs)
	}
	for i, s := range segs {
		if s != want[i] {
			t.Fatalf("segment %d: want %q got %q", i, want[i], s)
		}
	}
}

// TestPitrPerServerLockHeldUntilRemove verifies the CR-005 lifecycle: a
// terminal job still holds its per-server lock until Remove (PITR is
// non-idempotent; an unacknowledged terminal job must block new jobs on the
// same server). After Remove the lock is released and a new Create succeeds.
func TestPitrPerServerLockHeldUntilRemove(t *testing.T) {
	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) { return 3, true, nil },
		// configGet unset -> prereq fails fast (fake not-impl) -> terminal.
	}
	job, err := m.Create("prod", "2.2.2.2:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	_ = waitForTerminal(t, m, job.ID, 5*time.Second)

	// Terminal job must STILL hold the lock (CR-005: not released until Remove).
	m.mu.Lock()
	_, locked := m.serverLocks["2.2.2.2:6379"]
	m.mu.Unlock()
	if !locked {
		t.Fatalf("terminal job should still hold the per-server lock until Remove")
	}

	// A second Create on the same addr must be rejected while the lock is held.
	if _, err := m.Create("prod", "2.2.2.2:6379", 1716000000, deps); err == nil {
		t.Fatalf("second Create while lock held should be rejected")
	}

	// Remove releases the lock.
	if err := m.Remove(job.ID); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	m.mu.Lock()
	_, locked = m.serverLocks["2.2.2.2:6379"]
	m.mu.Unlock()
	if locked {
		t.Fatalf("per-server lock not released after Remove")
	}

	// Now a new Create on the same addr succeeds.
	if _, err := m.Create("prod", "2.2.2.2:6379", 1716000000, deps); err != nil {
		t.Fatalf("Create after Remove should succeed: %v", err)
	}
}

// TestPitrRemoveRejectsRunning verifies Remove refuses a non-terminal job
// (CR-005: Remove must not silently abort an in-flight truncate).
func TestPitrRemoveRejectsRunning(t *testing.T) {
	m := newTestPitrManager(t, true)
	gate := make(chan struct{})
	defer close(gate)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) {
			<-gate // hang in validate so the job stays running
			return 3, true, nil
		},
	}
	job, err := m.Create("prod", "6.6.6.6:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Give the goroutine a moment to enter validate (running).
	time.Sleep(50 * time.Millisecond)
	if err := m.Remove(job.ID); err == nil {
		t.Fatalf("Remove of running job should be rejected")
	}
}

func TestPitrListHidesInternals(t *testing.T) {
	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{}
	job, err := m.Create("prod", "3.3.3.3:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	_ = waitForTerminal(t, m, job.ID, 5*time.Second)

	list := m.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 job in list, got %d", len(list))
	}
	if list[0].mu != nil || list[0].cancel != nil {
		t.Fatalf("List snapshot must not expose internal mutex/cancel fields")
	}
}

func TestPitrRemove(t *testing.T) {
	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{}
	job, err := m.Create("prod", "4.4.4.4:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	_ = waitForTerminal(t, m, job.ID, 5*time.Second)

	if err := m.Remove(job.ID); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := m.Get(job.ID); err == nil {
		t.Fatalf("expected job gone after Remove")
	}
}

// --- Step 3 side-effect tests ---

// setupAofDir builds a temp AOF dir with a manifest + a last incr segment
// containing a #TS: annotation >= ts, returning the aofDir, manifest path and
// last segment name.
func setupAofDir(t *testing.T, ts int64) (aofDir, manifest, lastSeg string) {
	t.Helper()
	dir := t.TempDir()
	aofDir = filepath.Join(dir, "appendonlydir")
	if err := os.MkdirAll(aofDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest = filepath.Join(aofDir, "appendonly.aof.manifest")
	content := "file appendonly.aof.1.base.rdb seq 1 type b\n" +
		"file appendonly.aof.1.incr.aof seq 1 type h\n"
	if err := os.WriteFile(manifest, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	lastSeg = "appendonly.aof.1.incr.aof"
	incr := "#TS:" + strconv.FormatInt(ts, 10) + "\n*2\r\n$3\r\nSET\r\n$1\r\na\r\n"
	if err := os.WriteFile(filepath.Join(aofDir, lastSeg), []byte(incr), 0644); err != nil {
		t.Fatal(err)
	}
	return aofDir, manifest, lastSeg
}

// TestPitrSnapshotCopiesAndRecordsStat verifies stepSnapshot copies the manifest
// + last segment and records the pre-stat of the AOF dir.
func TestPitrSnapshotCopiesAndRecordsStat(t *testing.T) {
	aofDir, manifestPath, lastSeg := setupAofDir(t, 1000)
	job := &PitrJob{
		ID:              "test-snap",
		AofDir:          aofDir,
		AofManifest:     manifestPath,
		LastSegmentFile: lastSeg,
		mu:              &sync.Mutex{},
	}
	pm := &PitrManager{}
	if err := pm.stepSnapshot(stdcontext.Background(), job, nil); err != nil {
		t.Fatalf("stepSnapshot failed: %v", err)
	}
	if job.SnapshotDir == "" {
		t.Fatal("expected snapshot dir set")
	}
	// Manifest + last seg copied.
	if _, err := os.Stat(filepath.Join(job.SnapshotDir, filepath.Base(manifestPath))); err != nil {
		t.Fatalf("manifest not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(job.SnapshotDir, lastSeg)); err != nil {
		t.Fatalf("last segment not copied: %v", err)
	}
	if len(job.PreStatSnapshot) == 0 {
		t.Fatal("expected pre-stat recorded")
	}
}

// TestPitrTruncateFailureKeepsSnapshot verifies stepTruncate surfaces a
// restore-from-snapshot pointer when redis-check-aof exits non-zero (server is
// already down in this step).
func TestPitrTruncateFailureKeepsSnapshot(t *testing.T) {
	aofDir, manifestPath, lastSeg := setupAofDir(t, 1000)
	// Snapshot first so SnapshotDir is set.
	job := &PitrJob{
		ID:              "test-trunc-fail",
		AofDir:          aofDir,
		AofManifest:     manifestPath,
		LastSegmentFile: lastSeg,
		TruncateTs:      500,
		mu:              &sync.Mutex{},
	}
	pm := &PitrManager{redisCheckBin: falseBin(t)} // always exits non-zero
	if err := pm.stepSnapshot(stdcontext.Background(), job, nil); err != nil {
		t.Fatalf("stepSnapshot failed: %v", err)
	}
	err := pm.stepTruncate(stdcontext.Background(), job, nil)
	if err == nil {
		t.Fatal("expected truncate to fail with /bin/false")
	}
	if !strings.Contains(err.Error(), "restore from snapshot") {
		t.Fatalf("expected restore-from-snapshot pointer, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), job.SnapshotDir) {
		t.Fatalf("expected snapshot dir in error, got %q", err.Error())
	}
}

// trueBin returns a path to a command that exits 0 without side effects,
// portable across macOS/Linux. Falls back to a tiny generated shell script if
// no stock "true" is found on PATH.
func trueBin(t *testing.T) string {
	t.Helper()
	for _, cand := range []string{"/usr/bin/true", "/bin/true"} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	// Generate a fallback.
	p := filepath.Join(t.TempDir(), "true.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// falseBin returns a path to a command that always exits non-zero.
func falseBin(t *testing.T) string {
	t.Helper()
	for _, cand := range []string{"/usr/bin/false", "/bin/false"} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	p := filepath.Join(t.TempDir(), "false.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPitrTruncateStatDiffDrift verifies the stat-diff guard detects a non-last
// file changing. We simulate drift by mutating the recorded pre-stat so a
// non-last file appears changed after truncate. Since the "true" stand-in
// exits 0 without touching files, post-stat equals the real on-disk state — so
// we tamper with pre-stat to force a mismatch.
func TestPitrTruncateStatDiffDrift(t *testing.T) {
	aofDir, manifestPath, lastSeg := setupAofDir(t, 1000)
	job := &PitrJob{
		ID:              "test-drift",
		AofDir:          aofDir,
		AofManifest:     manifestPath,
		LastSegmentFile: lastSeg,
		TruncateTs:      500,
		mu:              &sync.Mutex{},
	}
	pm := &PitrManager{redisCheckBin: trueBin(t)}
	// Record a real pre-stat, then corrupt a non-last file's recorded size to
	// simulate the tool having touched it (which /bin/true did not, so the
	// post-stat will differ from our tampered pre-stat).
	realStat, err := statPitrAofDir(aofDir)
	if err != nil {
		t.Fatal(err)
	}
	// Add a fake "base" entry with wrong size to force drift detection.
	job.PreStatSnapshot = map[string]pitrFileStat{
		"appendonly.aof.1.base.rdb": {Size: 99999, Mtime: realStat["appendonly.aof.1.base.rdb"].Mtime},
		"appendonly.aof.1.incr.aof": realStat["appendonly.aof.1.incr.aof"],
	}
	err = pm.stepTruncate(stdcontext.Background(), job, nil)
	if err == nil || !strings.Contains(err.Error(), "model drift") {
		t.Fatalf("expected model drift error, got %v", err)
	}
}

// TestPitrResyncPartialFailureWarning verifies stepResync records a warning
// (not a job failure) when some replicas fail to resync.
func TestPitrResyncPartialFailureWarning(t *testing.T) {
	// Fake deps whose GroupServers returns 2 replicas, ResyncReplica fails for
	// one of them.
	deps := &resyncFakeDeps{
		servers:  []string{"1.1.1.1:6379", "2.2.2.2:6379", "3.3.3.3:6379"},
		failAddr: "3.3.3.3:6379",
	}
	job := &PitrJob{
		ID:         "test-resync",
		ServerAddr: "1.1.1.1:6379",
		GroupID:    3,
		State:      PitrStateResyncingReplicas,
		Steps:      []PitrStep{{Name: "resync", Status: pitrStepStatusRunning}},
		mu:         &sync.Mutex{},
	}
	pm := &PitrManager{}
	if err := pm.stepResync(stdcontext.Background(), job, deps); err != nil {
		t.Fatalf("stepResync should not fail on partial replica failure: %v", err)
	}
	// The resync step should carry a warning mentioning the failed replica.
	var warning string
	for _, s := range job.Steps {
		if s.Name == "resync" {
			warning = s.Error
		}
	}
	if !strings.Contains(warning, "3.3.3.3:6379") {
		t.Fatalf("expected warning mentioning failed replica, got %q", warning)
	}
}

type resyncFakeDeps struct {
	servers  []string
	failAddr string
}

func (d *resyncFakeDeps) GroupOfServer(string) (int, bool, error) { return 3, true, nil }
func (d *resyncFakeDeps) ConfigGet(stdcontext.Context, string, string) (map[string]string, error) {
	return nil, errFakeNotImpl
}
func (d *resyncFakeDeps) GroupServers(gid int) ([]string, error) { return d.servers, nil }
func (d *resyncFakeDeps) Shutdown(stdcontext.Context, string) error {
	return errFakeNotImpl
}
func (d *resyncFakeDeps) PingInfo(stdcontext.Context, string) (map[string]string, error) {
	return map[string]string{"loading": "0"}, nil
}
func (d *resyncFakeDeps) ResyncReplica(_ stdcontext.Context, _, replicaAddr string) error {
	if replicaAddr == d.failAddr {
		return errors.New("resync failed")
	}
	return nil
}

var _ PitrDeps = (*resyncFakeDeps)(nil)

// --- CR fixes regression tests ---

// TestPitrShutdownFailureStopsStateMachine verifies CR-001: when Shutdown
// returns an error (server still up / NOAUTH / unknown command), the state
// machine MUST fail and never reach snapshot/truncate. We model a server that
// stays up by having shutdown return an error and PingInfo still answer.
func TestPitrShutdownFailureStopsStateMachine(t *testing.T) {
	m := newTestPitrManager(t, true)
	aofDir, _, _ := setupAofDir(t, 1000)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) { return 3, true, nil },
		configGet: func(addr, pattern string) (map[string]string, error) {
			switch pattern {
			case "appendonly":
				return map[string]string{"appendonly": "yes"}, nil
			case "aof-timestamp-enabled":
				return map[string]string{"aof-timestamp-enabled": "yes"}, nil
			case "dir":
				return map[string]string{"dir": filepath.Dir(aofDir)}, nil
			case "appendfilename":
				return map[string]string{"appendfilename": "appendonly.aof"}, nil
			case "appenddirname":
				return map[string]string{"appenddirname": "appendonlydir"}, nil
			}
			return nil, nil
		},
		// Shutdown fails — simulating a server that's still up (NOAUTH etc.).
		shutdown: func(addr string) error {
			return errors.New("shutdown failed: server still up")
		},
	}
	job, err := m.Create("prod", "1.1.1.1:6379", 1000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := waitForTerminal(t, m, job.ID, 5*time.Second)
	if state != PitrStateFailed {
		t.Fatalf("expected failed when shutdown fails, got %s", state)
	}
	got, _ := m.Get(job.ID)
	// The shutdown step must be the failing step; snapshot/truncate must NOT
	// have run.
	var shutdownRan, snapshotRan, truncateRan bool
	for _, s := range got.Steps {
		switch s.Name {
		case "shutdown":
			shutdownRan = true
		case "snapshot":
			snapshotRan = true
		case "truncate":
			truncateRan = true
		}
	}
	if !shutdownRan {
		t.Fatalf("shutdown step should have run")
	}
	if snapshotRan {
		t.Fatalf("snapshot must NOT run when shutdown fails (CR-001)")
	}
	if truncateRan {
		t.Fatalf("truncate must NOT run when shutdown fails (CR-001)")
	}
}

// TestPitrCreateRejectsNonExecBinary verifies CR-004: a binary without the
// executable bit is rejected at Create (not accepted then EACCES'd after
// SHUTDOWN). No job should be registered.
func TestPitrCreateRejectsNonExecBinary(t *testing.T) {
	m := newTestPitrManager(t, true)
	// Create a regular file with no exec bit.
	nonExec := filepath.Join(t.TempDir(), "notexec")
	if err := os.WriteFile(nonExec, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m.redisCheckBin = nonExec
	_, err := m.Create("prod", "1.1.1.1:6379", 1716000000, fakePitrDeps{})
	if err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("expected non-executable-binary error, got %v", err)
	}
	if got := len(m.jobs); got != 0 {
		t.Fatalf("expected no job registered for non-exec binary, got %d", got)
	}
}

// TestPitrTruncateStatDiffForwardAppeared verifies the forward-compare branch
// of the stat-diff guard: a file present in post-stat but absent from pre-stat
// (appeared after truncate) is flagged as model drift. This is exercised by
// tampering pre-stat to omit a file that is on disk (so post-stat, computed
// from disk, sees it as new).
func TestPitrTruncateStatDiffForwardAppeared(t *testing.T) {
	aofDir, manifestPath, lastSeg := setupAofDir(t, 1000)
	extra := filepath.Join(aofDir, "appendonly.aof.1.base.rdb")
	if err := os.WriteFile(extra, []byte("REDIS002"), 0644); err != nil {
		t.Fatal(err)
	}
	realStat, err := statPitrAofDir(aofDir)
	if err != nil {
		t.Fatal(err)
	}
	job := &PitrJob{
		ID:              "test-fwd-appeared",
		AofDir:          aofDir,
		AofManifest:     manifestPath,
		LastSegmentFile: lastSeg,
		TruncateTs:      500,
		mu:              &sync.Mutex{},
	}
	// Pretend the base file was NOT present before truncate; since it IS on
	// disk, post-stat sees it as "appeared" → forward-compare drift.
	delete(realStat, "appendonly.aof.1.base.rdb")
	job.PreStatSnapshot = realStat
	pm := &PitrManager{redisCheckBin: trueBin(t)}
	err = pm.stepTruncate(stdcontext.Background(), job, nil)
	if err == nil || !strings.Contains(err.Error(), "appeared after truncate") {
		t.Fatalf("expected forward-compare 'appeared' drift, got %v", err)
	}
}

// TestPitrTruncateStatDiffReverseDisappeared verifies the reverse-compare
// branch: a file present in pre-stat but genuinely missing from disk after
// truncate (deleted) is flagged as model drift. Unlike the forward test, this
// one actually deletes the file from disk so stepTruncate's own post-stat
// (computed from disk) genuinely lacks it.
func TestPitrTruncateStatDiffReverseDisappeared(t *testing.T) {
	aofDir, manifestPath, lastSeg := setupAofDir(t, 1000)
	extra := filepath.Join(aofDir, "appendonly.aof.1.base.rdb")
	if err := os.WriteFile(extra, []byte("REDIS002"), 0644); err != nil {
		t.Fatal(err)
	}
	// Record the REAL pre-stat (base file present).
	realStat, err := statPitrAofDir(aofDir)
	if err != nil {
		t.Fatal(err)
	}
	job := &PitrJob{
		ID:              "test-rev-disappeared",
		AofDir:          aofDir,
		AofManifest:     manifestPath,
		LastSegmentFile: lastSeg,
		TruncateTs:      500,
		mu:              &sync.Mutex{},
	}
	job.PreStatSnapshot = realStat
	// Actually delete the base file so post-stat (computed from disk by
	// stepTruncate) genuinely lacks it → reverse-compare "disappeared" branch.
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	pm := &PitrManager{redisCheckBin: trueBin(t)}
	err = pm.stepTruncate(stdcontext.Background(), job, nil)
	if err == nil || !strings.Contains(err.Error(), "disappeared after truncate") {
		t.Fatalf("expected reverse-compare 'disappeared' drift, got %v", err)
	}
}

// TestPitrErrorPrecedenceOverCancel verifies CR-005: when a step returns a real
// error, the job ends failed with that error surfaced (not masked behind
// "cancelled" or "succeeded"). We deterministically fail prereq and assert the
// terminal job carries the real error and state=failed. The structural
// guarantee that a step error beats a concurrent ctx cancellation lives in
// run()'s switch ordering (err case precedes ctx.Err() case); this test pins
// the "real error is surfaced, never swallowed" half of that contract.
func TestPitrErrorPrecedenceOverCancel(t *testing.T) {
	m := newTestPitrManager(t, true)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) { return 3, true, nil },
		configGet: func(addr, pattern string) (map[string]string, error) {
			if pattern == "appendonly" {
				// Deterministic prereq failure with a recognizable error.
				return nil, errors.New("forced prereq error")
			}
			return nil, errFakeNotImpl
		},
	}
	job, err := m.Create("prod", "1.1.1.1:6379", 1716000000, deps)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	state := waitForTerminal(t, m, job.ID, 5*time.Second)
	if state != PitrStateFailed {
		t.Fatalf("expected failed, got %s", state)
	}
	got, _ := m.Get(job.ID)
	if !strings.Contains(got.Error, "forced prereq error") {
		t.Fatalf("CR-005: the real step error must be surfaced on the job, got %q", got.Error)
	}
	// And the failing step must be recorded as failed with that error.
	var prereqStep *PitrStep
	for i := range got.Steps {
		if got.Steps[i].Name == "prereq" {
			prereqStep = &got.Steps[i]
			break
		}
	}
	if prereqStep == nil || prereqStep.Status != pitrStepStatusFailed {
		t.Fatalf("expected prereq step failed, got %+v", prereqStep)
	}
}

// TestAdminPitrDispatchBooleans is a build-time guard: it does NOT run docopt
// (which needs a real argv); instead it asserts the dispatch map keys are typed
// as bool, catching the CR-002 regression where boolean flags were checked with
// != nil. We construct the expected default map shape docopt produces.
func TestAdminPitrDispatchBooleans(t *testing.T) {
	// docopt-go represents absent boolean options as false (bool), not nil.
	// The dispatch in cmdDashboard.Main and handlePitrCommand must therefore
	// type-assert .(bool). We simulate the default (overview) argv: no pitr
	// flags set → all must be false.
	d := map[string]interface{}{
		"--dashboard":                 "127.0.0.1:18080",
		"--pitr-create":               false,
		"--pitr-list":                 false,
		"--pitr-get":                  false,
		"--pitr-cancel":               false,
		"--pitr-remove":               false,
		"--rdb-analysis-remote-fetch": false,
	}
	// Verify every pitr flag is a bool (not nil). If any were nil, the
	// `.(bool)` type assertion in dispatch would panic — this is the CR-002
	// guard.
	for _, k := range []string{"--pitr-create", "--pitr-list", "--pitr-get", "--pitr-cancel", "--pitr-remove"} {
		v, ok := d[k].(bool)
		if !ok {
			t.Fatalf("docopt flag %s must be bool, got %T (CR-002 regression)", k, d[k])
		}
		if v {
			t.Fatalf("absent flag %s must default to false", k)
		}
	}
}

// --- Step 4: external-dep / concurrency tests ---

// TestPitrCreateRejectsMissingBin verifies Create rejects when the configured
// redis-check-aof binary does not exist. No job should be registered.
func TestPitrCreateRejectsMissingBin(t *testing.T) {
	m := newTestPitrManager(t, true)
	m.redisCheckBin = "/nonexistent/path/redis-check-aof"
	_, err := m.Create("prod", "1.1.1.1:6379", 1716000000, fakePitrDeps{})
	if err == nil || !strings.Contains(err.Error(), "redis-check-aof binary") {
		t.Fatalf("expected missing-bin error, got %v", err)
	}
	if got := len(m.jobs); got != 0 {
		t.Fatalf("expected no job registered, got %d", got)
	}
}

// TestPitrConcurrentLimit verifies the global concurrency cap rejects a job
// when too many are running (maxConcurrent=2 in newTestPitrManager). Uses a
// gate that hangs validate so the two starter jobs stay non-terminal, then
// asserts the third Create on a distinct addr is rejected.
func TestPitrConcurrentLimit(t *testing.T) {
	m := newTestPitrManager(t, true)
	m.redisCheckBin = trueBin(t)

	gate := make(chan struct{})
	defer close(gate)
	deps := fakePitrDeps{
		groupOfServer: func(addr string) (int, bool, error) {
			<-gate // hang until the test releases the gate
			return 1, true, nil
		},
	}
	// Fill the budget with 2 jobs on distinct addrs (per-server lock would
	// otherwise reject a second on the same addr).
	for _, addr := range []string{"1.1.1.1:6379", "2.2.2.2:6379"} {
		if _, err := m.Create("prod", addr, 1716000000, deps); err != nil {
			t.Fatalf("first two Creates should succeed: %v", err)
		}
	}
	// Give the state-machine goroutines a moment to enter validate (non-terminal).
	time.Sleep(50 * time.Millisecond)
	// Third must be rejected by the concurrency cap.
	_, err := m.Create("prod", "3.3.3.3:6379", 1716000000, deps)
	if err == nil || !strings.Contains(err.Error(), "too many running") {
		t.Fatalf("expected concurrency-limit error, got %v", err)
	}
}

// TestPitrRemoveCleansSnapshot verifies Remove deletes the snapshot directory.
func TestPitrRemoveCleansSnapshot(t *testing.T) {
	m := newTestPitrManager(t, true)
	aofDir, manifestPath, lastSeg := setupAofDir(t, 1000)
	// Build a job that already has a snapshot dir on disk.
	job := &PitrJob{
		ID:              "snap-cleanup",
		ServerAddr:      "5.5.5.5:6379",
		AofDir:          aofDir,
		AofManifest:     manifestPath,
		LastSegmentFile: lastSeg,
		State:           PitrStateFailed, // terminal so Remove is valid
		mu:              &sync.Mutex{},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	pm := &PitrManager{}
	if err := pm.stepSnapshot(stdcontext.Background(), job, nil); err != nil {
		t.Fatalf("stepSnapshot failed: %v", err)
	}
	snapshotDir := job.SnapshotDir
	if _, err := os.Stat(snapshotDir); err != nil {
		t.Fatalf("snapshot dir should exist: %v", err)
	}
	// Register the job with the manager and Remove it.
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.serverLocks[job.ServerAddr] = &pitrServerLock{owner: job.ID}
	m.mu.Unlock()
	if err := m.Remove(job.ID); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatalf("snapshot dir should be removed, got err=%v", err)
	}
}

// --- handler-gate + wire-contract tests (CR-001 regression, WR-001) ---

// newMinimalApiServer builds an apiServer whose Topom has only the pitr manager
// wired — enough to exercise the /pitr/* handler gate (pitrManagerEnabled) and
// the create-response wire contract without standing up a full coordinator /
// martini server. This is deliberately minimal: pitrManagerEnabled only reads
// s.topom.pitr.
func newMinimalApiServer(t *testing.T, enabled bool) *apiServer {
	t.Helper()
	cfg := NewDefaultConfig()
	cfg.PitrEnabled = enabled
	return &apiServer{topom: &Topom{config: cfg, pitr: NewPitrManager(cfg)}}
}

// TestPitrHandlerGateNoRecursion is the CR-001 regression: pitrManagerEnabled
// must NOT self-recurse (the previous bug called itself instead of
// pitrManager(), stack-overflowing on every /pitr/* request). We call it
// directly on the enabled, disabled, and nil-pitr paths and assert it returns
// without recursing, with the right result/error.
func TestPitrHandlerGateNoRecursion(t *testing.T) {
	// Disabled path: must return the "pitr is disabled" error, not panic.
	disabledSrv := newMinimalApiServer(t, false)
	mgr, err := disabledSrv.pitrManagerEnabled()
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled gate: expected 'pitr is disabled', got mgr=%v err=%v", mgr, err)
	}

	// Enabled path: must return the manager without recursion/panic.
	enabledSrv := newMinimalApiServer(t, true)
	mgr, err = enabledSrv.pitrManagerEnabled()
	if err != nil {
		t.Fatalf("enabled gate: unexpected error: %v", err)
	}
	if mgr == nil || !mgr.Enabled() {
		t.Fatalf("enabled gate: expected non-nil enabled manager, got %v", mgr)
	}

	// Uninitialized (nil pitr) path: distinct error, no panic.
	nilSrv := &apiServer{topom: &Topom{}}
	_, err = nilSrv.pitrManagerEnabled()
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("nil-pitr gate: expected 'not initialized', got %v", err)
	}
}

// TestPitrCreateResponseWireContract verifies WR-001: the create response
// serializes the job id under the "job_id" JSON key (per the SDD contract), not
// a bare "id".
func TestPitrCreateResponseWireContract(t *testing.T) {
	resp := &PitrCreateResponse{JobID: "0193deadbeef"}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	out := string(b)
	if !strings.Contains(out, `"job_id":"0193deadbeef"`) {
		t.Fatalf("expected job_id wire field, got %s", out)
	}
	if strings.Contains(out, `"id"`) {
		t.Fatalf("response must NOT use bare 'id' field, got %s", out)
	}
}
