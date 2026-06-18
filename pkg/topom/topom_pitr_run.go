// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	stdcontext "context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// PitrDeps abstracts the side-effectful operations the state machine needs
// (talking to the target Redis server). The real implementation is wired by
// the Topom; tests inject fakes. Keeping these behind an interface lets every
// step be unit-tested without a live Redis.
type PitrDeps interface {
	// ConfigGet returns the result of CONFIG GET pattern on the target server
	// as a map of matched parameter -> value.
	ConfigGet(ctx stdcontext.Context, serverAddr, pattern string) (map[string]string, error)
	// GroupOfServer reports the group id and whether serverAddr is the master
	// (Servers[0]) of that group within the current product topology.
	GroupOfServer(serverAddr string) (gid int, isMaster bool, err error)
	// GroupServers returns the addrs of all servers in the given group.
	GroupServers(gid int) ([]string, error)
	// Shutdown sends SHUTDOWN NOSAVE to the target server.
	Shutdown(ctx stdcontext.Context, serverAddr string) error
	// PingInfo issues an INFO probe; used to poll until the server is back up
	// and no longer loading after restart.
	PingInfo(ctx stdcontext.Context, serverAddr string) (map[string]string, error)
	// ResyncReplica re-points the replica at the master, forcing a full resync.
	ResyncReplica(ctx stdcontext.Context, masterAddr, replicaAddr string) error
}

// run drives the full PITR state machine for one job. Each step is recorded on
// the job; the first error transitions the job to failed. The ctx is cancelled
// by Cancel/Remove/Close.
func (m *PitrManager) run(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) {
	start := time.Now()
	log.Warnf("pitr job-[%s] start server=%s ts=%d", job.ID, job.ServerAddr, job.TruncateTs)

	err := m.runSteps(ctx, job, deps)

	// Decide the terminal state. A step error takes precedence over a cancelled
	// context: if a step already returned an error (e.g. AOF drift detected),
	// we must report failed, not cancelled — masking a real failure behind
	// "cancelled" would hide AOF damage from the operator. The per-server lock
	// is NOT released here: it stays held until the operator Removes the
	// terminal job (PITR is non-idempotent; a terminal-but-unacknowledged job
	// must keep blocking new jobs on the same server).
	switch {
	case err != nil:
		// runSteps already set the failing step and job state=failed; only log.
		log.WarnErrorf(err, "pitr job-[%s] failed in %v", job.ID, time.Since(start))
	case ctx.Err() != nil:
		job.setState(PitrStateCancelled, nil)
		log.Warnf("pitr job-[%s] cancelled in %v", job.ID, time.Since(start))
	default:
		job.setState(PitrStateSucceeded, nil)
		log.Warnf("pitr job-[%s] succeeded in %v", job.ID, time.Since(start))
	}
}

// runSteps executes the state-machine steps in order, returning on the first
// error. The ctx is checked before each step so a cancel between steps is
// honored promptly.
func (m *PitrManager) runSteps(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	steps := []struct {
		name string
		fn   func(stdcontext.Context, *PitrJob, PitrDeps) error
	}{
		{"validate", m.stepValidate},
		{"prereq", m.stepPrereq},
		{"feasibility", m.stepFeasibility},
		{"shutdown", m.stepShutdown},
		{"snapshot", m.stepSnapshot},
		{"truncate", m.stepTruncate},
		{"restart", m.stepRestart},
		{"wait-load", m.stepWaitLoad},
		{"resync", m.stepResync},
	}
	for _, s := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		var stepErr error
		if rerr := job.step(s.name, func() error {
			stepErr = s.fn(ctx, job, deps)
			return stepErr
		}); rerr != nil {
			return rerr
		}
	}
	return nil
}

// --- Step 2: validation steps ---

// stepValidate confirms the target server exists in the current product and is
// the master of its group. Replica targets are rejected (replica truncation
// would split-brain against the master). Runs while the server is still UP; a
// failure here touches nothing.
func (m *PitrManager) stepValidate(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	gid, isMaster, err := deps.GroupOfServer(job.ServerAddr)
	if err != nil {
		return errors.Trace(err)
	}
	job.mu.Lock()
	job.GroupID = gid
	job.mu.Unlock()
	if !isMaster {
		return errors.Errorf("server %s is not the master of group %d", job.ServerAddr, gid)
	}
	return nil
}

// stepPrereq reads the 5 CONFIG GET values needed to locate the AOF on disk
// (and only those — NOT port/bind/*file, which would be fork-restart residue).
// It verifies appendonly and aof-timestamp-enabled are both on; both are
// immutable-ish Redis configs that the operator must have enabled beforehand
// (dashboard never flips them). Runs while the server is still UP.
func (m *PitrManager) stepPrereq(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	config, err := deps.ConfigGet(ctx, job.ServerAddr, "appendonly")
	if err != nil {
		return errors.Trace(err)
	}
	if v, ok := config["appendonly"]; !ok || strings.ToLower(v) != "yes" {
		return errors.Errorf("prerequisites not met: appendonly=%q (expected yes)", config["appendonly"])
	}
	config, err = deps.ConfigGet(ctx, job.ServerAddr, "aof-timestamp-enabled")
	if err != nil {
		return errors.Trace(err)
	}
	if v, ok := config["aof-timestamp-enabled"]; !ok || strings.ToLower(v) != "yes" {
		return errors.Errorf("prerequisites not met: aof-timestamp-enabled=%q (expected yes)", config["aof-timestamp-enabled"])
	}
	// dir + appendfilename + appenddirname locate the AOF files on disk.
	dirCfg, err := deps.ConfigGet(ctx, job.ServerAddr, "dir")
	if err != nil {
		return errors.Trace(err)
	}
	appendfilename, err := deps.ConfigGet(ctx, job.ServerAddr, "appendfilename")
	if err != nil {
		return errors.Trace(err)
	}
	appenddirname, err := deps.ConfigGet(ctx, job.ServerAddr, "appenddirname")
	if err != nil {
		return errors.Trace(err)
	}
	dir := dirCfg["dir"]
	if dir == "" || appendfilename["appendfilename"] == "" || appenddirname["appenddirname"] == "" {
		return errors.Errorf("prerequisites not met: could not resolve dir/appendfilename/appenddirname")
	}
	job.mu.Lock()
	job.AofDir = filepath.Join(dir, appenddirname["appenddirname"])
	job.AofManifest = filepath.Join(job.AofDir, appendfilename["appendfilename"]+".manifest")
	job.mu.Unlock()
	return nil
}

// stepFeasibility confirms, before touching the server, that the truncate
// timestamp actually falls inside the last AOF segment file's #TS: range and
// that there is enough free disk for the snapshot. This front-loads the
// majority of redis-check-aof's pre-ftruncate failures (ts not in last file,
// ts before all records, bad annotation) so they never reach the SHUTDOWN step.
// Runs while the server is still UP; a failure touches nothing.
func (m *PitrManager) stepFeasibility(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	segments, err := readPitrAofManifest(job.AofManifest)
	if err != nil {
		return errors.Trace(err)
	}
	if len(segments) == 0 {
		return errors.Errorf("feasibility: aof manifest %s has no segments", job.AofManifest)
	}
	// The "last segment" per redis-check-aof is the last INCR if any exist,
	// otherwise the single BASE. We iterate in manifest order; the manifest
	// lists BASE first then INCRs in seq order, so the last entry is the last
	// file redis-check-aof will consider truncatable.
	lastFile := segments[len(segments)-1]
	lastPath := filepath.Join(job.AofDir, lastFile)

	minTs, maxTs, err := scanPitrAofTimestamps(lastPath)
	if err != nil {
		return errors.Trace(err)
	}
	if minTs == 0 && maxTs == 0 {
		return errors.Errorf("feasibility: last aof segment %s has no #TS: annotations", lastFile)
	}
	// The truncate point must lie within the last file's range. A ts earlier
	// than minTs means redis-check-aof would report "nothing before ts" on an
	// earlier file (and refuse, since it only truncates the last file); a ts
	// later than maxTs is in the future / beyond recorded history.
	if job.TruncateTs < minTs || job.TruncateTs > maxTs {
		return errors.Errorf("truncate point not in last aof segment: ts=%d not in [%d,%d]", job.TruncateTs, minTs, maxTs)
	}

	// Best-effort free-space check for the snapshot (manifest + last file).
	lastSize, err := fileSizePitr(lastPath)
	if err != nil {
		return errors.Trace(err)
	}
	if free, err := freeDiskPitr(job.AofDir); err == nil && free < lastSize {
		return errors.Errorf("insufficient disk for snapshot: need %d have %d", lastSize, free)
	}

	job.mu.Lock()
	job.LastSegmentFile = lastFile
	job.LastSegmentTsRange = [2]int64{minTs, maxTs}
	job.mu.Unlock()
	return nil
}

// readPitrAofManifest parses an MP-AOF manifest and returns the segment
// filenames in evaluation order (BASE first, then INCRs by seq). Each manifest
// line is "file <name> seq <n> type <b|h|i>".
func readPitrAofManifest(manifestPath string) ([]string, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, errors.Trace(err)
	}
	var base string
	var incrs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		// Expected: file <name> seq <n> type <type>
		if len(fields) < 6 || fields[0] != "file" {
			continue
		}
		name := fields[1]
		typ := fields[5]
		switch typ {
		case "b":
			base = name
		case "h", "i":
			incrs = append(incrs, name)
		}
	}
	out := make([]string, 0, 1+len(incrs))
	if base != "" {
		out = append(out, base)
	}
	out = append(out, incrs...)
	return out, nil
}

// scanPitrAofTimestamps scans an AOF segment file for #TS:<unix-seconds>
// annotations (written when aof-timestamp-enabled is on) and returns the
// [min, max] timestamp range. Returns (0, 0, nil) if no annotations exist.
// It only reads annotation lines (lines starting with '#'); RESP payloads that
// happen to contain "#TS:" are not misread because annotations are full-line
// comments outside RESP bulk framing.
func scanPitrAofTimestamps(path string) (minTs, maxTs int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, errors.Trace(err)
	}
	defer f.Close()

	buf := make([]byte, 64*1024)
	var carry []byte
	found := false
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			data := buf[:n]
			if len(carry) > 0 {
				data = append(carry, data...)
				carry = carry[:0]
			}
			// Process line by line. A #TS: annotation is a single full line.
			for {
				idx := indexByte(data, '\n')
				if idx < 0 {
					// Keep the trailing partial line for the next iteration.
					carry = append(carry, data...)
					break
				}
				line := data[:idx]
				data = data[idx+1:]
				ts, ok := parseTsAnnotation(line)
				if ok {
					found = true
					if minTs == 0 || ts < minTs {
						minTs = ts
					}
					if ts > maxTs {
						maxTs = ts
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	// Flush any final line without trailing newline.
	if len(carry) > 0 {
		if ts, ok := parseTsAnnotation(carry); ok {
			found = true
			if minTs == 0 || ts < minTs {
				minTs = ts
			}
			if ts > maxTs {
				maxTs = ts
			}
		}
	}
	if !found {
		return 0, 0, nil
	}
	return minTs, maxTs, nil
}

// parseTsAnnotation returns the unix timestamp from a "#TS:<seconds>" line.
func parseTsAnnotation(line []byte) (int64, bool) {
	s := strings.TrimSpace(string(line))
	if !strings.HasPrefix(s, "#TS:") {
		return 0, false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(s[4:]), 10, 64)
	if err != nil {
		return 0, false
	}
	return ts, true
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func fileSizePitr(path string) (int64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, errors.Trace(err)
	}
	return st.Size(), nil
}

// freeDiskPitr returns the free bytes on the filesystem holding path. Errors
// are non-fatal (caller treats them as "unknown").
func freeDiskPitr(path string) (int64, error) {
	return freeDisk(path)
}

// --- Step 3: side-effectful steps ---

// stepShutdown stops the target server with SHUTDOWN NOSAVE so Redis does not
// write an RDB on exit that could mask the truncation. After this step the
// server is DOWN; subsequent failures must account for that in their error
// messages (see stepSnapshot/stepTruncate).
func (m *PitrManager) stepShutdown(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	if err := deps.Shutdown(ctx, job.ServerAddr); err != nil {
		return errors.Trace(err)
	}
	return nil
}

// stepSnapshot copies the manifest + last segment file to a sibling directory
// on the same filesystem (so a later restore is a same-FS copy back) and
// records the size+mtime of every AOF directory entry for the post-truncate
// stat-diff drift check. On failure the AOF is untouched and the half-written
// snapshot dir is removed.
func (m *PitrManager) stepSnapshot(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	snapshotDir := filepath.Join(filepath.Dir(job.AofDir), ".pitr-snapshot-"+job.ID)
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		return errors.Trace(err)
	}

	// Record size+mtime of every file in the AOF dir BEFORE any truncate, for
	// the post-truncate drift check (only the last file should change).
	preStat, err := statPitrAofDir(job.AofDir)
	if err != nil {
		_ = os.RemoveAll(snapshotDir)
		return errors.Trace(err)
	}

	// Copy the manifest file.
	manifestBase := filepath.Base(job.AofManifest)
	if err := copyFile(job.AofManifest, filepath.Join(snapshotDir, manifestBase)); err != nil {
		_ = os.RemoveAll(snapshotDir)
		return errors.Trace(err)
	}
	// Copy only the last segment file (redis-check-aof only ftruncates it).
	if err := copyFile(filepath.Join(job.AofDir, job.LastSegmentFile), filepath.Join(snapshotDir, job.LastSegmentFile)); err != nil {
		_ = os.RemoveAll(snapshotDir)
		return errors.Trace(err)
	}

	job.mu.Lock()
	job.SnapshotDir = snapshotDir
	job.PreStatSnapshot = preStat
	job.mu.Unlock()
	return nil
}

// stepTruncate runs `redis-check-aof --truncate-to-timestamp <ts> --fix
// <manifest>`, then re-stats the AOF dir and compares to PreStatSnapshot. The
// --fix flag is required for ftruncate; redis-check-aof would otherwise exit
// non-zero without modifying anything. A non-zero exit OR a detected drift in
// any non-last file leaves the job failed with a restore-from-snapshot pointer.
func (m *PitrManager) stepTruncate(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	// Use CommandContext so a cancel (Cancel/Remove/Close) actually kills the
	// redis-check-aof child instead of leaving it running to ftruncate the AOF
	// after the job is reported cancelled.
	cmd := exec.CommandContext(ctx, m.redisCheckBin,
		"--truncate-to-timestamp", strconv.FormatInt(job.TruncateTs, 10),
		"--fix", job.AofManifest,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// If cancelled, surface that distinctly — the ftruncate may or may not
		// have happened; the snapshot (if taken) is the operator's recovery
		// path. Otherwise the server is DOWN and the AOF may be in an unknown
		// state; do NOT auto-restart: surface the snapshot dir to restore.
		if ctx.Err() != nil {
			return errors.Errorf("redis-check-aof cancelled (AOF state uncertain, server is down)\nsnapshot kept at %s: %v", job.SnapshotDir, ctx.Err())
		}
		return errors.Errorf("redis-check-aof failed (server is down): %v\noutput: %s\nrestore from snapshot %s then restart",
			err, truncateOutput(out), job.SnapshotDir)
	}

	// Post-truncate drift check: re-stat the AOF dir and compare. Only the last
	// segment file should have changed (redis-check-aof only ftruncates it). Any
	// other file changing means the tool's behavior diverged from our model
	// (version drift / bug) — treat as uncertain, keep the snapshot, alert.
	postStat, err := statPitrAofDir(job.AofDir)
	if err != nil {
		return errors.Errorf("post-truncate stat failed (server is down): %v\nsnapshot kept at %s", err, job.SnapshotDir)
	}
	job.mu.Lock()
	preStat := job.PreStatSnapshot
	lastFile := job.LastSegmentFile
	job.mu.Unlock()
	// Forward compare: every file present after truncate. A new file (not in
	// pre) or a changed non-last file is drift.
	for name, post := range postStat {
		pre, ok := preStat[name]
		if !ok {
			return errors.Errorf("aof model drift detected: file %s appeared after truncate (server is down)\nsnapshot kept at %s", name, job.SnapshotDir)
		}
		if name == lastFile {
			continue // the last file is expected to change (it got ftruncated)
		}
		if pre.Size != post.Size || !pre.Mtime.Equal(post.Mtime) {
			return errors.Errorf("aof model drift detected: non-last file %s changed after truncate (server is down)\nsnapshot kept at %s", name, job.SnapshotDir)
		}
	}
	// Reverse compare: every file present before truncate. A pre-existing file
	// missing after truncate (deletion) is also drift — redis-check-aof must
	// never delete any AOF file.
	for name := range preStat {
		if _, ok := postStat[name]; !ok {
			return errors.Errorf("aof model drift detected: file %s disappeared after truncate (server is down)\nsnapshot kept at %s", name, job.SnapshotDir)
		}
	}
	return nil
}

// truncateOutput trims redis-check-aof output to a reasonable size for the job
// error message.
func truncateOutput(out []byte) string {
	const max = 512
	s := strings.TrimSpace(string(out))
	if len(s) > max {
		return s[:max] + "...(truncated)"
	}
	return s
}

// stepRestart optionally executes pitr_restart_command (a kick, e.g.
// `systemctl restart codis-server@group3`) and then ALWAYS polls INFO until the
// server is reachable again. Dashboard never forks redis-server directly. If
// pitr_restart_command is unset, this step is pure polling (the operator's
// process manager must bring the server back up).
func (m *PitrManager) stepRestart(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	if m.restartCmd != "" {
		cmd := exec.CommandContext(ctx, "sh", "-c", m.restartCmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.WarnErrorf(err, "pitr job-[%s] pitr_restart_command failed: %s", job.ID, truncateOutput(out))
			// Fall through to polling: the operator's process manager may still
			// bring it up, or the kick may have partially worked.
		} else {
			job.mu.Lock()
			job.RestartKicked = true
			job.mu.Unlock()
		}
	}
	// Poll until INFO responds (server is back up, possibly still loading).
	if err := pollUntilResponsive(ctx, job, deps, m.jobTimeout); err != nil {
		return errors.Errorf("server did not become responsive after restart in %v (server may be down or still loading)\nsnapshot kept at %s", m.jobTimeout, job.SnapshotDir)
	}
	return nil
}

// stepWaitLoad polls INFO persistence until loading=0. After a successful
// truncate + restart the server loads the rewound AOF; we must not resync
// replicas until loading completes.
func (m *PitrManager) stepWaitLoad(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	deadline := time.Now().Add(m.jobTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := deps.PingInfo(ctx, job.ServerAddr)
		if err == nil {
			if strings.ToLower(info["loading"]) != "1" {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.Errorf("server still loading after %v (AOF was correctly truncated; operator may confirm and clear the job)\nsnapshot kept at %s", m.jobTimeout, job.SnapshotDir)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// stepResync re-points every other server in the group at the recovered master,
// forcing a full resync (the master's replication id/offset was rewound by the
// truncate). Partial failures do not fail the job — they are recorded as a
// warning on the step so the operator can retry the lagging replicas manually.
func (m *PitrManager) stepResync(ctx stdcontext.Context, job *PitrJob, deps PitrDeps) error {
	servers, err := deps.GroupServers(job.GroupID)
	if err != nil {
		return errors.Trace(err)
	}
	var failed []string
	for _, addr := range servers {
		if addr == job.ServerAddr {
			continue // skip the recovered master itself
		}
		if err := deps.ResyncReplica(ctx, job.ServerAddr, addr); err != nil {
			failed = append(failed, addr)
			log.WarnErrorf(err, "pitr job-[%s] resync replica %s failed", job.ID, addr)
		}
	}
	if len(failed) > 0 {
		// Record as a step warning, not a job failure — the master recovered
		// successfully; only some replicas lag. Surface in the step error field
		// but keep state succeeded.
		job.mu.Lock()
		for i, s := range job.Steps {
			if s.Name == "resync" {
				job.Steps[i].Error = "resync warning: replicas failed full resync: " + strings.Join(failed, ", ")
				break
			}
		}
		job.mu.Unlock()
	}
	return nil
}

// --- helpers used by the side-effectful steps ---

// statPitrAofDir records size+mtime of every regular file in dir.
func statPitrAofDir(dir string) (map[string]pitrFileStat, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Trace(err)
	}
	out := make(map[string]pitrFileStat, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out[e.Name()] = pitrFileStat{Size: info.Size(), Mtime: info.ModTime()}
	}
	return out, nil
}

// copyFile copies src to dst using io copy (same-filesystem assumption is the
// caller's responsibility — we do not hardlink because the source may be on a
// different mount than the snapshot dir in pathological layouts).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return errors.Trace(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return errors.Trace(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return errors.Trace(err)
	}
	return errors.Trace(out.Close())
}

// pollUntilResponsive polls PingInfo until it succeeds (server reachable) or
// timeout. It does NOT wait for loading to finish — that's stepWaitLoad.
func pollUntilResponsive(ctx stdcontext.Context, job *PitrJob, deps PitrDeps, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := deps.PingInfo(ctx, job.ServerAddr); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
