// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

// parseDashboardUsage drives the typed parser, mirroring main() except that
// argv is explicit.
func parseDashboardUsage(t *testing.T, argv ...string) *dashboardConfig {
	t.Helper()
	cfg, help, err := parseDashboardArgs(argv)
	if err != nil {
		t.Fatalf("parseDashboardArgs(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("parseDashboardArgs(%v) unexpectedly asked for help", argv)
	}
	return cfg
}

// parseDashboardUsageError parses an argv that is expected to be rejected.
func parseDashboardUsageError(t *testing.T, argv ...string) error {
	t.Helper()
	_, help, err := parseDashboardArgs(argv)
	if help {
		t.Fatalf("parseDashboardArgs(%v) unexpectedly asked for help", argv)
	}
	return err
}

func dashboardMustBool(t *testing.T, got cli.Bool, flag string, want bool) {
	t.Helper()
	if got.Value != want {
		t.Fatalf("%s = %v, want %v", flag, got.Value, want)
	}
	if got.Provided != want {
		t.Fatalf("%s: provided = %v, want %v", flag, got.Provided, want)
	}
}

func dashboardMustString(t *testing.T, got cli.String, flag string, want string) {
	t.Helper()
	if !got.Provided {
		t.Fatalf("%s: want string(%q), got not provided", flag, want)
	}
	if got.Value != want {
		t.Fatalf("%s = %q, want %q", flag, got.Value, want)
	}
}

func dashboardMustUnset(t *testing.T, got cli.String, flag string) {
	t.Helper()
	if got.Provided {
		t.Fatalf("%s = %#v, want not provided", flag, got.Value)
	}
}

func dashboardStringArg(cfg *dashboardConfig, flag string) cli.String {
	switch flag {
	case "--ncpu":
		return cfg.NCPU
	case "--config":
		return cfg.Config
	case "--log":
		return cfg.Log
	case "--log-level":
		return cfg.LogLevel
	case "--host-admin":
		return cfg.HostAdmin
	case "--pidfile":
		return cfg.Pidfile
	case "--zookeeper":
		return cfg.Coordinator.Zookeeper
	case "--filesystem":
		return cfg.Coordinator.Filesystem
	case "--consul":
		return cfg.Coordinator.Consul
	case "--product_name":
		return cfg.ProductName
	case "--product_auth":
		return cfg.ProductAuth
	default:
		panic("unknown dashboard flag " + flag)
	}
}

var dashboardStringFlags = []string{
	"--ncpu", "--config", "--log", "--log-level", "--host-admin", "--pidfile",
	"--zookeeper", "--filesystem", "--consul",
	"--product_name", "--product_auth",
}

var dashboardCoordinatorFlags = []string{"--zookeeper", "--filesystem", "--consul"}

func TestDashboardOneShotFlagsParseAsBools(t *testing.T) {
	tests := []struct {
		name       string
		argv       []string
		wantConfig bool
		wantVer    bool
	}{
		{"no flags", nil, false, false},
		{"default-config", []string{"--default-config"}, true, false},
		{"version", []string{"--version"}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseDashboardUsage(t, tt.argv...)
			dashboardMustBool(t, cfg.DefaultConfig, "--default-config", tt.wantConfig)
			dashboardMustBool(t, cfg.Version, "--version", tt.wantVer)
			if tt.wantConfig || tt.wantVer {
				for _, flag := range dashboardStringFlags {
					dashboardMustUnset(t, dashboardStringArg(cfg, flag), flag)
				}
			}
		})
	}
}

func TestDashboardOneShotFlagsAreExclusive(t *testing.T) {
	tests := [][]string{
		{"--default-config", "--version"},
		{"--default-config", "--zookeeper=zk"},
		{"--version", "--zookeeper=zk", "--remove-lock"},
		{"--version", "--config=/tmp/topom.conf"},
	}
	for _, argv := range tests {
		if err := parseDashboardUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestDashboardStringFlagsAbsentAreNil pins the "not provided" contract for
// every value-taking flag. The dashboard usage intentionally has no
// --*-auth options; those are covered by
// TestDashboardRejectsCoordinatorAuthFlags below.
func TestDashboardStringFlagsAbsentAreNil(t *testing.T) {
	cfg := parseDashboardUsage(t)
	for _, flag := range dashboardStringFlags {
		dashboardMustUnset(t, dashboardStringArg(cfg, flag), flag)
	}
}

func TestDashboardStringFlagsBindValues(t *testing.T) {
	argv := []string{
		"--ncpu=4", "--config=/etc/topom.conf", "--log=/tmp/dashboard.log",
		"--log-level=WARN", "--host-admin=:18080", "--pidfile=/tmp/dashboard.pid",
		"--zookeeper=127.0.0.1:2181", "--product_name=test-product",
		"--product_auth=product-secret",
	}
	want := map[string]string{
		"--ncpu": "4", "--config": "/etc/topom.conf", "--log": "/tmp/dashboard.log",
		"--log-level": "WARN", "--host-admin": ":18080", "--pidfile": "/tmp/dashboard.pid",
		"--zookeeper": "127.0.0.1:2181", "--product_name": "test-product",
		"--product_auth": "product-secret",
	}
	cfg := parseDashboardUsage(t, argv...)
	for flag, value := range want {
		dashboardMustString(t, dashboardStringArg(cfg, flag), flag, value)
	}
	dashboardMustBool(t, cfg.DefaultConfig, "--default-config", false)
	dashboardMustBool(t, cfg.Version, "--version", false)
	dashboardMustBool(t, cfg.RemoveLock, "--remove-lock", false)
	dashboardMustUnset(t, dashboardStringArg(cfg, "--filesystem"), "--filesystem")
	dashboardMustUnset(t, dashboardStringArg(cfg, "--consul"), "--consul")
}

func TestDashboardShortAliases(t *testing.T) {
	cfg := parseDashboardUsage(t, "-c", "/etc/topom.conf", "-l", "/tmp/dashboard.log", "--zookeeper=zk")
	dashboardMustString(t, cfg.Config, "--config", "/etc/topom.conf")
	dashboardMustString(t, cfg.Log, "--log", "/tmp/dashboard.log")
	dashboardMustString(t, cfg.Coordinator.Zookeeper, "--zookeeper", "zk")
}

// TestDashboardCoordinatorChoices covers the three mutual-exclusive coordinator
// addresses accepted by codis-dashboard.
func TestDashboardCoordinatorChoices(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want map[string]string
	}{
		{"zookeeper", []string{"--zookeeper=127.0.0.1:2181"}, map[string]string{"--zookeeper": "127.0.0.1:2181"}},
		{"filesystem", []string{"--filesystem=/tmp/codis"}, map[string]string{"--filesystem": "/tmp/codis"}},
		{"consul", []string{"--consul=127.0.0.1:8500"}, map[string]string{"--consul": "127.0.0.1:8500"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseDashboardUsage(t, tt.argv...)
			for _, flag := range dashboardCoordinatorFlags {
				if value, ok := tt.want[flag]; ok {
					dashboardMustString(t, dashboardStringArg(cfg, flag), flag, value)
				} else {
					dashboardMustUnset(t, dashboardStringArg(cfg, flag), flag)
				}
			}
		})
	}
}

func TestDashboardCoordinatorChoicesRejectMultipleSources(t *testing.T) {
	sources := [][]string{
		{"--zookeeper=zk"},
		{"--filesystem=/tmp/codis"},
		{"--consul=consul"},
	}
	for i := 0; i < len(sources); i++ {
		for j := i + 1; j < len(sources); j++ {
			argv := append(append([]string{}, sources[i]...), sources[j]...)
			if err := parseDashboardUsageError(t, argv...); err == nil {
				t.Fatalf("argv %v: expected parse error for conflicting coordinators", argv)
			}
		}
	}
}

// TestDashboardRejectsCoordinatorAuthFlags documents current behavior: unlike
// codis-proxy/codis-admin/codis-fe, the dashboard usage line has no
// --*-auth options, so coordinator credentials can only come from --config.
// This is pinned so a flag migration does not silently change CLI acceptance.
func TestDashboardRejectsCoordinatorAuthFlags(t *testing.T) {
	tests := [][]string{
		{"--zookeeper=zk", "--zookeeper-auth=user:pass"},
		{"--consul=consul", "--consul-auth=token"},
		{"--zookeeper-auth=user:pass"},
	}
	for _, argv := range tests {
		if err := parseDashboardUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error, dashboard has no coordinator auth flags", argv)
		}
	}
}

// TestDashboardRemoveLockIsBool pins the typed boolean behavior used by main().
func TestDashboardRemoveLockIsBool(t *testing.T) {
	cfg := parseDashboardUsage(t, "--zookeeper=zk")
	dashboardMustBool(t, cfg.RemoveLock, "--remove-lock", false)

	cfg = parseDashboardUsage(t, "--zookeeper=zk", "--remove-lock")
	dashboardMustBool(t, cfg.RemoveLock, "--remove-lock", true)
}

func TestDashboardHelp(t *testing.T) {
	cfg, help, err := parseDashboardArgs([]string{"-h"})
	if err != nil {
		t.Fatalf("parseDashboardArgs(-h) failed: %v", err)
	}
	if !help || cfg != nil {
		t.Fatalf("cfg = %+v, help = %v; want nil, true", cfg, help)
	}
}
