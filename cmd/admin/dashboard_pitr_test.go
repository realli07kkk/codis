// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"
)

// TestAdminPitrFlagsAreBool pins the CR-002 regression class with the typed
// parser: absent boolean options are false and not explicitly provided. The
// dashboard dispatch switches on the command, so an absent --pitr-* flag must
// never select a pitr handler.
func TestAdminPitrFlagsAreBool(t *testing.T) {
	flags := []string{"--pitr-create", "--pitr-list", "--pitr-get", "--pitr-cancel", "--pitr-remove"}

	// --pitr-list path: only --pitr-list is true; all others false.
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--pitr-list")
	adminMustBool(t, adminBoolArg(cfg, "--pitr-list"), "--pitr-list", true)
	for _, flag := range flags {
		if flag == "--pitr-list" {
			continue
		}
		adminMustBool(t, adminBoolArg(cfg, flag), flag, false)
	}

	// Overview path (no pitr flag): every --pitr-* must be false and not
	// provided, otherwise the default overview path would be shadowed.
	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080")
	for _, flag := range flags {
		adminMustBool(t, adminBoolArg(cfg, flag), flag, false)
	}
}

// TestAdminPitrGetWithJob verifies the --pitr-get / --pitr-cancel /
// --pitr-remove commands parse correctly with --job and surface the flag as a
// true bool plus the --job value (the dispatch contract the handlers rely on).
func TestAdminPitrGetWithJob(t *testing.T) {
	for _, cmd := range []string{"--pitr-get", "--pitr-cancel", "--pitr-remove"} {
		cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", cmd, "--job=abc")
		adminMustBool(t, adminBoolArg(cfg, cmd), cmd, true)
		adminMustString(t, adminStringArg(cfg, "--job"), "--job", "abc")
	}
}

// TestAdminPitrCreateParses verifies --pitr-create with --server and
// --truncate-ts parses and surfaces the flag as a true bool with both values
// bound (the contract handlePitrCreate relies on).
func TestAdminPitrCreateParses(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--pitr-create", "--server=1.1.1.1:6379", "--truncate-ts=1716000000")
	adminMustBool(t, adminBoolArg(cfg, "--pitr-create"), "--pitr-create", true)
	adminMustString(t, adminStringArg(cfg, "--server"), "--server", "1.1.1.1:6379")
	adminMustString(t, adminStringArg(cfg, "--truncate-ts"), "--truncate-ts", "1716000000")
}

// TestAdminOverviewNotShadowedByPitr is the end-to-end CR-002 guard: the plain
// overview command (--dashboard with no subcommand) must not select any
// --pitr-* subcommand.
func TestAdminOverviewNotShadowedByPitr(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080")
	if cfg.Command != "" {
		t.Fatalf("overview command = %q, want empty", cfg.Command)
	}
	for _, flag := range []string{"--pitr-create", "--pitr-list", "--pitr-get", "--pitr-cancel", "--pitr-remove"} {
		adminMustBool(t, adminBoolArg(cfg, flag), flag, false)
	}
}
