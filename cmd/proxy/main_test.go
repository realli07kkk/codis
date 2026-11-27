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
	"github.com/CodisLabs/codis/pkg/proxy"
)

// parseProxyUsage drives the typed parser, mirroring main() except that argv
// is explicit.
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
	"--host-admin", "--host-proxy", "--dashboard", "--fillslots",
	"--ulimit", "--pidfile", "--product_name", "--product_auth", "--session_auth",
}

// proxyMustEtcd pins the shared connection options selected by the parse.
func proxyMustEtcd(t *testing.T, cfg *proxyConfig, endpoints, configFile string) {
	t.Helper()
	if cfg.Etcd.HasEndpoints != (endpoints != "") || cfg.Etcd.EndpointsFlag != endpoints {
		t.Fatalf("--etcd = %q (set %v), want %q", cfg.Etcd.EndpointsFlag, cfg.Etcd.HasEndpoints, endpoints)
	}
	if cfg.Etcd.HasConfigFile != (configFile != "") || cfg.Etcd.ConfigFile != configFile {
		t.Fatalf("--etcd-config = %q (set %v), want %q", cfg.Etcd.ConfigFile, cfg.Etcd.HasConfigFile, configFile)
	}
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
				proxyMustEtcd(t, cfg, "", "")
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
		{"--default-config", "--etcd=http://127.0.0.1:2379"},
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
	proxyMustEtcd(t, cfg, "", "")
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
	proxyMustEtcd(t, cfg, "", "")
}

// TestProxyShortAliases verifies the short spellings that map onto their long
// flag values.
func TestProxyShortAliases(t *testing.T) {
	cfg := parseProxyUsage(t, "-c", "/etc/codis.conf", "-l", "/tmp/proxy.log")
	proxyMustString(t, cfg.Config, "--config", "/etc/codis.conf")
	proxyMustString(t, cfg.Log, "--log", "/tmp/proxy.log")
}

// TestProxyOnlineMode pins the automatic online entry selected by each argv.
// --etcd-config is not an entry: it only supplies connection options, so a
// proxy started with just that file must not talk to etcd at startup.
func TestProxyOnlineMode(t *testing.T) {
	tests := []struct {
		name       string
		argv       []string
		want       onlineSource
		configFile string
	}{
		{"manual", nil, onlineManual, ""},
		{"dashboard", []string{"--dashboard=127.0.0.1:18080"}, onlineDashboard, ""},
		{"etcd", []string{"--etcd=http://127.0.0.1:2379"}, onlineEtcd, ""},
		{"fillslots", []string{"--fillslots=/tmp/slots.json"}, onlineFillSlots, ""},
		{"etcd config only", []string{"--etcd-config=/etc/codis/etcd.toml"}, onlineManual, "/etc/codis/etcd.toml"},
		{"dashboard with etcd config", []string{"--dashboard=127.0.0.1:18080", "--etcd-config=/etc/codis/etcd.toml"}, onlineDashboard, "/etc/codis/etcd.toml"},
		{"etcd with etcd config", []string{"--etcd=http://127.0.0.1:2379", "--etcd-config=/etc/codis/etcd.toml"}, onlineEtcd, "/etc/codis/etcd.toml"},
		{"fillslots with etcd config", []string{"--fillslots=/tmp/slots.json", "--etcd-config=/etc/codis/etcd.toml"}, onlineFillSlots, "/etc/codis/etcd.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseProxyUsage(t, tt.argv...)
			if got := cfg.onlineMode(); got != tt.want {
				t.Fatalf("onlineMode() = %v, want %v", got, tt.want)
			}
			if !cfg.Etcd.HasConfigFile != (tt.configFile == "") {
				t.Fatalf("--etcd-config set = %v, want %v", cfg.Etcd.HasConfigFile, tt.configFile != "")
			}
			if cfg.Etcd.ConfigFile != tt.configFile {
				t.Fatalf("--etcd-config = %q, want %q", cfg.Etcd.ConfigFile, tt.configFile)
			}
		})
	}
}

// TestProxyOnlineEntriesAreMutuallyExclusive pins that at most one automatic
// online entry may be given.
func TestProxyOnlineEntriesAreMutuallyExclusive(t *testing.T) {
	sources := [][]string{
		{"--dashboard=127.0.0.1:18080"},
		{"--etcd=http://127.0.0.1:2379"},
		{"--fillslots=/tmp/slots.json"},
	}
	for i := 0; i < len(sources); i++ {
		for j := i + 1; j < len(sources); j++ {
			argv := append(append([]string{}, sources[i]...), sources[j]...)
			err := parseProxyUsageError(t, argv...)
			if err == nil {
				t.Fatalf("argv %v: expected parse error for conflicting online entries", argv)
			}
			if !strings.Contains(err.Error(), "at most one") {
				t.Fatalf("argv %v: error %q does not explain the exclusion", argv, err)
			}
		}
	}
}

// TestProxyEtcdConfigCombinesWithEntries pins that --etcd-config only carries
// connection options and therefore combines with every online entry, including
// the "dashboard plus etcd registration" combination.
func TestProxyEtcdConfigCombinesWithEntries(t *testing.T) {
	config := "--etcd-config=/etc/codis/etcd.toml"
	for _, entry := range [][]string{
		{"--dashboard=127.0.0.1:18080"},
		{"--etcd=http://127.0.0.1:2379"},
		{"--fillslots=/tmp/slots.json"},
		{},
	} {
		argv := append(append([]string{}, entry...), config)
		cfg := parseProxyUsage(t, argv...)
		proxyMustEtcd(t, cfg, endpointsOf(entry), "/etc/codis/etcd.toml")
	}
}

// endpointsOf returns the --etcd value of an entry list, if any.
func endpointsOf(argv []string) string {
	for _, arg := range argv {
		if v, ok := strings.CutPrefix(arg, "--etcd="); ok {
			return v
		}
	}
	return ""
}

// TestProxyLegacyCoordinatorFlagsAreRejected pins that the removed coordinator
// options fail with the flag name and the last supporting commit, and that
// their values never reach the error text.
func TestProxyLegacyCoordinatorFlagsAreRejected(t *testing.T) {
	tests := []struct {
		name   string
		argv   []string
		flag   string
		secret string
	}{
		{"zookeeper", []string{"--zookeeper=zk.example:2181"}, "zookeeper", "zk.example"},
		{"zookeeper auth", []string{"--zookeeper=zk.example:2181", "--zookeeper-auth=user:zk-secret"}, "zookeeper", "zk-secret"},
		{"filesystem", []string{"--filesystem=/tmp/codis-fs"}, "filesystem", "/tmp/codis-fs"},
		{"consul", []string{"--consul=consul.example:8500"}, "consul", "consul.example"},
		{"consul auth", []string{"--consul=consul.example:8500", "--consul-auth=consul-secret"}, "consul", "consul-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseProxyUsageError(t, tt.argv...)
			if err == nil {
				t.Fatalf("argv %v: expected parse error", tt.argv)
			}
			if !strings.Contains(err.Error(), coordarg.DeprecatedCoordinatorSHA) {
				t.Fatalf("error %q does not point at %s", err, coordarg.DeprecatedCoordinatorSHA)
			}
			if !strings.Contains(err.Error(), "--"+tt.flag) {
				t.Fatalf("error %q does not name --%s", err, tt.flag)
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("error %q echoes the option value", err)
			}
		})
	}
}

// TestProxyUsageHasNoLegacyCoordinator pins the help text: the removed
// backends are gone, the three online entries are documented as mutually
// exclusive and --etcd-config is documented as connection options only.
func TestProxyUsageHasNoLegacyCoordinator(t *testing.T) {
	for _, legacy := range []string{"zookeeper", "filesystem", "consul"} {
		if strings.Contains(proxyUsage, legacy) {
			t.Fatalf("usage still mentions %q:\n%s", legacy, proxyUsage)
		}
	}
	for _, want := range []string{"--dashboard", "--etcd", "--etcd-config", "--fillslots", "mutually"} {
		if !strings.Contains(proxyUsage, want) {
			t.Fatalf("usage does not mention %q:\n%s", want, proxyUsage)
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

// TestProxyOnlineRegistrationFactoryDisabled pins that a proxy without
// etcd_register_online never gets a registry factory, so it cannot reach the
// coordinator.
func TestProxyOnlineRegistrationFactoryDisabled(t *testing.T) {
	cfg := parseProxyUsage(t)
	if f := onlineRegistryFactory(cfg.Etcd, false); f != nil {
		t.Fatal("a factory must not be created when etcd_register_online is off")
	}
}

// TestProxyOnlineRegistrationFallsBackToDefaultEndpoints pins the documented
// address precedence: a registering proxy given no shared options still resolves
// the built-in default endpoints, and building the registry does not fail
// (nothing is connected until the registration loop runs).
func TestProxyOnlineRegistrationFallsBackToDefaultEndpoints(t *testing.T) {
	cfg := parseProxyUsage(t)
	factory := onlineRegistryFactory(cfg.Etcd, true)
	if factory == nil {
		t.Fatal("no registry factory with etcd_register_online on")
	}

	got, err := cfg.Etcd.Build("")
	if err != nil {
		t.Fatalf("build connection config: %v", err)
	}
	want, err := coordarg.Args{}.Build("")
	if err != nil {
		t.Fatalf("build default connection config: %v", err)
	}
	if strings.Join(got.Endpoints, ",") != strings.Join(want.Endpoints, ",") {
		t.Fatalf("endpoints = %v, want the default %v", got.Endpoints, want.Endpoints)
	}

	registry := factory("test-product", "token-a", "127.0.0.1:19000")
	if registry == nil {
		t.Fatal("the factory returned no registry")
	}
}

// TestProxyOnlineRegistrationBadConfigFileIsLazy pins that a broken
// --etcd-config file does not fail at startup: the registration option is
// built without reading it, and only the background registration reports the
// error. The deferred error is checked here on the connection selection, and
// its text carries no credentials.
func TestProxyOnlineRegistrationBadConfigFileIsLazy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etcd.toml")
	const secret = "s3cr3t-password"
	if err := os.WriteFile(path, []byte("endpoints = [\"http://127.0.0.1:2379\"]\npassword = \""+secret+"\"\nunknown_field = \"x\"\n"), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg := parseProxyUsage(t, "--etcd-config="+path)
	// The option set is built without touching the file, so startup succeeds.
	factory := onlineRegistryFactory(cfg.Etcd, true)
	if factory == nil {
		t.Fatal("the broken file must not remove the online registration option")
	}
	if registry := factory("test-product", "token-a", "127.0.0.1:19000"); registry == nil {
		t.Fatal("building the registry must not depend on the file")
	}

	// The failure is deferred to the first connection attempt, which happens
	// inside the registration loop in the background.
	if _, err := cfg.Etcd.Build(""); err == nil {
		t.Fatal("the broken config file must fail when the connection is resolved")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("deferred error %q leaks the password", err)
	}
}

// TestProxyUsageDocumentsOnlineRegistration pins the help text: the online
// registration needs connection options, may be configured with --etcd-config
// alone, and runs in the background without delaying going online.
func TestProxyUsageDocumentsOnlineRegistration(t *testing.T) {
	for _, want := range []string{
		"etcd_register_online", "--etcd-config", "background", "never delays going online",
	} {
		if !strings.Contains(proxyUsage, want) {
			t.Fatalf("usage does not mention %q:\n%s", want, proxyUsage)
		}
	}
}

// TestProxyDefaultConfigFileLoads pins that the packaged config/proxy.toml is
// byte-identical to what --default-config prints (the Makefile default-configs
// target regenerates it that way) and that the current binary can load and
// validate it. LoadFromFile also rejects every jodis_* key, so the load proves
// the shipped file carries none.
func TestProxyDefaultConfigFileLoads(t *testing.T) {
	const path = "../../config/proxy.toml"
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(blob) != proxy.DefaultConfig {
		t.Fatalf("%s is stale: regenerate it with 'codis-proxy --default-config'", path)
	}

	c := proxy.NewDefaultConfig()
	if err := c.LoadFromFile(path); err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("validate %s: %v", path, err)
	}
	if c.EtcdRegisterOnline {
		t.Fatal("etcd_register_online = true in the default file, want false")
	}
	if c.ClusterNodesCompat != proxy.ClusterNodesCompatDisabled {
		t.Fatalf("cluster_nodes_compat = %q, want %q", c.ClusterNodesCompat, proxy.ClusterNodesCompatDisabled)
	}
}
