// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"strings"
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
)

// parseAdminUsage drives the typed parser against the package-level
// adminUsage contract, mirroring how main() parses argv.
func parseAdminUsage(t *testing.T, argv ...string) *adminConfig {
	t.Helper()
	cfg, help, err := parseAdminArgs(append([]string{}, argv...))
	if err != nil {
		t.Fatalf("parseAdminArgs(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("parseAdminArgs(%v) unexpectedly asked for help", argv)
	}
	return cfg
}

// parseAdminUsageError parses an argv that is expected to be rejected.
func parseAdminUsageError(t *testing.T, argv ...string) error {
	t.Helper()
	_, help, err := parseAdminArgs(append([]string{}, argv...))
	if help {
		t.Fatalf("parseAdminArgs(%v) unexpectedly asked for help", argv)
	}
	return err
}

func adminMustBool(t *testing.T, got cli.Bool, flag string, want bool) {
	t.Helper()
	if got.Value != want {
		t.Fatalf("%s = %v, want %v", flag, got.Value, want)
	}
	if got.Provided != want {
		t.Fatalf("%s: provided = %v, want %v", flag, got.Provided, want)
	}
}

func adminMustString(t *testing.T, got cli.String, flag string, want string) {
	t.Helper()
	if !got.Provided {
		t.Fatalf("%s: want string(%q), got not provided", flag, want)
	}
	if got.Value != want {
		t.Fatalf("%s = %q, want %q", flag, got.Value, want)
	}
}

func adminMustUnset(t *testing.T, got cli.String, flag string) {
	t.Helper()
	if got.Provided {
		t.Fatalf("%s = %#v, want not provided", flag, got.Value)
	}
}

func adminStringArg(cfg *adminConfig, flag string) cli.String {
	switch flag {
	case "--proxy":
		return cfg.Proxy
	case "--dashboard":
		return cfg.Dashboard
	case "--auth":
		return cfg.Auth
	case "--addr":
		return cfg.Addr
	case "--token":
		return cfg.Token
	case "--gid":
		return cfg.Gid
	case "--pid":
		return cfg.Pid
	case "--product":
		return cfg.Product
	case "--datacenter":
		return cfg.Datacenter
	case "--log-level":
		return cfg.LogLevel
	case "--fillslots":
		return cfg.FillSlots
	case "--beg":
		return cfg.Beg
	case "--end":
		return cfg.End
	case "--sid":
		return cfg.Sid
	case "--gid-from":
		return cfg.GidFrom
	case "--gid-to":
		return cfg.GidTo
	case "--num-slots":
		return cfg.NumSlots
	case "--interval":
		return cfg.Interval
	case "--disabled":
		return cfg.Disabled
	case "--acl-set":
		return cfg.ACLSet
	case "--server":
		return cfg.Server
	case "--topn":
		return cfg.TopN
	case "--prefix-sep":
		return cfg.PrefixSep
	case "--max-depth":
		return cfg.MaxDepth
	case "--regex":
		return cfg.Regex
	case "--truncate-ts":
		return cfg.TruncateTS
	case "--job":
		return cfg.Job
	case "--config-convert":
		return cfg.ConfigConvert
	case "--config-restore":
		return cfg.ConfigRestore
	default:
		panic("unknown admin string flag " + flag)
	}
}

// adminMustEtcd pins the shared connection options of the commands that read
// the metadata directly.
func adminMustEtcd(t *testing.T, cfg *adminConfig, value string) {
	t.Helper()
	if !cfg.Coordinator.Etcd.Provided {
		t.Fatalf("--etcd not provided, want %q", value)
	}
	if got := cfg.Coordinator.Etcd.Value; got != value {
		t.Fatalf("--etcd = %q, want %q", got, value)
	}
	if cfg.Coordinator.EtcdConfig.Provided {
		t.Fatal("--etcd-config reported as provided")
	}
}

func adminMustEtcdConfig(t *testing.T, cfg *adminConfig, value string) {
	t.Helper()
	if !cfg.Coordinator.EtcdConfig.Provided {
		t.Fatalf("--etcd-config not provided, want %q", value)
	}
	if got := cfg.Coordinator.EtcdConfig.Value; got != value {
		t.Fatalf("--etcd-config = %q, want %q", got, value)
	}
	if cfg.Coordinator.Etcd.Provided {
		t.Fatal("--etcd reported as provided")
	}
}

func adminBoolArg(cfg *adminConfig, flag string) cli.Bool {
	// Positional overview selectors are represented by cfg.Command.
	switch flag {
	case "config", "model", "stats", "slots", "group", "proxy":
		v := cfg.Command == flag
		return cli.Bool{Value: v, Provided: v}
	}
	switch flag {
	case "-v":
		return cfg.Verbose
	case "--start":
		return cfg.Start
	case "--shutdown":
		return cfg.Shutdown
	case "--reload":
		return cfg.Reload
	case "--locked":
		return cfg.Locked
	case "--reset-stats":
		return cfg.ResetStats
	case "--forcegc":
		return cfg.ForceGC
	case "--show-lock":
		return cfg.ShowLock
	case "--remove-lock":
		return cfg.RemoveLock
	case "--config-dump":
		return cfg.ConfigDump
	case "--dashboard-list":
		return cfg.DashboardList
	case "--slots-assign":
		return cfg.SlotsAssign
	case "--slots-status":
		return cfg.SlotsStatus
	case "--offline":
		return cfg.Offline
	case "--confirm":
		return cfg.Confirm
	case "--list-proxy":
		return cfg.ListProxy
	case "--create-proxy":
		return cfg.CreateProxy
	case "--online-proxy":
		return cfg.OnlineProxy
	case "--remove-proxy":
		return cfg.RemoveProxy
	case "--reinit-proxy":
		return cfg.ReinitProxy
	case "--proxy-status":
		return cfg.ProxyStatus
	case "--force":
		return cfg.Force
	case "--all":
		return cfg.All
	case "--list-group":
		return cfg.ListGroup
	case "--create-group":
		return cfg.CreateGroup
	case "--remove-group":
		return cfg.RemoveGroup
	case "--resync-group":
		return cfg.ResyncGroup
	case "--group-add":
		return cfg.GroupAdd
	case "--group-del":
		return cfg.GroupDel
	case "--group-status":
		return cfg.GroupStatus
	case "--replica-groups":
		return cfg.ReplicaGroups
	case "--enable":
		return cfg.Enable
	case "--disable":
		return cfg.Disable
	case "--promote-server":
		return cfg.PromoteServer
	case "--sync-action":
		return cfg.SyncAction
	case "--create":
		return cfg.Create
	case "--remove":
		return cfg.Remove
	case "--slot-action":
		return cfg.SlotAction
	case "--create-some":
		return cfg.CreateSome
	case "--create-range":
		return cfg.CreateRange
	case "--rebalance":
		return cfg.Rebalance
	case "--acl":
		return cfg.ACL
	case "--acl-sync":
		return cfg.ACLSync
	case "--rdb-analysis-remote-fetch":
		return cfg.RDBAnalysisRemoteFetch
	case "--include-expired":
		return cfg.IncludeExpired
	case "--pitr-create":
		return cfg.PitrCreate
	case "--pitr-list":
		return cfg.PitrList
	case "--pitr-get":
		return cfg.PitrGet
	case "--pitr-cancel":
		return cfg.PitrCancel
	case "--pitr-remove":
		return cfg.PitrRemove
	case "--sentinel-add":
		return cfg.SentinelAdd
	case "--sentinel-del":
		return cfg.SentinelDel
	case "--sentinel-resync":
		return cfg.SentinelResync
	default:
		panic("unknown admin bool flag " + flag)
	}
}

// adminBoolFlags are the boolean options/commands that main() and the cmd*
// dispatchers select. Boolean options default to false and record whether
// they were explicitly provided.
var adminBoolFlags = []string{
	"-v", "--start", "--shutdown", "--offline", "--locked", "--all", "--confirm",
	"--force", "--remove-proxy", "--reinit-proxy", "--enable", "--disable",
	"--include-expired", "--show-lock", "--remove-lock", "--config-dump", "--dashboard-list",
	"--slots-assign", "--slots-status", "--list-proxy", "--create-proxy",
	"--online-proxy", "--proxy-status", "--list-group", "--create-group",
	"--remove-group", "--resync-group", "--group-add", "--group-del",
	"--group-status", "--replica-groups", "--promote-server", "--sync-action",
	"--slot-action", "--rebalance", "--acl", "--acl-sync",
	"--rdb-analysis-remote-fetch", "--pitr-create", "--pitr-list", "--pitr-get",
	"--pitr-cancel", "--pitr-remove", "--sentinel-add", "--sentinel-del",
	"--sentinel-resync", "--reload", "--reset-stats", "--forcegc", "--create",
	"--remove", "config", "model", "stats", "slots", "group", "proxy",
}

// TestAdminProxyAndDashboardAreMutuallyExclusive pins the two top-level modes.
func TestAdminProxyAndDashboardAreMutuallyExclusive(t *testing.T) {
	tests := [][]string{
		{"--proxy=127.0.0.1:11080", "--dashboard=127.0.0.1:18080"},
		{"--proxy=127.0.0.1:11080", "--start", "--shutdown"},
		{"--proxy=127.0.0.1:11080", "--start", "--forcegc"},
		{"--proxy=127.0.0.1:11080", "model", "stats"},
		{"--dashboard=127.0.0.1:18080", "--shutdown", "--reload"},
		{"--dashboard=127.0.0.1:18080", "--create-group", "--remove-group", "--gid=1"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestAdminProxyOverviewCommands covers the [config|model|stats|slots]
// positional selector of the proxy mode.
func TestAdminProxyOverviewCommands(t *testing.T) {
	cfg := parseAdminUsage(t, "--proxy=127.0.0.1:11080")
	adminMustString(t, adminStringArg(cfg, "--proxy"), "--proxy", "127.0.0.1:11080")
	adminMustUnset(t, adminStringArg(cfg, "--dashboard"), "--dashboard")
	adminMustUnset(t, adminStringArg(cfg, "--auth"), "--auth")
	for _, cmd := range []string{"config", "model", "stats", "slots"} {
		adminMustBool(t, adminBoolArg(cfg, cmd), cmd, false)
	}

	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "model")
	adminMustBool(t, adminBoolArg(cfg, "model"), "model", true)
	for _, cmd := range []string{"config", "stats", "slots"} {
		adminMustBool(t, adminBoolArg(cfg, cmd), cmd, false)
	}
}

func TestAdminProxyCommandFlags(t *testing.T) {
	cfg := parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--start")
	adminMustBool(t, adminBoolArg(cfg, "--start"), "--start", true)
	adminMustBool(t, adminBoolArg(cfg, "--shutdown"), "--shutdown", false)

	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--shutdown")
	adminMustBool(t, adminBoolArg(cfg, "--shutdown"), "--shutdown", true)
	adminMustBool(t, adminBoolArg(cfg, "--start"), "--start", false)

	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--log-level=DEBUG")
	adminMustString(t, adminStringArg(cfg, "--log-level"), "--log-level", "DEBUG")

	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--fillslots=/tmp/slots.json")
	adminMustString(t, adminStringArg(cfg, "--fillslots"), "--fillslots", "/tmp/slots.json")
	adminMustBool(t, adminBoolArg(cfg, "--locked"), "--locked", false)
	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--fillslots=/tmp/slots.json", "--locked")
	adminMustBool(t, adminBoolArg(cfg, "--locked"), "--locked", true)

	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--reset-stats")
	adminMustBool(t, adminBoolArg(cfg, "--reset-stats"), "--reset-stats", true)

	cfg = parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--forcegc")
	adminMustBool(t, adminBoolArg(cfg, "--forcegc"), "--forcegc", true)
}

func TestAdminProxyAuthFlagBinds(t *testing.T) {
	cfg := parseAdminUsage(t, "--proxy=127.0.0.1:11080", "--auth=secret")
	adminMustString(t, adminStringArg(cfg, "--auth"), "--auth", "secret")
	adminMustUnset(t, adminStringArg(cfg, "--dashboard"), "--dashboard")
	adminMustUnset(t, adminStringArg(cfg, "--product"), "--product")
}

// TestAdminDashboardOverviewAndDispatchKeys pins the fields main()/cmdDashboard
// use to choose a handler. In particular the positional `proxy` command must
// not collide with the --proxy flag, and the admin-only flags must default to
// false so the dispatch cannot select them accidentally.
func TestAdminDashboardOverviewAndDispatchKeys(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080")
	adminMustString(t, adminStringArg(cfg, "--dashboard"), "--dashboard", "127.0.0.1:18080")
	adminMustUnset(t, adminStringArg(cfg, "--proxy"), "--proxy")
	adminMustUnset(t, adminStringArg(cfg, "--config-convert"), "--config-convert")
	adminMustUnset(t, adminStringArg(cfg, "--config-restore"), "--config-restore")
	for _, flag := range adminBoolFlags {
		adminMustBool(t, adminBoolArg(cfg, flag), flag, false)
	}

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "proxy")
	adminMustBool(t, adminBoolArg(cfg, "proxy"), "proxy", true)
	adminMustUnset(t, adminStringArg(cfg, "--proxy"), "--proxy")
	adminMustUnset(t, adminStringArg(cfg, "--config-convert"), "--config-convert")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "group")
	adminMustBool(t, adminBoolArg(cfg, "group"), "group", true)
	adminMustUnset(t, adminStringArg(cfg, "--proxy"), "--proxy")

	cfg = parseAdminUsage(t, "-v", "--dashboard=127.0.0.1:18080", "--log-level=WARN")
	adminMustBool(t, adminBoolArg(cfg, "-v"), "-v", true)
	adminMustString(t, adminStringArg(cfg, "--log-level"), "--log-level", "WARN")
}

func TestAdminSlotsAssignRequiredArgs(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080",
		"--slots-assign", "--beg=0", "--end=1023", "--gid=1")
	adminMustBool(t, adminBoolArg(cfg, "--slots-assign"), "--slots-assign", true)
	adminMustString(t, adminStringArg(cfg, "--beg"), "--beg", "0")
	adminMustString(t, adminStringArg(cfg, "--end"), "--end", "1023")
	adminMustString(t, adminStringArg(cfg, "--gid"), "--gid", "1")
	adminMustBool(t, adminBoolArg(cfg, "--offline"), "--offline", false)
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", false)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080",
		"--slots-assign", "--beg=0", "--end=1023", "--offline", "--confirm")
	adminMustBool(t, adminBoolArg(cfg, "--offline"), "--offline", true)
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", true)
	adminMustUnset(t, adminStringArg(cfg, "--gid"), "--gid")

	tests := [][]string{
		{"--dashboard=d", "--slots-assign", "--beg=0", "--end=1023"},
		{"--dashboard=d", "--slots-assign", "--beg=0", "--end=1023", "--gid=1", "--offline"},
		{"--dashboard=d", "--slots-assign", "--end=1023", "--gid=1"},
		{"--dashboard=d", "--slots-assign", "--beg=0", "--gid=1"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

func TestAdminProxyAdminRequiredSelectors(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--remove-proxy", "--token=t")
	adminMustBool(t, adminBoolArg(cfg, "--remove-proxy"), "--remove-proxy", true)
	adminMustString(t, adminStringArg(cfg, "--token"), "--token", "t")
	adminMustBool(t, adminBoolArg(cfg, "--force"), "--force", false)
	adminMustUnset(t, adminStringArg(cfg, "--addr"), "--addr")
	adminMustUnset(t, adminStringArg(cfg, "--pid"), "--pid")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--remove-proxy", "--pid=7", "--force")
	adminMustString(t, adminStringArg(cfg, "--pid"), "--pid", "7")
	adminMustBool(t, adminBoolArg(cfg, "--force"), "--force", true)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--reinit-proxy", "--all")
	adminMustBool(t, adminBoolArg(cfg, "--reinit-proxy"), "--reinit-proxy", true)
	adminMustBool(t, adminBoolArg(cfg, "--all"), "--all", true)

	tests := [][]string{
		{"--dashboard=d", "--remove-proxy"},
		{"--dashboard=d", "--remove-proxy", "--addr=a", "--token=t"},
		{"--dashboard=d", "--reinit-proxy"},
		{"--dashboard=d", "--reinit-proxy", "--all", "--token=t"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

func TestAdminGroupCommands(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--create-group", "--gid=1")
	adminMustBool(t, adminBoolArg(cfg, "--create-group"), "--create-group", true)
	adminMustString(t, adminStringArg(cfg, "--gid"), "--gid", "1")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--group-add", "--gid=1", "--addr=127.0.0.1:6379")
	adminMustBool(t, adminBoolArg(cfg, "--group-add"), "--group-add", true)
	adminMustString(t, adminStringArg(cfg, "--addr"), "--addr", "127.0.0.1:6379")
	adminMustUnset(t, adminStringArg(cfg, "--datacenter"), "--datacenter")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--group-add", "--gid=1", "--addr=127.0.0.1:6379", "--datacenter=dc1")
	adminMustString(t, adminStringArg(cfg, "--datacenter"), "--datacenter", "dc1")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--replica-groups", "--gid=1", "--addr=127.0.0.1:6379", "--enable")
	adminMustBool(t, adminBoolArg(cfg, "--replica-groups"), "--replica-groups", true)
	adminMustBool(t, adminBoolArg(cfg, "--enable"), "--enable", true)
	adminMustBool(t, adminBoolArg(cfg, "--disable"), "--disable", false)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--resync-group")
	adminMustBool(t, adminBoolArg(cfg, "--resync-group"), "--resync-group", true)
	adminMustUnset(t, adminStringArg(cfg, "--gid"), "--gid")
	adminMustBool(t, adminBoolArg(cfg, "--all"), "--all", false)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--resync-group", "--all")
	adminMustBool(t, adminBoolArg(cfg, "--all"), "--all", true)

	tests := [][]string{
		{"--dashboard=d", "--create-group"},
		{"--dashboard=d", "--group-add", "--gid=1"},
		{"--dashboard=d", "--group-del", "--gid=1"},
		{"--dashboard=d", "--replica-groups", "--gid=1", "--addr=a"},
		{"--dashboard=d", "--replica-groups", "--gid=1", "--addr=a", "--enable", "--disable"},
		{"--dashboard=d", "--resync-group", "--gid=1", "--all"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

func TestAdminSlotAndSyncActionCommands(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--sync-action", "--create", "--addr=127.0.0.1:6379")
	if cfg.Command != "sync-action" {
		t.Fatalf("sync-action command = %q", cfg.Command)
	}
	adminMustBool(t, adminBoolArg(cfg, "--sync-action"), "--sync-action", true)
	adminMustBool(t, adminBoolArg(cfg, "--create"), "--create", true)
	adminMustBool(t, adminBoolArg(cfg, "--remove"), "--remove", false)
	adminMustString(t, adminStringArg(cfg, "--addr"), "--addr", "127.0.0.1:6379")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--slot-action", "--create", "--sid=1", "--gid=2")
	adminMustBool(t, adminBoolArg(cfg, "--slot-action"), "--slot-action", true)
	adminMustBool(t, adminBoolArg(cfg, "--create"), "--create", true)
	adminMustString(t, adminStringArg(cfg, "--sid"), "--sid", "1")
	adminMustString(t, adminStringArg(cfg, "--gid"), "--gid", "2")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--slot-action", "--create-some", "--gid-from=1", "--gid-to=2", "--num-slots=3")
	adminMustBool(t, adminBoolArg(cfg, "--create-some"), "--create-some", true)
	adminMustString(t, adminStringArg(cfg, "--gid-from"), "--gid-from", "1")
	adminMustString(t, adminStringArg(cfg, "--gid-to"), "--gid-to", "2")
	adminMustString(t, adminStringArg(cfg, "--num-slots"), "--num-slots", "3")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--slot-action", "--interval=3")
	adminMustString(t, adminStringArg(cfg, "--interval"), "--interval", "3")
	adminMustUnset(t, adminStringArg(cfg, "--disabled"), "--disabled")
	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--slot-action", "--disabled=abc")
	adminMustString(t, adminStringArg(cfg, "--disabled"), "--disabled", "abc")
	adminMustUnset(t, adminStringArg(cfg, "--interval"), "--interval")

	tests := [][]string{
		{"--dashboard=d", "--sync-action", "--create"},
		{"--dashboard=d", "--sync-action", "--create", "--remove", "--addr=a"},
		{"--dashboard=d", "--slot-action", "--create", "--sid=1"},
		{"--dashboard=d", "--slot-action", "--create", "--gid=1"},
		{"--dashboard=d", "--slot-action", "--create-some", "--gid-from=1", "--gid-to=2"},
		{"--dashboard=d", "--slot-action", "--interval=3", "--disabled=abc"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

func TestAdminACLAndRebalanceCommands(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--rebalance")
	adminMustBool(t, adminBoolArg(cfg, "--rebalance"), "--rebalance", true)
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", false)
	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--rebalance", "--confirm")
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", true)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--acl")
	adminMustBool(t, adminBoolArg(cfg, "--acl"), "--acl", true)
	adminMustBool(t, adminBoolArg(cfg, "--acl-sync"), "--acl-sync", false)
	adminMustUnset(t, adminStringArg(cfg, "--acl-set"), "--acl-set")

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--acl-set=/tmp/acl.json", "--confirm")
	adminMustString(t, adminStringArg(cfg, "--acl-set"), "--acl-set", "/tmp/acl.json")
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", true)
	adminMustBool(t, adminBoolArg(cfg, "--acl"), "--acl", false)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--acl-sync")
	adminMustBool(t, adminBoolArg(cfg, "--acl-sync"), "--acl-sync", true)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--sentinel-add", "--addr=127.0.0.1:26379")
	adminMustBool(t, adminBoolArg(cfg, "--sentinel-add"), "--sentinel-add", true)
	adminMustString(t, adminStringArg(cfg, "--addr"), "--addr", "127.0.0.1:26379")
	adminMustBool(t, adminBoolArg(cfg, "--sentinel-del"), "--sentinel-del", false)
}

// TestAdminTailCommands pins the cmdAdmin dispatch keys of the commands that
// access the metadata directly.
func TestAdminTailCommands(t *testing.T) {
	cfg := parseAdminUsage(t, "--show-lock", "--product=test-product", "--etcd=http://127.0.0.1:2379")
	if cfg.Command != "show-lock" {
		t.Fatalf("command = %q, want show-lock", cfg.Command)
	}
	adminMustBool(t, adminBoolArg(cfg, "--show-lock"), "--show-lock", true)
	adminMustString(t, adminStringArg(cfg, "--product"), "--product", "test-product")
	adminMustEtcd(t, cfg, "http://127.0.0.1:2379")
	adminMustBool(t, adminBoolArg(cfg, "--remove-lock"), "--remove-lock", false)
	adminMustUnset(t, adminStringArg(cfg, "--proxy"), "--proxy")
	adminMustUnset(t, adminStringArg(cfg, "--dashboard"), "--dashboard")

	cfg = parseAdminUsage(t, "--remove-lock", "--product=test-product",
		"--expected-revision=417", "--confirm", "--etcd-config=/etc/etcd.toml")
	if cfg.Command != "remove-lock" {
		t.Fatalf("command = %q, want remove-lock", cfg.Command)
	}
	adminMustBool(t, adminBoolArg(cfg, "--remove-lock"), "--remove-lock", true)
	adminMustString(t, adminStringArg(cfg, "--product"), "--product", "test-product")
	adminMustEtcdConfig(t, cfg, "/etc/etcd.toml")
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", true)
	if !cfg.ExpectedRevision.Provided || cfg.ExpectedRevision.Value != 417 {
		t.Fatalf("--expected-revision = %d (provided %v), want 417", cfg.ExpectedRevision.Value, cfg.ExpectedRevision.Provided)
	}
	adminMustBool(t, adminBoolArg(cfg, "--show-lock"), "--show-lock", false)

	cfg = parseAdminUsage(t, "--config-dump", "--product=test-product", "--etcd=http://127.0.0.1:2379")
	if cfg.Command != "config-dump" {
		t.Fatalf("command = %q, want config-dump", cfg.Command)
	}
	adminMustBool(t, adminBoolArg(cfg, "--config-dump"), "--config-dump", true)
	adminMustEtcd(t, cfg, "http://127.0.0.1:2379")

	// --config-convert never touches the backend, so it needs no shared flag.
	cfg = parseAdminUsage(t, "--config-convert=/tmp/config.json")
	if cfg.Command != "config-convert" {
		t.Fatalf("command = %q, want config-convert", cfg.Command)
	}
	adminMustString(t, adminStringArg(cfg, "--config-convert"), "--config-convert", "/tmp/config.json")
	adminMustUnset(t, adminStringArg(cfg, "--config-restore"), "--config-restore")
	adminMustUnset(t, adminStringArg(cfg, "--product"), "--product")
	adminMustUnset(t, adminStringArg(cfg, "--proxy"), "--proxy")
	adminMustUnset(t, adminStringArg(cfg, "--dashboard"), "--dashboard")
	adminMustBool(t, adminBoolArg(cfg, "--config-dump"), "--config-dump", false)

	cfg = parseAdminUsage(t, "--config-restore=/tmp/config.json", "--product=test-product", "--etcd-config=/etc/etcd.toml")
	adminMustString(t, adminStringArg(cfg, "--config-restore"), "--config-restore", "/tmp/config.json")
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", false)
	adminMustEtcdConfig(t, cfg, "/etc/etcd.toml")
	cfg = parseAdminUsage(t, "--config-restore=/tmp/config.json", "--product=test-product",
		"--etcd=http://127.0.0.1:2379", "--confirm")
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", true)

	cfg = parseAdminUsage(t, "--dashboard-list", "--etcd=http://127.0.0.1:2379")
	adminMustBool(t, adminBoolArg(cfg, "--dashboard-list"), "--dashboard-list", true)
	adminMustUnset(t, adminStringArg(cfg, "--product"), "--product")
	adminMustBool(t, adminBoolArg(cfg, "--remove-lock"), "--remove-lock", false)

	tests := [][]string{
		{"--remove-lock", "--product=test-product"},
		{"--remove-lock", "--product=test-product", "--expected-revision=1", "--etcd=http://127.0.0.1:2379"},
		{"--remove-lock", "--product=test-product", "--confirm", "--etcd=http://127.0.0.1:2379"},
		{"--remove-lock", "--product=test-product", "--expected-revision=0", "--confirm", "--etcd=http://127.0.0.1:2379"},
		{"--remove-lock", "--expected-revision=1", "--confirm", "--etcd=http://127.0.0.1:2379"},
		{"--show-lock", "--etcd=http://127.0.0.1:2379"},
		{"--config-dump", "--etcd=http://x:2379"},
		{"--config-convert=/tmp/config.json", "--product=p"},
		{"--config-restore=/tmp/config.json", "--product=p"},
		{"--dashboard-list"},
		{"--dashboard-list", "--product=p", "--etcd=http://127.0.0.1:2379"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestAdminTailRequiresSharedCoordinator pins that the five commands that read
// the metadata need an explicit --etcd or --etcd-config, and that the removed
// backends fail with the last supporting commit without echoing values.
func TestAdminTailRequiresSharedCoordinator(t *testing.T) {
	const wantErr = "at least one of --etcd, --etcd-config is required"
	tests := [][]string{
		{"--show-lock", "--product=p"},
		{"--remove-lock", "--product=p", "--expected-revision=1", "--confirm"},
		{"--config-dump", "--product=p"},
		{"--config-restore=/tmp/config.json", "--product=p"},
		{"--dashboard-list"},
	}
	for _, argv := range tests {
		err := parseAdminUsageError(t, argv...)
		if err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("argv %v: error = %v, want %q", argv, err, wantErr)
		}
	}

	// Both shared options may be given; --etcd wins at connection time.
	cfg := parseAdminUsage(t, "--show-lock", "--product=p",
		"--etcd=http://a:2379", "--etcd-config=/etc/etcd.toml")
	if !cfg.Coordinator.Etcd.Provided || cfg.Coordinator.Etcd.Value != "http://a:2379" {
		t.Fatalf("--etcd = %q (provided %v)", cfg.Coordinator.Etcd.Value, cfg.Coordinator.Etcd.Provided)
	}
	if !cfg.Coordinator.EtcdConfig.Provided || cfg.Coordinator.EtcdConfig.Value != "/etc/etcd.toml" {
		t.Fatalf("--etcd-config = %q (provided %v)", cfg.Coordinator.EtcdConfig.Value, cfg.Coordinator.EtcdConfig.Provided)
	}

	removed := [][]string{
		{"--show-lock", "--product=p", "--zookeeper=zk"},
		{"--show-lock", "--product=p", "--filesystem=/tmp/codis"},
		{"--remove-lock", "--product=p", "--expected-revision=1", "--confirm", "--consul=consul"},
		{"--show-lock", "--product=p", "--zookeeper=zk", "--zookeeper-auth=user:secret-pass"},
		{"--show-lock", "--product=p", "--consul=consul", "--consul-auth=secret-token"},
	}
	for _, argv := range removed {
		err := parseAdminUsageError(t, argv...)
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

func TestAdminPitrRequiredArgs(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080",
		"--pitr-create", "--server=127.0.0.1:6379", "--truncate-ts=1716000000")
	adminMustBool(t, adminBoolArg(cfg, "--pitr-create"), "--pitr-create", true)
	adminMustString(t, adminStringArg(cfg, "--server"), "--server", "127.0.0.1:6379")
	adminMustString(t, adminStringArg(cfg, "--truncate-ts"), "--truncate-ts", "1716000000")
	adminMustBool(t, adminBoolArg(cfg, "--pitr-list"), "--pitr-list", false)

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", "--pitr-list")
	adminMustBool(t, adminBoolArg(cfg, "--pitr-list"), "--pitr-list", true)
	adminMustUnset(t, adminStringArg(cfg, "--job"), "--job")

	for _, cmd := range []string{"--pitr-get", "--pitr-cancel", "--pitr-remove"} {
		cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080", cmd, "--job=job-1")
		adminMustBool(t, adminBoolArg(cfg, cmd), cmd, true)
		adminMustString(t, adminStringArg(cfg, "--job"), "--job", "job-1")
	}

	tests := [][]string{
		{"--dashboard=d", "--pitr-create", "--server=127.0.0.1:6379"},
		{"--dashboard=d", "--pitr-create", "--truncate-ts=1716000000"},
		{"--dashboard=d", "--pitr-get"},
		{"--dashboard=d", "--pitr-cancel"},
		{"--dashboard=d", "--pitr-remove"},
	}
	for _, argv := range tests {
		if err := parseAdminUsageError(t, argv...); err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
	}
}

// TestAdminRDBAnalysisRemoteFetchContract pins the flag layer plus the
// documented bridge into topom.RDBAnalysisOptions, so the migration preserves
// both names and value mapping.
func TestAdminRDBAnalysisRemoteFetchContract(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080",
		"--rdb-analysis-remote-fetch", "--server=127.0.0.1:6379")
	adminMustBool(t, adminBoolArg(cfg, "--rdb-analysis-remote-fetch"), "--rdb-analysis-remote-fetch", true)
	adminMustString(t, adminStringArg(cfg, "--server"), "--server", "127.0.0.1:6379")
	for _, flag := range []string{"--topn", "--prefix-sep", "--max-depth", "--regex"} {
		adminMustUnset(t, adminStringArg(cfg, flag), flag)
	}
	adminMustBool(t, adminBoolArg(cfg, "--include-expired"), "--include-expired", false)

	options := parseRDBAnalysisRemoteFetchOptions(cfg)
	if options.TopN != 0 || options.MaxDepth != 0 || options.Regex != "" ||
		len(options.PrefixSeparators) != 0 || options.IncludeExpired {
		t.Fatalf("options = %+v, want zero value", options)
	}

	cfg = parseAdminUsage(t, "--dashboard=127.0.0.1:18080",
		"--rdb-analysis-remote-fetch", "--server=127.0.0.1:6379",
		"--topn=10", "--prefix-sep=:,|", "--max-depth=4", "--regex=^user:",
		"--include-expired")
	adminMustString(t, adminStringArg(cfg, "--topn"), "--topn", "10")
	adminMustString(t, adminStringArg(cfg, "--max-depth"), "--max-depth", "4")
	adminMustString(t, adminStringArg(cfg, "--regex"), "--regex", "^user:")
	adminMustString(t, adminStringArg(cfg, "--prefix-sep"), "--prefix-sep", ":,|")
	adminMustBool(t, adminBoolArg(cfg, "--include-expired"), "--include-expired", true)

	options = parseRDBAnalysisRemoteFetchOptions(cfg)
	if options.TopN != 10 || options.MaxDepth != 4 || options.Regex != "^user:" || !options.IncludeExpired {
		t.Fatalf("options = %+v, want TopN=10 MaxDepth=4 Regex=^user: IncludeExpired=true", options)
	}
	if len(options.PrefixSeparators) != 2 || options.PrefixSeparators[0] != ":" || options.PrefixSeparators[1] != "|" {
		t.Fatalf("prefix separators = %v, want [:] [|]", options.PrefixSeparators)
	}

	if err := parseAdminUsageError(t, "--dashboard=d", "--rdb-analysis-remote-fetch"); err == nil {
		t.Fatal("expected parse error without --server")
	}
}

// TestAdminRDBIntegerFlagsAreStrings pins that the parser does no numeric
// validation: "abc" parses and fails later in argumentInteger.
func TestAdminRDBIntegerFlagsAreStrings(t *testing.T) {
	cfg := parseAdminUsage(t, "--dashboard=127.0.0.1:18080",
		"--rdb-analysis-remote-fetch", "--server=127.0.0.1:6379", "--topn=abc")
	adminMustString(t, adminStringArg(cfg, "--topn"), "--topn", "abc")
}

func TestAdminShortAliases(t *testing.T) {
	cfg := parseAdminUsage(t, "-a", "secret", "--proxy=127.0.0.1:11080", "--shutdown")
	adminMustString(t, adminStringArg(cfg, "--auth"), "--auth", "secret")
	adminMustBool(t, adminBoolArg(cfg, "--shutdown"), "--shutdown", true)

	cfg = parseAdminUsage(t, "-g", "1", "--dashboard=127.0.0.1:18080", "--create-group")
	adminMustString(t, adminStringArg(cfg, "--gid"), "--gid", "1")

	cfg = parseAdminUsage(t, "-x", "127.0.0.1:11080", "--dashboard=127.0.0.1:18080", "--remove-proxy")
	adminMustString(t, adminStringArg(cfg, "--addr"), "--addr", "127.0.0.1:11080")

	cfg = parseAdminUsage(t, "-t", "token-1", "--dashboard=127.0.0.1:18080", "--remove-proxy")
	adminMustString(t, adminStringArg(cfg, "--token"), "--token", "token-1")
}

// TestAdminOptionsAfterPositional pins that options may appear before or after
// the positional overview selector, as docopt allowed.
func TestAdminOptionsAfterPositional(t *testing.T) {
	cfg := parseAdminUsage(t, "model", "--proxy=127.0.0.1:11080", "-v")
	if cfg.Command != "model" {
		t.Fatalf("command = %q, want model", cfg.Command)
	}
	adminMustString(t, adminStringArg(cfg, "--proxy"), "--proxy", "127.0.0.1:11080")
	adminMustBool(t, adminBoolArg(cfg, "-v"), "-v", true)

	cfg = parseAdminUsage(t, "config", "--proxy", "127.0.0.1:11080")
	adminMustString(t, adminStringArg(cfg, "--proxy"), "--proxy", "127.0.0.1:11080")
}

// TestAdminUsageLinesParse covers every documented usage line so a missing
// flag registration or validator cannot silently drop a documented command.
func TestAdminUsageLinesParse(t *testing.T) {
	lines := [][]string{
		{"--proxy=127.0.0.1:11080"},
		{"--proxy=127.0.0.1:11080", "config"},
		{"--proxy=127.0.0.1:11080", "--start"},
		{"--proxy=127.0.0.1:11080", "--shutdown"},
		{"--proxy=127.0.0.1:11080", "--log-level=DEBUG"},
		{"--proxy=127.0.0.1:11080", "--fillslots=/tmp/slots.json", "--locked"},
		{"--proxy=127.0.0.1:11080", "--reset-stats"},
		{"--proxy=127.0.0.1:11080", "--forcegc"},

		{"--dashboard=127.0.0.1:18080"},
		{"--dashboard=127.0.0.1:18080", "group"},
		{"--dashboard=127.0.0.1:18080", "--shutdown"},
		{"--dashboard=127.0.0.1:18080", "--reload"},
		{"--dashboard=127.0.0.1:18080", "--log-level=WARN"},
		{"--dashboard=127.0.0.1:18080", "--slots-assign", "--beg=0", "--end=1023", "--gid=1", "--confirm"},
		{"--dashboard=127.0.0.1:18080", "--slots-status"},
		{"--dashboard=127.0.0.1:18080", "--list-proxy"},
		{"--dashboard=127.0.0.1:18080", "--create-proxy", "--addr=127.0.0.1:11080"},
		{"--dashboard=127.0.0.1:18080", "--online-proxy", "--addr=127.0.0.1:11080"},
		{"--dashboard=127.0.0.1:18080", "--remove-proxy", "--token=t", "--force"},
		{"--dashboard=127.0.0.1:18080", "--reinit-proxy", "--all", "--force"},
		{"--dashboard=127.0.0.1:18080", "--proxy-status"},
		{"--dashboard=127.0.0.1:18080", "--list-group"},
		{"--dashboard=127.0.0.1:18080", "--create-group", "--gid=1"},
		{"--dashboard=127.0.0.1:18080", "--remove-group", "--gid=1"},
		{"--dashboard=127.0.0.1:18080", "--resync-group", "--all"},
		{"--dashboard=127.0.0.1:18080", "--group-add", "--gid=1", "--addr=127.0.0.1:6379", "--datacenter=dc1"},
		{"--dashboard=127.0.0.1:18080", "--group-del", "--gid=1", "--addr=127.0.0.1:6379"},
		{"--dashboard=127.0.0.1:18080", "--group-status"},
		{"--dashboard=127.0.0.1:18080", "--replica-groups", "--gid=1", "--addr=127.0.0.1:6379", "--enable"},
		{"--dashboard=127.0.0.1:18080", "--promote-server", "--gid=1", "--addr=127.0.0.1:6379"},
		{"--dashboard=127.0.0.1:18080", "--sync-action", "--create", "--addr=127.0.0.1:6379"},
		{"--dashboard=127.0.0.1:18080", "--sync-action", "--remove", "--addr=127.0.0.1:6379"},
		{"--dashboard=127.0.0.1:18080", "--slot-action", "--create", "--sid=1", "--gid=2"},
		{"--dashboard=127.0.0.1:18080", "--slot-action", "--remove", "--sid=1"},
		{"--dashboard=127.0.0.1:18080", "--slot-action", "--create-some", "--gid-from=1", "--gid-to=2", "--num-slots=3"},
		{"--dashboard=127.0.0.1:18080", "--slot-action", "--create-range", "--beg=0", "--end=1", "--gid=2"},
		{"--dashboard=127.0.0.1:18080", "--slot-action", "--interval=3"},
		{"--dashboard=127.0.0.1:18080", "--slot-action", "--disabled=1"},
		{"--dashboard=127.0.0.1:18080", "--rebalance", "--confirm"},
		{"--dashboard=127.0.0.1:18080", "--acl"},
		{"--dashboard=127.0.0.1:18080", "--acl-set=/tmp/acl.json", "--confirm"},
		{"--dashboard=127.0.0.1:18080", "--acl-sync"},
		{"--dashboard=127.0.0.1:18080", "--rdb-analysis-remote-fetch", "--server=127.0.0.1:6379", "--topn=10", "--prefix-sep=:,", "--max-depth=1", "--regex=^user:", "--include-expired"},
		{"--dashboard=127.0.0.1:18080", "--pitr-create", "--server=127.0.0.1:6379", "--truncate-ts=1716000000"},
		{"--dashboard=127.0.0.1:18080", "--pitr-list"},
		{"--dashboard=127.0.0.1:18080", "--pitr-get", "--job=job-1"},
		{"--dashboard=127.0.0.1:18080", "--pitr-cancel", "--job=job-1"},
		{"--dashboard=127.0.0.1:18080", "--pitr-remove", "--job=job-1"},
		{"--dashboard=127.0.0.1:18080", "--sentinel-add", "--addr=127.0.0.1:26379"},
		{"--dashboard=127.0.0.1:18080", "--sentinel-del", "--addr=127.0.0.1:26379", "--force"},
		{"--dashboard=127.0.0.1:18080", "--sentinel-resync"},

		{"--show-lock", "--product=test-product", "--etcd=http://127.0.0.1:2379"},
		{"--remove-lock", "--product=test-product", "--expected-revision=417", "--confirm", "--etcd-config=/etc/etcd.toml"},
		{"--config-dump", "--product=test-product", "--etcd=http://127.0.0.1:2379"},
		{"--config-convert=/tmp/config.json"},
		{"--config-restore=/tmp/config.json", "--product=test-product", "--etcd=http://127.0.0.1:2379", "--confirm"},
		{"--dashboard-list", "--etcd-config=/etc/etcd.toml"},
	}
	want := []string{
		"", "config", "start", "shutdown", "log-level", "fillslots", "reset-stats", "forcegc",
		"", "group", "shutdown", "reload", "log-level", "slots-assign", "slots-status",
		"list-proxy", "create-proxy", "online-proxy", "remove-proxy", "reinit-proxy",
		"proxy-status", "list-group", "create-group", "remove-group", "resync-group",
		"group-add", "group-del", "group-status", "replica-groups", "promote-server",
		"sync-action", "sync-action", "slot-action", "slot-action", "slot-action",
		"slot-action", "slot-action", "slot-action", "rebalance", "acl", "acl-set",
		"acl-sync", "rdb-analysis-remote-fetch", "pitr-create", "pitr-list", "pitr-get",
		"pitr-cancel", "pitr-remove", "sentinel-add", "sentinel-del", "sentinel-resync",
		"show-lock", "remove-lock", "config-dump", "config-convert", "config-restore",
		"dashboard-list",
	}
	if len(want) != len(lines) {
		t.Fatalf("test table mismatch: %d usage lines, %d expected commands", len(lines), len(want))
	}
	for i, argv := range lines {
		cfg := parseAdminUsage(t, argv...)
		if cfg.Command != want[i] {
			t.Fatalf("argv %v: command = %q, want %q", argv, cfg.Command, want[i])
		}
	}
}

func TestAdminHelp(t *testing.T) {
	cfg, help, err := parseAdminArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("parseAdminArgs(--help) failed: %v", err)
	}
	if !help || cfg != nil {
		t.Fatalf("cfg = %+v, help = %v; want nil, true", cfg, help)
	}
}
