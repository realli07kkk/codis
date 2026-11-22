// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"flag"
	"fmt"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
)

// adminConfig is the typed result of parsing the codis-admin command line.
// Command holds the selected command (a positional overview selector or a
// command flag); the remaining fields hold the option values.
type adminConfig struct {
	Verbose cli.Bool

	Proxy      cli.String
	Dashboard  cli.String
	Auth       cli.String
	Addr       cli.String
	Token      cli.String
	Gid        cli.String
	Pid        cli.String
	Product    cli.String
	Datacenter cli.String
	LogLevel   cli.String
	FillSlots  cli.String
	Beg        cli.String
	End        cli.String
	Sid        cli.String
	GidFrom    cli.String
	GidTo      cli.String
	NumSlots   cli.String
	Interval   cli.String
	Disabled   cli.String
	ACLSet     cli.String
	Server     cli.String
	TopN       cli.String
	PrefixSep  cli.String
	MaxDepth   cli.String
	Regex      cli.String
	TruncateTS cli.String
	Job        cli.String

	ConfigConvert cli.String
	ConfigRestore cli.String

	Start         cli.Bool
	Shutdown      cli.Bool
	Reload        cli.Bool
	Locked        cli.Bool
	ResetStats    cli.Bool
	ForceGC       cli.Bool
	RemoveLock    cli.Bool
	ConfigDump    cli.Bool
	ConfigDumpV1  cli.Bool
	DashboardList cli.Bool

	SlotsAssign cli.Bool
	SlotsStatus cli.Bool
	Offline     cli.Bool
	Confirm     cli.Bool

	ListProxy   cli.Bool
	CreateProxy cli.Bool
	OnlineProxy cli.Bool
	RemoveProxy cli.Bool
	ReinitProxy cli.Bool
	ProxyStatus cli.Bool
	Force       cli.Bool
	All         cli.Bool

	ListGroup     cli.Bool
	CreateGroup   cli.Bool
	RemoveGroup   cli.Bool
	ResyncGroup   cli.Bool
	GroupAdd      cli.Bool
	GroupDel      cli.Bool
	GroupStatus   cli.Bool
	ReplicaGroups cli.Bool
	Enable        cli.Bool
	Disable       cli.Bool
	PromoteServer cli.Bool

	SyncAction cli.Bool
	Create     cli.Bool
	Remove     cli.Bool

	SlotAction  cli.Bool
	CreateSome  cli.Bool
	CreateRange cli.Bool

	Rebalance cli.Bool
	ACL       cli.Bool
	ACLSync   cli.Bool

	RDBAnalysisRemoteFetch cli.Bool
	IncludeExpired         cli.Bool

	PitrCreate cli.Bool
	PitrList   cli.Bool
	PitrGet    cli.Bool
	PitrCancel cli.Bool
	PitrRemove cli.Bool

	SentinelAdd    cli.Bool
	SentinelDel    cli.Bool
	SentinelResync cli.Bool

	Coordinator    coordarg.Flags
	CoordinatorArg coordarg.Args
	HasCoordinator bool

	Command string
}

var adminFlagAliases = map[string]string{
	"a": "auth",
	"x": "addr",
	"t": "token",
	"g": "gid",
	"v": "v",
	"1": "1",
}

func newAdminFlagSet(cfg *adminConfig) *flag.FlagSet {
	fs := flag.NewFlagSet("codis-admin", flag.ContinueOnError)
	fs.Var(&cfg.Verbose, "v", "debug logging")
	fs.Var(&cfg.Proxy, "proxy", "proxy address")
	fs.Var(&cfg.Dashboard, "dashboard", "dashboard address")
	fs.Var(&cfg.Auth, "auth", "proxy auth")
	fs.Var(&cfg.Addr, "addr", "server address")
	fs.Var(&cfg.Token, "token", "proxy token")
	fs.Var(&cfg.Gid, "gid", "group id")
	fs.Var(&cfg.Pid, "pid", "proxy pid")
	fs.Var(&cfg.Product, "product", "product name")
	fs.Var(&cfg.Datacenter, "datacenter", "datacenter name")
	fs.Var(&cfg.LogLevel, "log-level", "log level")
	fs.Var(&cfg.FillSlots, "fillslots", "fill slots from file")
	fs.Var(&cfg.Beg, "beg", "begin slot id")
	fs.Var(&cfg.End, "end", "end slot id")
	fs.Var(&cfg.Sid, "sid", "slot id")
	fs.Var(&cfg.GidFrom, "gid-from", "source group id")
	fs.Var(&cfg.GidTo, "gid-to", "destination group id")
	fs.Var(&cfg.NumSlots, "num-slots", "number of slots")
	fs.Var(&cfg.Interval, "interval", "slot action interval")
	fs.Var(&cfg.Disabled, "disabled", "slot action disabled")
	fs.Var(&cfg.ACLSet, "acl-set", "ACL file")
	fs.Var(&cfg.Server, "server", "server address")
	fs.Var(&cfg.TopN, "topn", "top N")
	fs.Var(&cfg.PrefixSep, "prefix-sep", "prefix separators")
	fs.Var(&cfg.MaxDepth, "max-depth", "maximum depth")
	fs.Var(&cfg.Regex, "regex", "key regex")
	fs.Var(&cfg.TruncateTS, "truncate-ts", "truncate timestamp")
	fs.Var(&cfg.Job, "job", "pitr job id")
	fs.Var(&cfg.ConfigConvert, "config-convert", "convert config file")
	fs.Var(&cfg.ConfigRestore, "config-restore", "restore config file")

	fs.Var(&cfg.Start, "start", "start proxy")
	fs.Var(&cfg.Shutdown, "shutdown", "shutdown service")
	fs.Var(&cfg.Reload, "reload", "reload dashboard")
	fs.Var(&cfg.Locked, "locked", "lock filled slots")
	fs.Var(&cfg.ResetStats, "reset-stats", "reset proxy stats")
	fs.Var(&cfg.ForceGC, "forcegc", "force GC")
	fs.Var(&cfg.RemoveLock, "remove-lock", "force remove product lock")
	fs.Var(&cfg.ConfigDump, "config-dump", "dump product config")
	fs.Var(&cfg.ConfigDumpV1, "1", "dump config in v1 format")
	fs.Var(&cfg.DashboardList, "dashboard-list", "list dashboards")

	fs.Var(&cfg.SlotsAssign, "slots-assign", "assign slots")
	fs.Var(&cfg.SlotsStatus, "slots-status", "show slots status")
	fs.Var(&cfg.Offline, "offline", "mark slots offline")
	fs.Var(&cfg.Confirm, "confirm", "confirm the operation")

	fs.Var(&cfg.ListProxy, "list-proxy", "list proxies")
	fs.Var(&cfg.CreateProxy, "create-proxy", "create proxy")
	fs.Var(&cfg.OnlineProxy, "online-proxy", "online proxy")
	fs.Var(&cfg.RemoveProxy, "remove-proxy", "remove proxy")
	fs.Var(&cfg.ReinitProxy, "reinit-proxy", "reinit proxy")
	fs.Var(&cfg.ProxyStatus, "proxy-status", "show proxy status")
	fs.Var(&cfg.Force, "force", "force the operation")
	fs.Var(&cfg.All, "all", "apply to all")

	fs.Var(&cfg.ListGroup, "list-group", "list groups")
	fs.Var(&cfg.CreateGroup, "create-group", "create group")
	fs.Var(&cfg.RemoveGroup, "remove-group", "remove group")
	fs.Var(&cfg.ResyncGroup, "resync-group", "resync group")
	fs.Var(&cfg.GroupAdd, "group-add", "add group server")
	fs.Var(&cfg.GroupDel, "group-del", "delete group server")
	fs.Var(&cfg.GroupStatus, "group-status", "show group status")
	fs.Var(&cfg.ReplicaGroups, "replica-groups", "enable/disable replica groups")
	fs.Var(&cfg.Enable, "enable", "enable replica groups")
	fs.Var(&cfg.Disable, "disable", "disable replica groups")
	fs.Var(&cfg.PromoteServer, "promote-server", "promote group server")

	fs.Var(&cfg.SyncAction, "sync-action", "sync action")
	fs.Var(&cfg.Create, "create", "create action")
	fs.Var(&cfg.Remove, "remove", "remove action")

	fs.Var(&cfg.SlotAction, "slot-action", "slot action")
	fs.Var(&cfg.CreateSome, "create-some", "create slots in a group range")
	fs.Var(&cfg.CreateRange, "create-range", "create slots in a slot range")

	fs.Var(&cfg.Rebalance, "rebalance", "rebalance slots")
	fs.Var(&cfg.ACL, "acl", "show ACL")
	fs.Var(&cfg.ACLSync, "acl-sync", "sync ACL")

	fs.Var(&cfg.RDBAnalysisRemoteFetch, "rdb-analysis-remote-fetch", "run remote RDB analysis")
	fs.Var(&cfg.IncludeExpired, "include-expired", "include expired keys")

	fs.Var(&cfg.PitrCreate, "pitr-create", "create a PITR job")
	fs.Var(&cfg.PitrList, "pitr-list", "list PITR jobs")
	fs.Var(&cfg.PitrGet, "pitr-get", "get a PITR job")
	fs.Var(&cfg.PitrCancel, "pitr-cancel", "cancel a PITR job")
	fs.Var(&cfg.PitrRemove, "pitr-remove", "remove a PITR job")

	fs.Var(&cfg.SentinelAdd, "sentinel-add", "add sentinel")
	fs.Var(&cfg.SentinelDel, "sentinel-del", "delete sentinel")
	fs.Var(&cfg.SentinelResync, "sentinel-resync", "resync sentinels")

	cfg.Coordinator.Bind(fs)
	return fs
}

func parseAdminArgs(args []string) (*adminConfig, bool, error) {
	cfg := &adminConfig{}
	parsed, help, err := cli.Parse(newAdminFlagSet(cfg), args, adminFlagAliases)
	if help || err != nil {
		return nil, help, err
	}
	if len(parsed.Positionals) > 1 {
		return nil, false, fmt.Errorf("unexpected argument %q", parsed.Positionals[1])
	}
	var positional string
	if len(parsed.Positionals) == 1 {
		positional = parsed.Positionals[0]
	}

	switch {
	case parsed.IsSet("proxy"):
		if parsed.IsSet("dashboard") {
			return nil, false, fmt.Errorf("options --proxy and --dashboard are mutually exclusive")
		}
		err = parseAdminProxyMode(cfg, parsed, positional)
	case parsed.IsSet("dashboard"):
		err = parseAdminDashboardMode(cfg, parsed, positional)
	default:
		if positional != "" {
			return nil, false, fmt.Errorf("unexpected argument %q", positional)
		}
		err = parseAdminTailMode(cfg, parsed)
	}
	if err != nil {
		return nil, false, err
	}
	return cfg, false, nil
}

var adminProxyCommands = []string{
	"start", "shutdown", "log-level", "fillslots", "reset-stats", "forcegc",
}

func parseAdminProxyMode(cfg *adminConfig, parsed *cli.Args, positional string) error {
	allowed := []string{"v", "proxy", "auth"}
	allowed = append(allowed, adminProxyCommands...)
	allowed = append(allowed, "locked")
	if err := parsed.Only(allowed...); err != nil {
		return err
	}

	switch positional {
	case "", "config", "model", "stats", "slots":
		cfg.Command = positional
	default:
		return fmt.Errorf("unknown command %q for --proxy", positional)
	}

	if cfg.Command != "" && parsed.CountSet(adminProxyCommands...) != 0 {
		return fmt.Errorf("command %q cannot be combined with a command option", positional)
	}
	if parsed.CountSet(adminProxyCommands...) > 1 {
		_, err := parsed.ExactlyOne(adminProxyCommands...)
		return err
	}
	if cfg.Command == "" && parsed.CountSet(adminProxyCommands...) == 1 {
		cmd, _ := parsed.ExactlyOne(adminProxyCommands...)
		cfg.Command = cmd
	}
	if parsed.IsSet("locked") && !parsed.IsSet("fillslots") {
		return fmt.Errorf("option --locked requires --fillslots")
	}
	return nil
}

var adminDashboardCommands = []string{
	"shutdown", "reload", "log-level",
	"slots-assign", "slots-status",
	"create-proxy", "online-proxy", "remove-proxy", "reinit-proxy", "proxy-status",
	"create-group", "remove-group", "resync-group", "group-add", "group-del",
	"group-status", "replica-groups", "promote-server",
	"sentinel-add", "sentinel-del", "sentinel-resync",
	"sync-action", "slot-action", "rebalance",
	"acl", "acl-set", "acl-sync",
	"rdb-analysis-remote-fetch",
	"pitr-create", "pitr-list", "pitr-get", "pitr-cancel", "pitr-remove",
}

func parseAdminDashboardMode(cfg *adminConfig, parsed *cli.Args, positional string) error {
	switch positional {
	case "", "config", "model", "stats", "slots", "group", "proxy":
		cfg.Command = positional
	default:
		return fmt.Errorf("unknown command %q for --dashboard", positional)
	}
	if cfg.Command != "" && parsed.CountSet(adminDashboardCommands...) != 0 {
		return fmt.Errorf("command %q cannot be combined with a command option", positional)
	}
	if parsed.CountSet("list-group", "list-proxy") > 0 && cfg.Command != "" {
		return fmt.Errorf("command %q cannot be combined with --list-group or --list-proxy", positional)
	}
	if err := parsed.AtMostOne("list-group", "list-proxy"); err != nil {
		return err
	}
	if parsed.CountSet(adminDashboardCommands...) > 1 {
		_, err := parsed.ExactlyOne(adminDashboardCommands...)
		return err
	}

	if parsed.CountSet("list-group", "list-proxy") > 0 && parsed.CountSet(adminDashboardCommands...) > 0 {
		return fmt.Errorf("--list-group and --list-proxy cannot be combined with a command option")
	}

	if cfg.Command == "" {
		switch {
		case parsed.IsSet("list-group"):
			cfg.Command = "list-group"
		case parsed.IsSet("list-proxy"):
			cfg.Command = "list-proxy"
		case parsed.CountSet(adminDashboardCommands...) == 1:
			cmd, err := parsed.ExactlyOne(adminDashboardCommands...)
			if err != nil {
				return err
			}
			cfg.Command = cmd
		}
	}

	allowed := []string{"v", "dashboard"}
	switch cfg.Command {
	case "list-group":
		allowed = append(allowed, "list-group")
	case "list-proxy":
		allowed = append(allowed, "list-proxy")
	}

	switch cfg.Command {
	case "", "config", "model", "stats", "slots", "group", "proxy", "list-group", "list-proxy":
		// overview
	case "shutdown":
		allowed = append(allowed, "shutdown")
	case "reload":
		allowed = append(allowed, "reload")
	case "log-level":
		allowed = append(allowed, "log-level")
	case "slots-status":
		allowed = append(allowed, "slots-status")
	case "slots-assign":
		allowed = append(allowed, "slots-assign", "beg", "end", "gid", "offline", "confirm")
		if err := requireOptions(parsed, "beg", "end"); err != nil {
			return err
		}
		if _, err := parsed.ExactlyOne("gid", "offline"); err != nil {
			return err
		}
	case "create-proxy":
		allowed = append(allowed, "create-proxy", "addr")
		if err := requireOptions(parsed, "addr"); err != nil {
			return err
		}
	case "online-proxy":
		allowed = append(allowed, "online-proxy", "addr")
		if err := requireOptions(parsed, "addr"); err != nil {
			return err
		}
	case "remove-proxy":
		allowed = append(allowed, "remove-proxy", "addr", "token", "pid", "force")
		if _, err := parsed.ExactlyOne("addr", "token", "pid"); err != nil {
			return err
		}
	case "reinit-proxy":
		allowed = append(allowed, "reinit-proxy", "addr", "token", "pid", "all", "force")
		if _, err := parsed.ExactlyOne("addr", "token", "pid", "all"); err != nil {
			return err
		}
	case "proxy-status":
		allowed = append(allowed, "proxy-status")
	case "create-group":
		allowed = append(allowed, "create-group", "gid")
		if err := requireOptions(parsed, "gid"); err != nil {
			return err
		}
	case "remove-group":
		allowed = append(allowed, "remove-group", "gid")
		if err := requireOptions(parsed, "gid"); err != nil {
			return err
		}
	case "resync-group":
		allowed = append(allowed, "resync-group", "gid", "all")
		if err := parsed.AtMostOne("gid", "all"); err != nil {
			return err
		}
	case "group-add":
		allowed = append(allowed, "group-add", "gid", "addr", "datacenter")
		if err := requireOptions(parsed, "gid", "addr"); err != nil {
			return err
		}
	case "group-del":
		allowed = append(allowed, "group-del", "gid", "addr")
		if err := requireOptions(parsed, "gid", "addr"); err != nil {
			return err
		}
	case "group-status":
		allowed = append(allowed, "group-status")
	case "replica-groups":
		allowed = append(allowed, "replica-groups", "gid", "addr", "enable", "disable")
		if err := requireOptions(parsed, "gid", "addr"); err != nil {
			return err
		}
		if _, err := parsed.ExactlyOne("enable", "disable"); err != nil {
			return err
		}
	case "promote-server":
		allowed = append(allowed, "promote-server", "gid", "addr")
		if err := requireOptions(parsed, "gid", "addr"); err != nil {
			return err
		}
	case "sync-action":
		allowed = append(allowed, "sync-action", "create", "remove", "addr")
		if _, err := parsed.ExactlyOne("create", "remove"); err != nil {
			return err
		}
		if err := requireOptions(parsed, "addr"); err != nil {
			return err
		}
	case "slot-action":
		allowed = append(allowed, "slot-action")
		sub, err := parsed.ExactlyOne("create", "remove", "create-some", "create-range", "interval", "disabled")
		if err != nil {
			return err
		}
		switch sub {
		case "create":
			allowed = append(allowed, "create", "sid", "gid")
			if err := requireOptions(parsed, "sid", "gid"); err != nil {
				return err
			}
		case "remove":
			allowed = append(allowed, "remove", "sid")
			if err := requireOptions(parsed, "sid"); err != nil {
				return err
			}
		case "create-some":
			allowed = append(allowed, "create-some", "gid-from", "gid-to", "num-slots")
			if err := requireOptions(parsed, "gid-from", "gid-to", "num-slots"); err != nil {
				return err
			}
		case "create-range":
			allowed = append(allowed, "create-range", "beg", "end", "gid")
			if err := requireOptions(parsed, "beg", "end", "gid"); err != nil {
				return err
			}
		case "interval":
			allowed = append(allowed, "interval")
		case "disabled":
			allowed = append(allowed, "disabled")
		}
	case "rebalance":
		allowed = append(allowed, "rebalance", "confirm")
	case "acl":
		allowed = append(allowed, "acl")
	case "acl-set":
		allowed = append(allowed, "acl-set", "confirm")
	case "acl-sync":
		allowed = append(allowed, "acl-sync")
	case "rdb-analysis-remote-fetch":
		allowed = append(allowed, "rdb-analysis-remote-fetch", "server", "topn", "prefix-sep", "max-depth", "regex", "include-expired")
		if err := requireOptions(parsed, "server"); err != nil {
			return err
		}
	case "pitr-create":
		allowed = append(allowed, "pitr-create", "server", "truncate-ts")
		if err := requireOptions(parsed, "server", "truncate-ts"); err != nil {
			return err
		}
	case "pitr-list":
		allowed = append(allowed, "pitr-list")
	case "pitr-get", "pitr-cancel", "pitr-remove":
		allowed = append(allowed, cfg.Command, "job")
		if err := requireOptions(parsed, "job"); err != nil {
			return err
		}
	case "sentinel-add":
		allowed = append(allowed, "sentinel-add", "addr")
		if err := requireOptions(parsed, "addr"); err != nil {
			return err
		}
	case "sentinel-del":
		allowed = append(allowed, "sentinel-del", "addr", "force")
		if err := requireOptions(parsed, "addr"); err != nil {
			return err
		}
	case "sentinel-resync":
		allowed = append(allowed, "sentinel-resync")
	}

	return parsed.Only(allowed...)
}

func parseAdminTailMode(cfg *adminConfig, parsed *cli.Args) error {
	commands := []string{"remove-lock", "config-dump", "config-convert", "config-restore", "dashboard-list"}
	cmd, err := parsed.ExactlyOne(commands...)
	if err != nil {
		return fmt.Errorf("exactly one of --remove-lock, --config-dump, --config-convert, --config-restore, --dashboard-list is required")
	}
	cfg.Command = cmd

	allowed := []string{"v"}
	switch cmd {
	case "remove-lock":
		allowed = append(allowed, "remove-lock", "product")
		if err := requireOptions(parsed, "product"); err != nil {
			return err
		}
	case "config-dump":
		allowed = append(allowed, "config-dump", "product", "1")
		if err := requireOptions(parsed, "product"); err != nil {
			return err
		}
	case "config-convert":
		allowed = append(allowed, "config-convert")
	case "config-restore":
		allowed = append(allowed, "config-restore", "product", "confirm")
		if err := requireOptions(parsed, "product"); err != nil {
			return err
		}
	case "dashboard-list":
		allowed = append(allowed, "dashboard-list")
	}

	if cmd == "config-convert" {
		return parsed.Only(allowed...)
	}

	allowed = append(allowed, coordarg.FlagNames()...)
	if err := parsed.Only(allowed...); err != nil {
		return err
	}
	coordinator, ok, err := cfg.Coordinator.Parse()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("exactly one of --zookeeper, --filesystem, --consul is required")
	}
	cfg.CoordinatorArg, cfg.HasCoordinator = coordinator, true
	return nil
}

func requireOptions(parsed *cli.Args, names ...string) error {
	for _, name := range names {
		if !parsed.IsSet(name) {
			return fmt.Errorf("option %s is required", cli.FlagName(name))
		}
	}
	return nil
}
