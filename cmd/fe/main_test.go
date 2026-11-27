// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
	"github.com/CodisLabs/codis/pkg/models"
	etcdclient "github.com/CodisLabs/codis/pkg/models/etcd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	case "--listen":
		return cfg.Listen
	default:
		panic("unknown fe flag " + flag)
	}
}

var feStringFlags = []string{
	"--ncpu", "--log", "--log-level", "--assets-dir", "--pidfile",
	"--dashboard-list", "--listen",
}

// feMustEtcd pins the shared connection options selected by the parse.
func feMustEtcd(t *testing.T, cfg *feConfig, endpoints, configFile string) {
	t.Helper()
	if cfg.Etcd.HasEndpoints != (endpoints != "") || cfg.Etcd.EndpointsFlag != endpoints {
		t.Fatalf("--etcd = %q (set %v), want %q", cfg.Etcd.EndpointsFlag, cfg.Etcd.HasEndpoints, endpoints)
	}
	if cfg.Etcd.HasConfigFile != (configFile != "") || cfg.Etcd.ConfigFile != configFile {
		t.Fatalf("--etcd-config = %q (set %v), want %q", cfg.Etcd.ConfigFile, cfg.Etcd.HasConfigFile, configFile)
	}
}

// TestFEParserRequiresListenAndSource pins the usage grammar: both a dashboard
// source and --listen are mandatory in the run form.
func TestFEParserRequiresListenAndSource(t *testing.T) {
	tests := [][]string{
		{},
		{"--listen=:8080"},
		{"--etcd=http://127.0.0.1:2379"},
		{"--etcd-config=/tmp/etcd.toml"},
		{"--dashboard-list=/tmp/list.json"},
		{"--etcd=http://127.0.0.1:2379", "--etcd-config=/tmp/etcd.toml"},
	}
	for _, argv := range tests {
		if err := parseFEUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestFESourceChoices covers the static dashboard list and the dynamic etcd
// source, including --etcd with --etcd-config being one single source.
func TestFESourceChoices(t *testing.T) {
	tests := []struct {
		name       string
		argv       []string
		endpoints  string
		configFile string
	}{
		{"dashboard-list", []string{"--dashboard-list=/tmp/list.json"}, "", ""},
		{"etcd", []string{"--etcd=http://127.0.0.1:2379"}, "http://127.0.0.1:2379", ""},
		{"etcd list", []string{"--etcd=http://a:2379,http://b:2379"}, "http://a:2379,http://b:2379", ""},
		{"etcd config", []string{"--etcd-config=/etc/codis/etcd.toml"}, "", "/etc/codis/etcd.toml"},
		{"etcd with config", []string{"--etcd=http://127.0.0.1:2379", "--etcd-config=/etc/codis/etcd.toml"}, "http://127.0.0.1:2379", "/etc/codis/etcd.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := append([]string{"--listen=:8080"}, tt.argv...)
			cfg := parseFEUsage(t, argv...)
			feMustString(t, cfg.Listen, "--listen", ":8080")
			feMustEtcd(t, cfg, tt.endpoints, tt.configFile)
			if wantStatic := tt.endpoints == "" && tt.configFile == ""; cfg.DashboardList.Provided != wantStatic {
				t.Fatalf("--dashboard-list provided = %v, want %v", cfg.DashboardList.Provided, wantStatic)
			}
		})
	}
}

// TestFESourceChoicesRejectStaticWithDynamic pins that the static list and the
// etcd source are mutually exclusive, whether the etcd side is spelled with
// --etcd, --etcd-config or both.
func TestFESourceChoicesRejectStaticWithDynamic(t *testing.T) {
	tests := [][]string{
		{"--dashboard-list=/tmp/list.json", "--etcd=http://127.0.0.1:2379"},
		{"--dashboard-list=/tmp/list.json", "--etcd-config=/etc/codis/etcd.toml"},
		{"--dashboard-list=/tmp/list.json", "--etcd=http://127.0.0.1:2379", "--etcd-config=/etc/codis/etcd.toml"},
	}
	for _, argv := range tests {
		argv = append([]string{"--listen=:8080"}, argv...)
		err := parseFEUsageError(t, argv...)
		if err == nil {
			t.Fatalf("argv %v: expected parse error for conflicting sources", argv)
		}
		if !strings.Contains(err.Error(), "--dashboard-list") || !strings.Contains(err.Error(), "--etcd") {
			t.Fatalf("argv %v: error %q does not name both sources", argv, err)
		}
	}
}

// TestFELegacyCoordinatorFlagsAreRejected pins that the removed coordinator
// options fail with the flag name and the last supporting commit, and that
// their values never reach the error text.
func TestFELegacyCoordinatorFlagsAreRejected(t *testing.T) {
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
			argv := append([]string{"--listen=:8080"}, tt.argv...)
			err := parseFEUsageError(t, argv...)
			if err == nil {
				t.Fatalf("argv %v: expected parse error", argv)
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

func TestFEVersionFlagIsBool(t *testing.T) {
	cfg := parseFEUsage(t, "--version")
	feMustBool(t, cfg.Version, "--version", true)
	for _, flag := range feStringFlags {
		feMustUnset(t, feStringArg(cfg, flag), flag)
	}
	feMustEtcd(t, cfg, "", "")

	cfg = parseFEUsage(t, "--listen=:8080", "--etcd=http://127.0.0.1:2379")
	feMustBool(t, cfg.Version, "--version", false)
}

func TestFEVersionIsExclusiveWithRunFlags(t *testing.T) {
	tests := [][]string{
		{"--version", "--listen=:8080"},
		{"--version", "--etcd=http://127.0.0.1:2379"},
		{"--version", "--dashboard-list=/tmp/list.json", "--listen=:8080"},
	}
	for _, argv := range tests {
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
	feMustEtcd(t, cfg, "", "")
}

// TestFEStringFlagsAbsentExceptRequired covers the optional value flags. The
// required --listen and the required source are verified separately because an
// absent value is a parse error, not a silently empty value.
func TestFEStringFlagsAbsentExceptRequired(t *testing.T) {
	cfg := parseFEUsage(t, "--listen=:8080", "--etcd=http://127.0.0.1:2379")
	for _, flag := range []string{"--ncpu", "--log", "--log-level", "--assets-dir", "--pidfile", "--dashboard-list"} {
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

// TestFEUsageHasNoLegacyCoordinator pins the help text: the removed backends
// are gone and the mutual exclusion of the two remaining sources is stated.
func TestFEUsageHasNoLegacyCoordinator(t *testing.T) {
	for _, legacy := range []string{"zookeeper", "filesystem", "consul"} {
		if strings.Contains(feUsage, legacy) {
			t.Fatalf("usage still mentions %q:\n%s", legacy, feUsage)
		}
	}
	for _, want := range []string{"--dashboard-list", "--etcd-config", "Mutually exclusive"} {
		if !strings.Contains(feUsage, want) {
			t.Fatalf("usage does not mention %q:\n%s", want, feUsage)
		}
	}
}

// feScriptedLoader counts Reload calls and fails the first `failures` of them
// (-1 fails forever) before returning routes.
type feScriptedLoader struct {
	mu       sync.Mutex
	calls    int
	failures int
	routes   map[string]string
	err      error
}

func (l *feScriptedLoader) Reload() (map[string]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.failures != 0 {
		if l.failures > 0 {
			l.failures--
		}
		return nil, l.err
	}
	return l.routes, nil
}

func (l *feScriptedLoader) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func feShrinkFirstRefresh(t *testing.T, attempts int, interval time.Duration) {
	t.Helper()
	oldAttempts, oldInterval := firstRefreshAttempts, firstRefreshInterval
	firstRefreshAttempts, firstRefreshInterval = attempts, interval
	t.Cleanup(func() { firstRefreshAttempts, firstRefreshInterval = oldAttempts, oldInterval })
}

// TestFERefreshDashboardsSucceeds pins the happy path of the startup refresh.
func TestFERefreshDashboardsSucceeds(t *testing.T) {
	loader := &feScriptedLoader{routes: map[string]string{"alpha": "127.0.0.1:18080"}}
	if err := refreshDashboards(loader); err != nil {
		t.Fatalf("refreshDashboards() failed: %v", err)
	}
	if n := loader.count(); n != 1 {
		t.Fatalf("reload calls = %d, want 1", n)
	}
}

// TestFERefreshDashboardsReportsRejectionWithoutRetry pins that a refused
// first read fails with the reason instead of retrying until a timeout.
func TestFERefreshDashboardsReportsRejectionWithoutRetry(t *testing.T) {
	feShrinkFirstRefresh(t, 3, time.Millisecond)
	loader := &feScriptedLoader{
		failures: -1,
		err: &etcdclient.OpError{
			Op: "list", Key: "/codis3", Err: errors.New("etcdserver: permission denied"), Kind: etcdclient.KindRejected,
		},
	}
	err := refreshDashboards(loader)
	if err == nil {
		t.Fatal("refreshDashboards() succeeded, want a rejection error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error %q does not carry the reason", err)
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error %q does not say the request was refused", err)
	}
	if n := loader.count(); n != 1 {
		t.Fatalf("reload calls = %d, want 1 (no retry for a rejection)", n)
	}
}

// TestFERefreshDashboardsRetriesConnectivityErrors pins the bounded retry of
// connection and unknown errors.
func TestFERefreshDashboardsRetriesConnectivityErrors(t *testing.T) {
	feShrinkFirstRefresh(t, 3, time.Millisecond)
	loader := &feScriptedLoader{
		failures: -1,
		err:      errors.New("etcd list /codis3: context deadline exceeded"),
	}
	err := refreshDashboards(loader)
	if err == nil {
		t.Fatal("refreshDashboards() succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("error %q does not carry the reason", err)
	}
	if n := loader.count(); n != 3 {
		t.Fatalf("reload calls = %d, want 3", n)
	}
}

// TestFERefreshDashboardsStopsRetryingOnSuccess pins that a later successful
// attempt ends the preflight instead of exhausting the attempts.
func TestFERefreshDashboardsStopsRetryingOnSuccess(t *testing.T) {
	feShrinkFirstRefresh(t, 5, time.Millisecond)
	loader := &feScriptedLoader{
		failures: 2,
		err:      errors.New("etcd list /codis3: connection refused"),
		routes:   map[string]string{"alpha": "127.0.0.1:18080"},
	}
	if err := refreshDashboards(loader); err != nil {
		t.Fatalf("refreshDashboards() failed: %v", err)
	}
	if n := loader.count(); n != 3 {
		t.Fatalf("reload calls = %d, want 3 (two failures then success)", n)
	}
}

// TestFEIsRejectedCoversEtcdErrors pins the refusal predicate for both the
// driver's OpError and a raw gRPC status error.
func TestFEIsRejectedCoversEtcdErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"permission denied", &etcdclient.OpError{
			Op: "list", Key: "/codis3", Err: errors.New("etcdserver: permission denied"), Kind: etcdclient.KindRejected,
		}, true},
		{"deadline", &etcdclient.OpError{
			Op: "list", Key: "/codis3", Err: errors.New("context deadline exceeded"), Kind: etcdclient.KindUnknown,
		}, false},
		{"wrapped permission denied", fmt.Errorf("trace: %w", &etcdclient.OpError{
			Op: "read", Key: "/codis3/a/topom", Err: errors.New("etcdserver: permission denied"), Kind: etcdclient.KindRejected,
		}), true},
		{"grpc permission denied", status.Error(codes.PermissionDenied, "etcdserver: permission denied"), true},
		{"grpc unauthenticated", status.Error(codes.Unauthenticated, "auth"), true},
		{"grpc unavailable", status.Error(codes.Unavailable, "connection refused"), false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		if got := isRejected(tt.err); got != tt.want {
			t.Fatalf("%s: isRejected() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// feEtcdStub is a minimal models.Client serving a product list and per-product
// topom answers; the remaining methods are never called.
type feEtcdStub struct {
	models.Client
	products []string
	failing  map[string]bool
	admins   map[string]string
}

func (c *feEtcdStub) List(path string, must bool) ([]string, error) {
	paths := make([]string, 0, len(c.products))
	for _, product := range c.products {
		paths = append(paths, models.CodisDir+"/"+product)
	}
	return paths, nil
}

func (c *feEtcdStub) Read(path string, must bool) ([]byte, error) {
	for _, product := range c.products {
		if path != models.LockPath(product) {
			continue
		}
		if c.failing[product] {
			return nil, &etcdclient.OpError{
				Op: "read", Key: path, Err: errors.New("etcdserver: permission denied"), Kind: etcdclient.KindRejected,
			}
		}
		return []byte(`{"admin_addr":"` + c.admins[product] + `"}`), nil
	}
	return nil, nil
}

// TestFEDynamicLoaderKeepsLastAddressOnReadFailure pins that a failed topom
// read keeps the previous dashboard address instead of shrinking the list.
func TestFEDynamicLoaderKeepsLastAddressOnReadFailure(t *testing.T) {
	client := &feEtcdStub{
		products: []string{"alpha", "beta"},
		failing:  map[string]bool{},
		admins:   map[string]string{"alpha": "127.0.0.1:18080", "beta": "127.0.0.1:18081"},
	}
	loader := &DynamicLoader{client: client}

	routes, err := loader.Reload()
	if err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}
	if len(routes) != 2 || routes["alpha"] != "127.0.0.1:18080" || routes["beta"] != "127.0.0.1:18081" {
		t.Fatalf("routes = %v", routes)
	}

	client.failing["alpha"] = true
	routes, err = loader.Reload()
	if err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}
	if len(routes) != 2 || routes["alpha"] != "127.0.0.1:18080" {
		t.Fatalf("routes after a failed read = %v, want the previous alpha address", routes)
	}

	// A product that disappears from the listing must drop out even though an
	// address was remembered for it.
	client.products = []string{"alpha"}
	routes, err = loader.Reload()
	if err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}
	if _, ok := routes["beta"]; ok || routes["alpha"] != "127.0.0.1:18080" {
		t.Fatalf("routes after a product was removed = %v", routes)
	}
}

// TestFEReverseProxyKeepsLastSnapshotOnReloadFailure pins that a failed
// periodic refresh keeps the last successful dashboard list instead of
// answering an empty one.
func TestFEReverseProxyKeepsLastSnapshotOnReloadFailure(t *testing.T) {
	loader := &feScriptedLoader{routes: map[string]string{"alpha": "127.0.0.1:18080"}}
	r := NewReverseProxy(loader)

	names := r.GetNames()
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("names = %v, want [alpha]", names)
	}

	loader.mu.Lock()
	loader.failures, loader.err = -1, errors.New("etcd list /codis3: context deadline exceeded")
	loader.mu.Unlock()
	r.Lock()
	r.loadAt = time.Time{}
	r.Unlock()

	names = r.GetNames()
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("names after a failed reload = %v, want the previous [alpha]", names)
	}
	if p := r.GetProxy("alpha"); p == nil {
		t.Fatal("alpha route disappeared after a failed reload")
	}
}
