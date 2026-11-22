// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

// parseProxyUsage drives the new typed parser, mirroring main() except that
// argv is explicit.
func parseProxyUsage(t *testing.T, argv ...string) *proxyConfig {
	t.Helper()
	cfg, help, err := parseProxyArgs(argv)
	if err != nil {
		t.Fatalf("parseProxyArgs(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("parseProxyArgs(%v) unexpectedly asked for help", argv)
	}
	return cfg
}

// parseProxyUsageError parses an argv that is expected to be rejected.
func parseProxyUsageError(t *testing.T, argv ...string) error {
	t.Helper()
	_, help, err := parseProxyArgs(argv)
	if help {
		t.Fatalf("parseProxyArgs(%v) unexpectedly asked for help", argv)
	}
	return err
}

func proxyMustBool(t *testing.T, got cli.Bool, flag string, want bool) {
	t.Helper()
	if got.Value != want {
		t.Fatalf("%s = %v, want %v", flag, got.Value, want)
	}
	// Boolean options take no argument, so "provided" is equivalent to true.
	if got.Provided != want {
		t.Fatalf("%s: provided = %v, want %v", flag, got.Provided, want)
	}
}

func proxyMustString(t *testing.T, got cli.String, flag string, want string) {
	t.Helper()
	if !got.Provided {
		t.Fatalf("%s: want string(%q), got not provided", flag, want)
	}
	if got.Value != want {
		t.Fatalf("%s = %q, want %q", flag, got.Value, want)
	}
}

func proxyMustUnset(t *testing.T, got cli.String, flag string) {
	t.Helper()
	if got.Provided {
		t.Fatalf("%s = %#v, want not provided", flag, got.Value)
	}
}

func proxyStringArg(cfg *proxyConfig, flag string) cli.String {
	switch flag {
	case "--ncpu":
		return cfg.NCPU
	case "--max-ncpu":
		return cfg.MaxNCPU
	case "--config":
		return cfg.Config
	case "--log":
		return cfg.Log
	case "--log-level":
		return cfg.LogLevel
	case "--host-admin":
		return cfg.HostAdmin
	case "--host-proxy":
		return cfg.HostProxy
	case "--dashboard":
		return cfg.Dashboard
	case "--zookeeper":
		return cfg.Coordinator.Zookeeper
	case "--zookeeper-auth":
		return cfg.Coordinator.ZookeeperAuth
	case "--etcd":
		return cfg.Coordinator.Etcd
	case "--etcd-auth":
		return cfg.Coordinator.EtcdAuth
	case "--filesystem":
		return cfg.Coordinator.Filesystem
	case "--consul":
		return cfg.Coordinator.Consul
	case "--consul-auth":
		return cfg.Coordinator.ConsulAuth
	case "--fillslots":
		return cfg.FillSlots
	case "--ulimit":
		return cfg.Ulimit
	case "--pidfile":
		return cfg.Pidfile
	case "--product_name":
		return cfg.ProductName
	case "--product_auth":
		return cfg.ProductAuth
	case "--session_auth":
		return cfg.SessionAuth
	default:
		panic("unknown proxy flag " + flag)
	}
}

var proxyStringFlags = []string{
	"--ncpu", "--max-ncpu", "--config", "--log", "--log-level",
	"--host-admin", "--host-proxy", "--dashboard",
	"--zookeeper", "--zookeeper-auth", "--etcd", "--etcd-auth",
	"--filesystem", "--consul", "--consul-auth", "--fillslots",
	"--ulimit", "--pidfile", "--product_name", "--product_auth", "--session_auth",
}

var proxyCoordinatorFlags = []string{
	"--dashboard", "--zookeeper", "--zookeeper-auth", "--etcd", "--etcd-auth",
	"--filesystem", "--consul", "--consul-auth", "--fillslots",
}

// TestProxyOneShotFlagsParseAsBools pins that --default-config/--version are
// plain booleans that default to false and are recorded as explicitly provided
// only when present.
func TestProxyOneShotFlagsParseAsBools(t *testing.T) {
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
			cfg := parseProxyUsage(t, tt.argv...)
			proxyMustBool(t, cfg.DefaultConfig, "--default-config", tt.wantConfig)
			proxyMustBool(t, cfg.Version, "--version", tt.wantVer)
			if tt.wantConfig || tt.wantVer {
				for _, flag := range proxyStringFlags {
					proxyMustUnset(t, proxyStringArg(cfg, flag), flag)
				}
			}
		})
	}
}

// TestProxyOneShotFlagsAreExclusive pins that --default-config and --version
// are standalone usage lines; the parser must keep rejecting extra arguments
// instead of silently accepting (and ignoring) them.
func TestProxyOneShotFlagsAreExclusive(t *testing.T) {
	tests := [][]string{
		{"--default-config", "--version"},
		{"--default-config", "--ncpu=2"},
		{"--default-config", "--zookeeper=zk"},
		{"--version", "--config=/tmp/codis.conf"},
		{"--version", "--log=/tmp/proxy.log"},
		{"--version", "--dashboard=127.0.0.1:18080"},
	}
	for _, argv := range tests {
		if err := parseProxyUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestProxyStringFlagsAbsentAreNil pins the second half of the legacy
// contract: absent value-taking flags are not provided (not empty strings).
func TestProxyStringFlagsAbsentAreNil(t *testing.T) {
	cfg := parseProxyUsage(t)
	for _, flag := range proxyStringFlags {
		proxyMustUnset(t, proxyStringArg(cfg, flag), flag)
	}
}

func TestProxyStringFlagsBindValues(t *testing.T) {
	argv := []string{
		"--ncpu=8", "--max-ncpu=16", "--config=/etc/codis.conf",
		"--log=/tmp/proxy.log", "--log-level=DEBUG",
		"--host-admin=:11080", "--host-proxy=:19000",
		"--dashboard=127.0.0.1:18080", "--ulimit=65535",
		"--pidfile=/tmp/proxy.pid", "--product_name=test-product",
		"--product_auth=product-secret", "--session_auth=session-secret",
	}
	want := map[string]string{
		"--ncpu": "8", "--max-ncpu": "16", "--config": "/etc/codis.conf",
		"--log": "/tmp/proxy.log", "--log-level": "DEBUG",
		"--host-admin": ":11080", "--host-proxy": ":19000",
		"--dashboard": "127.0.0.1:18080", "--ulimit": "65535",
		"--pidfile": "/tmp/proxy.pid", "--product_name": "test-product",
		"--product_auth": "product-secret", "--session_auth": "session-secret",
	}
	cfg := parseProxyUsage(t, argv...)
	for flag, value := range want {
		proxyMustString(t, proxyStringArg(cfg, flag), flag, value)
	}
	// Value-taking flags stay strings even when they are numeric.
	proxyMustBool(t, cfg.DefaultConfig, "--default-config", false)
	proxyMustBool(t, cfg.Version, "--version", false)
	proxyMustUnset(t, proxyStringArg(cfg, "--fillslots"), "--fillslots")
	proxyMustUnset(t, proxyStringArg(cfg, "--zookeeper"), "--zookeeper")
}

// TestProxyShortAliases verifies the short spellings that map onto their long
// flag values.
func TestProxyShortAliases(t *testing.T) {
	cfg := parseProxyUsage(t, "-c", "/etc/codis.conf", "-l", "/tmp/proxy.log", "--zookeeper=zk")
	proxyMustString(t, cfg.Config, "--config", "/etc/codis.conf")
	proxyMustString(t, cfg.Log, "--log", "/tmp/proxy.log")
	proxyMustString(t, cfg.Coordinator.Zookeeper, "--zookeeper", "zk")
}

// TestProxyCoordinatorChoices covers the six mutually exclusive online modes
// (dashboard or one coordinator or a slots file), including the auth flags
// that are nested under their coordinator.
func TestProxyCoordinatorChoices(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want map[string]string
	}{
		{"dashboard", []string{"--dashboard=127.0.0.1:18080"}, map[string]string{"--dashboard": "127.0.0.1:18080"}},
		{"zookeeper", []string{"--zookeeper=127.0.0.1:2181"}, map[string]string{"--zookeeper": "127.0.0.1:2181"}},
		{"zookeeper auth", []string{"--zookeeper=127.0.0.1:2181", "--zookeeper-auth=user:pass"}, map[string]string{"--zookeeper": "127.0.0.1:2181", "--zookeeper-auth": "user:pass"}},
		{"etcd", []string{"--etcd=127.0.0.1:2379"}, map[string]string{"--etcd": "127.0.0.1:2379"}},
		{"etcd auth", []string{"--etcd=127.0.0.1:2379", "--etcd-auth=user:pass"}, map[string]string{"--etcd": "127.0.0.1:2379", "--etcd-auth": "user:pass"}},
		{"filesystem", []string{"--filesystem=/tmp/codis"}, map[string]string{"--filesystem": "/tmp/codis"}},
		{"consul", []string{"--consul=127.0.0.1:8500"}, map[string]string{"--consul": "127.0.0.1:8500"}},
		{"consul auth", []string{"--consul=127.0.0.1:8500", "--consul-auth=token"}, map[string]string{"--consul": "127.0.0.1:8500", "--consul-auth": "token"}},
		{"fillslots", []string{"--fillslots=/tmp/slots.json"}, map[string]string{"--fillslots": "/tmp/slots.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseProxyUsage(t, tt.argv...)
			for _, flag := range proxyCoordinatorFlags {
				if value, ok := tt.want[flag]; ok {
					proxyMustString(t, proxyStringArg(cfg, flag), flag, value)
				} else {
					proxyMustUnset(t, proxyStringArg(cfg, flag), flag)
				}
			}
		})
	}
}

// TestProxyLongFlagSpaceSeparatedValue pins that both `--flag=value` and
// `--flag value` spellings are accepted.
func TestProxyLongFlagSpaceSeparatedValue(t *testing.T) {
	cfg := parseProxyUsage(t, "--config", "/etc/codis.conf", "--dashboard", "127.0.0.1:18080")
	proxyMustString(t, cfg.Config, "--config", "/etc/codis.conf")
	proxyMustString(t, cfg.Dashboard, "--dashboard", "127.0.0.1:18080")
}

func TestProxyCoordinatorChoicesRejectMultipleSources(t *testing.T) {
	sources := [][]string{
		{"--dashboard=127.0.0.1:18080"},
		{"--zookeeper=127.0.0.1:2181", "--zookeeper-auth=user:pass"},
		{"--etcd=127.0.0.1:2379", "--etcd-auth=user:pass"},
		{"--filesystem=/tmp/codis"},
		{"--consul=127.0.0.1:8500", "--consul-auth=token"},
		{"--fillslots=/tmp/slots.json"},
	}
	for i := 0; i < len(sources); i++ {
		for j := i + 1; j < len(sources); j++ {
			argv := append(append([]string{}, sources[i]...), sources[j]...)
			if err := parseProxyUsageError(t, argv...); err == nil {
				t.Fatalf("argv %v: expected parse error for conflicting online sources", argv)
			}
		}
	}
}

// TestProxyAuthFlagsRequireTheirCoordinator pins that --*-auth is only valid
// together with the matching coordinator address.
func TestProxyAuthFlagsRequireTheirCoordinator(t *testing.T) {
	for _, flag := range []string{"--zookeeper-auth=user:pass", "--etcd-auth=user:pass", "--consul-auth=token"} {
		if err := parseProxyUsageError(t, flag); err == nil {
			t.Fatalf("argv %q: expected parse error without coordinator address", flag)
		}
	}
}

func TestProxyMaxNCPURequiresNCPU(t *testing.T) {
	if err := parseProxyUsageError(t, "--max-ncpu=8"); err == nil {
		t.Fatal("expected --max-ncpu to require --ncpu")
	}
}

func TestProxyHelp(t *testing.T) {
	cfg, help, err := parseProxyArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("parseProxyArgs(--help) failed: %v", err)
	}
	if !help || cfg != nil {
		t.Fatalf("cfg = %+v, help = %v; want nil, true", cfg, help)
	}
}
