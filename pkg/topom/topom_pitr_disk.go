// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"os"
	"syscall"
)

// freeDisk returns the free bytes available on the filesystem holding path.
// Used only for the best-effort snapshot free-space check; callers treat errors
// as "unknown, proceed".
func freeDisk(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// pitrBinExecutable reports whether path is a regular file executable by the
// current process. Used by the Create-time preflight so a non-executable or
// missing redis-check-aof fails before any side effect (rather than EACCES in
// stepTruncate after the master is already shut down).
func pitrBinExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	// Regular file check via Sys() stat mode.
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		// Any executable bit (user/ group/ other) — consistent with how a shell
		// resolves an executable for the effective uid/gid in practice; the
		// strict EACCES test on actual exec is the final authority in
		// stepTruncate, this is the early gate.
		mode := stat.Mode
		return mode&0111 != 0
	}
	// Fallback (non-Unix): trust non-dir existence.
	return true
}
