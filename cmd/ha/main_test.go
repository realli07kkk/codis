// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

// parseHAUsage drives the typed parser, mirroring main() except that argv is
// explicit.
func parseHAUsage(t *testing.T, argv ...string) *haConfig {
	t.Helper()
	cfg, help, err := parseHAArgs(argv)
	if err != nil {
		t.Fatalf("parseHAArgs(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("parseHAArgs(%v) unexpectedly asked for help", argv)
	}
	return cfg
}

// parseHAUsageError parses an argv that is expected to be rejected.
func parseHAUsageError(t *testing.T, argv ...string) error {
	t.Helper()
	_, help, err := parseHAArgs(argv)
	if help {
		t.Fatalf("parseHAArgs(%v) unexpectedly asked for help", argv)
	}
	return err
}

func haMustBool(t *testing.T, got cli.Bool, flag string, want bool) {
	t.Helper()
	if got.Value != want {
		t.Fatalf("%s = %v, want %v", flag, got.Value, want)
	}
	if got.Provided != want {
		t.Fatalf("%s: provided = %v, want %v", flag, got.Provided, want)
	}
}

func haMustString(t *testing.T, got cli.String, flag string, want string) {
	t.Helper()
	if !got.Provided {
		t.Fatalf("%s: want string(%q), got not provided", flag, want)
	}
	if got.Value != want {
		t.Fatalf("%s = %q, want %q", flag, got.Value, want)
	}
}

func haMustUnset(t *testing.T, got cli.String, flag string) {
	t.Helper()
	if got.Provided {
		t.Fatalf("%s = %#v, want not provided", flag, got.Value)
	}
}

func haStringArg(cfg *haConfig, flag string) cli.String {
	switch flag {
	case "--log":
		return cfg.Log
	case "--log-level":
		return cfg.LogLevel
	case "--interval":
		return cfg.Interval
	case "--dashboard":
		return cfg.Dashboard
	default:
		panic("unknown ha flag " + flag)
	}
}

var haStringFlags = []string{"--log", "--log-level", "--interval", "--dashboard"}

// TestHARequiresDashboard pins that the run form of codis-ha always requires
// --dashboard; --version is the only standalone form.
func TestHARequiresDashboard(t *testing.T) {
	tests := [][]string{
		{},
		{"--no-maintains"},
		{"--log=/tmp/ha.log"},
		{"--log-level=DEBUG"},
		{"--interval=5"},
		{"--log=/tmp/ha.log", "--no-maintains"},
	}
	for _, argv := range tests {
		if err := parseHAUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error without --dashboard", argv)
		}
	}
}

func TestHAVersionFlagIsBool(t *testing.T) {
	cfg := parseHAUsage(t, "--version")
	haMustBool(t, cfg.Version, "--version", true)
	for _, flag := range haStringFlags {
		haMustUnset(t, haStringArg(cfg, flag), flag)
	}
	haMustBool(t, cfg.NoMaintains, "--no-maintains", false)

	cfg = parseHAUsage(t, "--dashboard=127.0.0.1:18080")
	haMustBool(t, cfg.Version, "--version", false)
	haMustString(t, cfg.Dashboard, "--dashboard", "127.0.0.1:18080")
}

func TestHAVersionIsExclusiveWithRunFlags(t *testing.T) {
	tests := [][]string{
		{"--version", "--dashboard=127.0.0.1:18080"},
		{"--version", "--no-maintains"},
		{"--version", "--log=/tmp/ha.log"},
	}
	for _, argv := range tests {
		if err := parseHAUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestHANoMaintainsIsBool pins main()'s use of cfg.NoMaintains and the
// default of maintains=true when the flag is absent.
func TestHANoMaintainsIsBool(t *testing.T) {
	cfg := parseHAUsage(t, "--dashboard=127.0.0.1:18080")
	haMustBool(t, cfg.NoMaintains, "--no-maintains", false)

	cfg = parseHAUsage(t, "--dashboard=127.0.0.1:18080", "--no-maintains")
	haMustBool(t, cfg.NoMaintains, "--no-maintains", true)
}

func TestHAStringFlagsBindValues(t *testing.T) {
	argv := []string{
		"--log=/tmp/ha.log", "--log-level=DEBUG", "--interval=10",
		"--dashboard=127.0.0.1:18080",
	}
	want := map[string]string{
		"--log": "/tmp/ha.log", "--log-level": "DEBUG", "--interval": "10",
		"--dashboard": "127.0.0.1:18080",
	}
	cfg := parseHAUsage(t, argv...)
	for flag, value := range want {
		haMustString(t, haStringArg(cfg, flag), flag, value)
	}
	haMustBool(t, cfg.Version, "--version", false)
	haMustBool(t, cfg.NoMaintains, "--no-maintains", false)
}

// TestHAIntervalIsNotValidatedByParser pins that numeric validation lives in
// main() (it exits on n <= 0), not in the parser. The flag migration keeps
// "parse first, validate later" behavior.
func TestHAIntervalIsNotValidatedByParser(t *testing.T) {
	cfg := parseHAUsage(t, "--dashboard=127.0.0.1:18080", "--interval=0")
	haMustString(t, cfg.Interval, "--interval", "0")
}

func TestHAShortLogAlias(t *testing.T) {
	cfg := parseHAUsage(t, "-l", "/tmp/ha.log", "--dashboard=127.0.0.1:18080")
	haMustString(t, cfg.Log, "--log", "/tmp/ha.log")
}

func TestHAHelp(t *testing.T) {
	cfg, help, err := parseHAArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("parseHAArgs(--help) failed: %v", err)
	}
	if !help || cfg != nil {
		t.Fatalf("cfg = %+v, help = %v; want nil, true", cfg, help)
	}
}
