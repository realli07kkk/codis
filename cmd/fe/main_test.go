// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

// parseFEUsage drives the typed parser, mirroring main() except that argv is
// explicit.
func parseFEUsage(t *testing.T, argv ...string) *feConfig {
	t.Helper()
	cfg, help, err := parseFEArgs(argv)
	if err != nil {
		t.Fatalf("parseFEArgs(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("parseFEArgs(%v) unexpectedly asked for help", argv)
	}
	return cfg
}

// parseFEUsageError parses an argv that is expected to be rejected.
func parseFEUsageError(t *testing.T, argv ...string) error {
	t.Helper()
	_, help, err := parseFEArgs(argv)
	if help {
		t.Fatalf("parseFEArgs(%v) unexpectedly asked for help", argv)
	}
	return err
}

func feMustBool(t *testing.T, got cli.Bool, flag string, want bool) {
	t.Helper()
	if got.Value != want {
		t.Fatalf("%s = %v, want %v", flag, got.Value, want)
	}
	if got.Provided != want {
		t.Fatalf("%s: provided = %v, want %v", flag, got.Provided, want)
	}
}

func feMustString(t *testing.T, got cli.String, flag string, want string) {
	t.Helper()
	if !got.Provided {
		t.Fatalf("%s: want string(%q), got not provided", flag, want)
	}
	if got.Value != want {
		t.Fatalf("%s = %q, want %q", flag, got.Value, want)
	}
}

func feMustUnset(t *testing.T, got cli.String, flag string) {
	t.Helper()
	if got.Provided {
		t.Fatalf("%s = %#v, want not provided", flag, got.Value)
	}
}

func feStringArg(cfg *feConfig, flag string) cli.String {
	switch flag {
	case "--ncpu":
		return cfg.NCPU
	case "--log":
		return cfg.Log
	case "--log-level":
		return cfg.LogLevel
	case "--assets-dir":
		return cfg.AssetsDir
	case "--pidfile":
		return cfg.Pidfile
	case "--dashboard-list":
		return cfg.DashboardList
	case "--zookeeper":
		return cfg.Coordinator.Zookeeper
	case "--zookeeper-auth":
		return cfg.Coordinator.ZookeeperAuth
	case "--filesystem":
		return cfg.Coordinator.Filesystem
	case "--consul":
		return cfg.Coordinator.Consul
	case "--consul-auth":
		return cfg.Coordinator.ConsulAuth
	case "--listen":
		return cfg.Listen
	default:
		panic("unknown fe flag " + flag)
	}
}

var feStringFlags = []string{
	"--ncpu", "--log", "--log-level", "--assets-dir", "--pidfile",
	"--dashboard-list", "--zookeeper", "--filesystem", "--consul",
	"--listen",
}

var feSourceFlags = []string{
	"--dashboard-list", "--zookeeper", "--zookeeper-auth",
	"--filesystem", "--consul", "--consul-auth",
}

// TestFEParserRequiresListenAndSource pins the usage grammar: both a dashboard
// source and --listen are mandatory in the run form.
func TestFEParserRequiresListenAndSource(t *testing.T) {
	tests := [][]string{
		{},
		{"--listen=:8080"},
		{"--zookeeper=zk"},
		{"--dashboard-list=/tmp/list.json"},
		{"--zookeeper=zk", "--zookeeper-auth=user:pass"},
	}
	for _, argv := range tests {
		if err := parseFEUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

func TestFEVersionFlagIsBool(t *testing.T) {
	cfg := parseFEUsage(t, "--version")
	feMustBool(t, cfg.Version, "--version", true)
	for _, flag := range feStringFlags {
		feMustUnset(t, feStringArg(cfg, flag), flag)
	}

	cfg = parseFEUsage(t, "--listen=:8080", "--zookeeper=zk")
	feMustBool(t, cfg.Version, "--version", false)
}

func TestFEVersionIsExclusiveWithRunFlags(t *testing.T) {
	tests := [][]string{
		{"--version", "--listen=:8080"},
		{"--version", "--zookeeper=zk"},
		{"--version", "--dashboard-list=/tmp/list.json", "--listen=:8080"},
	}
	for _, argv := range tests {
		if err := parseFEUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestFESourceChoices covers the five mutually exclusive dashboard sources.
func TestFESourceChoices(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want map[string]string
	}{
		{"dashboard-list", []string{"--dashboard-list=/tmp/list.json"}, map[string]string{"--dashboard-list": "/tmp/list.json"}},
		{"zookeeper", []string{"--zookeeper=127.0.0.1:2181"}, map[string]string{"--zookeeper": "127.0.0.1:2181"}},
		{"zookeeper auth", []string{"--zookeeper=127.0.0.1:2181", "--zookeeper-auth=user:pass"}, map[string]string{"--zookeeper": "127.0.0.1:2181", "--zookeeper-auth": "user:pass"}},
		{"filesystem", []string{"--filesystem=/tmp/codis"}, map[string]string{"--filesystem": "/tmp/codis"}},
		{"consul", []string{"--consul=127.0.0.1:8500"}, map[string]string{"--consul": "127.0.0.1:8500"}},
		{"consul auth", []string{"--consul=127.0.0.1:8500", "--consul-auth=token"}, map[string]string{"--consul": "127.0.0.1:8500", "--consul-auth": "token"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := append([]string{"--listen=:8080"}, tt.argv...)
			cfg := parseFEUsage(t, argv...)
			feMustString(t, cfg.Listen, "--listen", ":8080")
			for _, flag := range feSourceFlags {
				if value, ok := tt.want[flag]; ok {
					feMustString(t, feStringArg(cfg, flag), flag, value)
				} else {
					feMustUnset(t, feStringArg(cfg, flag), flag)
				}
			}
		})
	}
}

func TestFESourceChoicesRejectMultipleSources(t *testing.T) {
	sources := [][]string{
		{"--dashboard-list=/tmp/list.json"},
		{"--zookeeper=zk", "--zookeeper-auth=user:pass"},
		{"--filesystem=/tmp/codis"},
		{"--consul=consul", "--consul-auth=token"},
	}
	for i := 0; i < len(sources); i++ {
		for j := i + 1; j < len(sources); j++ {
			argv := append([]string{"--listen=:8080"}, sources[i]...)
			argv = append(argv, sources[j]...)
			if err := parseFEUsageError(t, argv...); err == nil {
				t.Fatalf("argv %v: expected parse error for conflicting sources", argv)
			}
		}
	}
}

// TestFEAuthFlagsRequireTheirCoordinator pins that --*-auth only forms a valid
// argv together with the matching coordinator address.
func TestFEAuthFlagsRequireTheirCoordinator(t *testing.T) {
	for _, flag := range []string{"--zookeeper-auth=user:pass", "--consul-auth=token"} {
		argv := append([]string{"--listen=:8080"}, flag)
		if err := parseFEUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

func TestFEStringFlagsBindValues(t *testing.T) {
	argv := []string{
		"--ncpu=4", "--log=/tmp/fe.log", "--log-level=WARN",
		"--assets-dir=/opt/codis/assets", "--pidfile=/tmp/fe.pid",
		"--dashboard-list=/tmp/list.json", "--listen=0.0.0.0:8080",
	}
	want := map[string]string{
		"--ncpu": "4", "--log": "/tmp/fe.log", "--log-level": "WARN",
		"--assets-dir": "/opt/codis/assets", "--pidfile": "/tmp/fe.pid",
		"--dashboard-list": "/tmp/list.json", "--listen": "0.0.0.0:8080",
	}
	cfg := parseFEUsage(t, argv...)
	for flag, value := range want {
		feMustString(t, feStringArg(cfg, flag), flag, value)
	}
	feMustBool(t, cfg.Version, "--version", false)
	feMustUnset(t, feStringArg(cfg, "--zookeeper"), "--zookeeper")
}

// TestFEStringFlagsAbsentExceptRequired covers the optional value flags. The
// required --listen and the required source are verified separately because an
// absent value is a parse error, not a silently empty value.
func TestFEStringFlagsAbsentExceptRequired(t *testing.T) {
	cfg := parseFEUsage(t, "--listen=:8080", "--zookeeper=zk")
	for _, flag := range []string{"--ncpu", "--log", "--log-level", "--assets-dir", "--pidfile", "--dashboard-list", "--filesystem", "--consul"} {
		feMustUnset(t, feStringArg(cfg, flag), flag)
	}
}

func TestFEShortAliases(t *testing.T) {
	cfg := parseFEUsage(t, "-d", "/tmp/list.json", "-l", "/tmp/fe.log", "--listen=:8080")
	feMustString(t, cfg.DashboardList, "--dashboard-list", "/tmp/list.json")
	feMustString(t, cfg.Log, "--log", "/tmp/fe.log")
}

func TestFEHelp(t *testing.T) {
	cfg, help, err := parseFEArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("parseFEArgs(--help) failed: %v", err)
	}
	if !help || cfg != nil {
		t.Fatalf("cfg = %+v, help = %v; want nil, true", cfg, help)
	}
}
