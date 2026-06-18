// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/docopt/docopt-go"
)

// parseAdminUsage runs the real docopt.Parse from the admin package against the
// package-level adminUsage contract, mirroring how main() parses argv. It is the
// authoritative source for "what shape does docopt produce" so the dispatch
// contract can be pinned without hand-rolled map fakes.
func parseAdminUsage(t *testing.T, argv ...string) map[string]interface{} {
	t.Helper()
	// docopt.Parse(usage, argv, help=true, version="", optionsFirst=false).
	// The admin binary passes nil for argv to read os.Args; here we feed an
	// explicit argv so the test is deterministic.
	d, err := docopt.Parse(adminUsage, argv, true, "", false)
	if err != nil {
		t.Fatalf("docopt.Parse(%v) failed: %v", argv, err)
	}
	return d
}

// TestAdminDocoptPitrFlagsAreBool pins the CR-002 regression class: docopt-go
// represents absent boolean options as false (bool), never nil. The dashboard
// dispatch uses `.(bool)` on every --pitr-* flag; if docopt ever returned nil
// for an absent flag, that type assertion would panic and the default overview
// path would fall into a pitr handler. We assert the real parser's output shape
// for the pitr-list and overview argvs.
func TestAdminDocoptPitrFlagsAreBool(t *testing.T) {
	flags := []string{"--pitr-create", "--pitr-list", "--pitr-get", "--pitr-cancel", "--pitr-remove"}

	// --pitr-list path: only --pitr-list is true; all others false (bool).
	d := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--pitr-list")
	if v, ok := d["--pitr-list"].(bool); !ok || !v {
		t.Fatalf("--pitr-list: expected true bool, got %T=%v", d["--pitr-list"], d["--pitr-list"])
	}
	for _, f := range flags {
		if f == "--pitr-list" {
			continue
		}
		v, ok := d[f].(bool)
		if !ok {
			t.Fatalf("absent flag %s must be bool, got %T (CR-002 regression)", f, d[f])
		}
		if v {
			t.Fatalf("absent flag %s must default to false", f)
		}
	}

	// Overview path (no pitr flag): every --pitr-* must be false (bool), never
	// nil — otherwise the default overview case in cmdDashboard.Main would be
	// shadowed by the pitr fallthrough chain.
	d = parseAdminUsage(t, "--dashboard=127.0.0.1:18080")
	for _, f := range flags {
		v, ok := d[f].(bool)
		if !ok {
			t.Fatalf("overview path: absent flag %s must be bool, got %T (CR-002 regression)", f, d[f])
		}
		if v {
			t.Fatalf("overview path: absent flag %s must be false", f)
		}
	}
}

// TestAdminDocoptPitrGetWithJob verifies the --pitr-get / --pitr-cancel /
// --pitr-remove commands parse correctly with --job and surface the flag as a
// true bool plus the --job value (the dispatch contract the handlers rely on).
func TestAdminDocoptPitrGetWithJob(t *testing.T) {
	for _, cmd := range []string{"--pitr-get", "--pitr-cancel", "--pitr-remove"} {
		d := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", cmd, "--job=abc")
		if v, ok := d[cmd].(bool); !ok || !v {
			t.Fatalf("%s --job=abc: expected flag true bool, got %T=%v", cmd, d[cmd], d[cmd])
		}
		if d["--job"] != "abc" {
			t.Fatalf("%s: --job value mismatch, got %v", cmd, d["--job"])
		}
	}
}

// TestAdminDocoptPitrCreateParses verifies --pitr-create with --server and
// --truncate-ts parses and surfaces the flag as a true bool with both values
// bound (the contract handlePitrCreate relies on).
func TestAdminDocoptPitrCreateParses(t *testing.T) {
	d := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--pitr-create", "--server=1.1.1.1:6379", "--truncate-ts=1716000000")
	if v, ok := d["--pitr-create"].(bool); !ok || !v {
		t.Fatalf("--pitr-create: expected true bool, got %T=%v", d["--pitr-create"], d["--pitr-create"])
	}
	if d["--server"] != "1.1.1.1:6379" {
		t.Fatalf("--server value mismatch, got %v", d["--server"])
	}
	if d["--truncate-ts"] != "1716000000" {
		t.Fatalf("--truncate-ts value mismatch, got %v", d["--truncate-ts"])
	}
}

// TestAdminDocoptOverviewNotShadowedByPitr is the end-to-end CR-002 guard: the
// plain overview command (--dashboard with no subcommand) must NOT set any
// --pitr-* flag true. Combined with the .(bool) dispatch this guarantees the
// overview path reaches handleOverview, not handlePitrCommand.
func TestAdminDocoptOverviewNotShadowedByPitr(t *testing.T) {
	d := parseAdminUsage(t, "--dashboard=127.0.0.1:18080")
	for _, f := range []string{"--pitr-create", "--pitr-list", "--pitr-get", "--pitr-cancel", "--pitr-remove"} {
		if v, _ := d[f].(bool); v {
			t.Fatalf("overview argv must not set %s true (CR-002: overview would be shadowed)", f)
		}
	}
}
