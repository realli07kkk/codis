// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
	"github.com/CodisLabs/codis/pkg/topom"
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
	"--product_name", "--product_auth",
}

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
		{"--default-config", "--etcd=http://127.0.0.1:2379"},
		{"--version", "--etcd=http://127.0.0.1:2379"},
		{"--version", "--config=/tmp/topom.conf"},
	}
	for _, argv := range tests {
		if err := parseDashboardUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestDashboardStringFlagsAbsentAreNil pins the "not provided" contract for
// every value-taking flag. The connection options are checked separately.
func TestDashboardStringFlagsAbsentAreNil(t *testing.T) {
	cfg := parseDashboardUsage(t)
	for _, flag := range dashboardStringFlags {
		dashboardMustUnset(t, dashboardStringArg(cfg, flag), flag)
	}
	if cfg.Coordinator.Etcd.Provided || cfg.Coordinator.EtcdConfig.Provided {
		t.Fatalf("connection options are provided by default: %+v", cfg.Coordinator)
	}
}

func TestDashboardStringFlagsBindValues(t *testing.T) {
	argv := []string{
		"--ncpu=4", "--config=/etc/topom.conf", "--log=/tmp/dashboard.log",
		"--log-level=WARN", "--host-admin=:18080", "--pidfile=/tmp/dashboard.pid",
		"--product_name=test-product", "--product_auth=product-secret",
	}
	want := map[string]string{
		"--ncpu": "4", "--config": "/etc/topom.conf", "--log": "/tmp/dashboard.log",
		"--log-level": "WARN", "--host-admin": ":18080", "--pidfile": "/tmp/dashboard.pid",
		"--product_name": "test-product", "--product_auth": "product-secret",
	}
	cfg := parseDashboardUsage(t, argv...)
	for flag, value := range want {
		dashboardMustString(t, dashboardStringArg(cfg, flag), flag, value)
	}
	dashboardMustBool(t, cfg.DefaultConfig, "--default-config", false)
	dashboardMustBool(t, cfg.Version, "--version", false)
}

func TestDashboardShortAliases(t *testing.T) {
	cfg := parseDashboardUsage(t, "-c", "/etc/topom.conf", "-l", "/tmp/dashboard.log")
	dashboardMustString(t, cfg.Config, "--config", "/etc/topom.conf")
	dashboardMustString(t, cfg.Log, "--log", "/tmp/dashboard.log")
}

func TestDashboardConnectionFlagsBind(t *testing.T) {
	cfg := parseDashboardUsage(t, "--etcd=http://10.0.0.1:2379,http://10.0.0.2:2379")
	if !cfg.Coordinator.Etcd.Provided {
		t.Fatal("--etcd not recorded")
	}
	if got := cfg.Coordinator.Etcd.Value; got != "http://10.0.0.1:2379,http://10.0.0.2:2379" {
		t.Fatalf("--etcd = %q", got)
	}
	if cfg.Coordinator.EtcdConfig.Provided {
		t.Fatal("--etcd-config reported as provided")
	}

	cfg = parseDashboardUsage(t, "--etcd-config=/etc/etcd.toml")
	if !cfg.Coordinator.EtcdConfig.Provided {
		t.Fatal("--etcd-config not recorded")
	}
	if got := cfg.Coordinator.EtcdConfig.Value; got != "/etc/etcd.toml" {
		t.Fatalf("--etcd-config = %q", got)
	}
	if cfg.Coordinator.Etcd.Provided {
		t.Fatal("--etcd reported as provided")
	}
}

// TestDashboardEtcdOverridesCoordinatorAddr pins the documented precedence:
// an explicit --etcd wins over the dashboard coordinator_addr.
func TestDashboardEtcdOverridesCoordinatorAddr(t *testing.T) {
	cfg := parseDashboardUsage(t, "--etcd=http://etcd-from-flag:2379")
	built, err := cfg.CoordinatorArg.Build("http://coordinator-addr:2379")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if len(built.Endpoints) != 1 || built.Endpoints[0] != "http://etcd-from-flag:2379" {
		t.Fatalf("endpoints = %v, want the --etcd value", built.Endpoints)
	}
}

// TestDashboardCoordinatorAddrFallback pins that coordinator_addr is used when
// neither --etcd nor --etcd-config names endpoints.
func TestDashboardCoordinatorAddrFallback(t *testing.T) {
	cfg := parseDashboardUsage(t)
	built, err := cfg.CoordinatorArg.Build("http://coordinator-addr:2379")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if len(built.Endpoints) != 1 || built.Endpoints[0] != "http://coordinator-addr:2379" {
		t.Fatalf("endpoints = %v, want coordinator_addr", built.Endpoints)
	}
}

// TestDashboardEtcdConfigIsAdopted pins that the connection file is loaded and
// its endpoints take precedence over coordinator_addr.
func TestDashboardEtcdConfigIsAdopted(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "etcd.toml")
	content := "endpoints = [\"https://etcd-from-file:2379\"]\n" +
		"username = \"codis\"\npassword = \"file-secret\"\n"
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatalf("write connection file: %v", err)
	}

	cfg := parseDashboardUsage(t, "--etcd-config="+file)
	built, err := cfg.CoordinatorArg.Build("http://coordinator-addr:2379")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if len(built.Endpoints) != 1 || built.Endpoints[0] != "https://etcd-from-file:2379" {
		t.Fatalf("endpoints = %v, want the file endpoints", built.Endpoints)
	}
	if built.Username != "codis" {
		t.Fatalf("username = %q, want the file value", built.Username)
	}
	if strings.Contains(built.String(), "file-secret") {
		t.Fatalf("formatted config leaks the password: %s", built.String())
	}
}

// TestDashboardEtcdConfigInvalidIsRejectedBeforeConnect pins that a broken
// connection file is reported by Build, i.e. before any connection attempt.
func TestDashboardEtcdConfigInvalidIsRejectedBeforeConnect(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "etcd.toml")
	if err := os.WriteFile(file, []byte("endpoints = [\"127.0.0.1:2379\"]\n"), 0600); err != nil {
		t.Fatalf("write connection file: %v", err)
	}
	cfg := parseDashboardUsage(t, "--etcd-config="+file)
	if _, err := cfg.CoordinatorArg.Build("http://coordinator-addr:2379"); err == nil {
		t.Fatal("Build() accepted an invalid connection file")
	}

	cfg = parseDashboardUsage(t, "--etcd-config="+filepath.Join(dir, "missing.toml"))
	if _, err := cfg.CoordinatorArg.Build("http://coordinator-addr:2379"); err == nil {
		t.Fatal("Build() accepted a missing connection file")
	}
}

// TestDashboardEtcdEmptyIsRejected pins that an explicitly empty --etcd is an
// error instead of a silent fallback to another address.
func TestDashboardEtcdEmptyIsRejected(t *testing.T) {
	cfg := parseDashboardUsage(t, "--etcd=")
	if !cfg.Coordinator.Etcd.Provided {
		t.Fatal("--etcd= not recorded as provided")
	}
	_, err := cfg.CoordinatorArg.Build("http://coordinator-addr:2379")
	if err == nil {
		t.Fatal("Build() accepted an empty --etcd")
	}
	if !strings.Contains(err.Error(), "endpoints") {
		t.Fatalf("error = %v, want an endpoints error", err)
	}
}

// TestDashboardRejectsRemovedCoordinatorFlags pins that the removed backends
// fail with the last supporting commit and never echo the supplied value.
func TestDashboardRejectsRemovedCoordinatorFlags(t *testing.T) {
	tests := [][]string{
		{"--zookeeper=127.0.0.1:2181"},
		{"--zookeeper=127.0.0.1:2181", "--zookeeper-auth=user:secret-pass"},
		{"--filesystem=/tmp/codis"},
		{"--consul=127.0.0.1:8500", "--consul-auth=secret-token"},
	}
	for _, argv := range tests {
		err := parseDashboardUsageError(t, argv...)
		if err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
		if !strings.Contains(err.Error(), coordarg.DeprecatedCoordinatorSHA) {
			t.Fatalf("argv %v: error %q does not name the deprecated commit", argv, err)
		}
		for _, secret := range []string{"secret-pass", "secret-token"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("argv %v: error echoes the value: %q", argv, err)
			}
		}
	}
}

// TestDashboardRemoveLockIsGone pins that the dashboard no longer offers
// --remove-lock; lock recovery is an operator action of codis-admin.
func TestDashboardRemoveLockIsGone(t *testing.T) {
	err := parseDashboardUsageError(t, "--remove-lock")
	if err == nil {
		t.Fatal("--remove-lock is still accepted")
	}
	if !strings.Contains(err.Error(), "remove-lock") {
		t.Fatalf("error = %v, want an unknown flag error", err)
	}
	if strings.Contains(dashboardUsage, "remove-lock") {
		t.Fatal("dashboard usage still documents --remove-lock")
	}
	for _, removed := range []string{"--zookeeper", "--filesystem", "--consul"} {
		if strings.Contains(dashboardUsage, removed) {
			t.Fatalf("dashboard usage still documents %s", removed)
		}
	}
	for _, kept := range []string{"--etcd", "--etcd-config", "coordinator_addr"} {
		if !strings.Contains(dashboardUsage, kept) {
			t.Fatalf("dashboard usage does not document %s", kept)
		}
	}
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

// TestDashboardDefaultConfigFileLoads pins that the packaged
// config/dashboard.toml is byte-identical to what --default-config prints
// (the Makefile default-configs target regenerates it that way) and that the
// current binary can load and validate it. Validate also rejects a
// coordinator_auth key, so the load proves the shipped file carries none.
func TestDashboardDefaultConfigFileLoads(t *testing.T) {
	const path = "../../config/dashboard.toml"
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(blob) != topom.DefaultConfig {
		t.Fatalf("%s is stale: regenerate it with 'codis-dashboard --default-config'", path)
	}

	c := topom.NewDefaultConfig()
	if err := c.LoadFromFile(path); err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate %s: %v", path, err)
	}
	if c.CoordinatorName != topom.CoordinatorNameEtcd {
		t.Fatalf("coordinator_name = %q, want %q", c.CoordinatorName, topom.CoordinatorNameEtcd)
	}
	if c.CoordinatorAddr != "http://127.0.0.1:2379" {
		t.Fatalf("coordinator_addr = %q, want the built-in etcd endpoint", c.CoordinatorAddr)
	}
}
