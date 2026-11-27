// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"flag"
	"fmt"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
)

type feConfig struct {
	Version cli.Bool

	NCPU          cli.String
	Log           cli.String
	LogLevel      cli.String
	AssetsDir     cli.String
	Pidfile       cli.String
	DashboardList cli.String
	Listen        cli.String

	// Coordinator holds the shared --etcd/--etcd-config options. Etcd is the
	// parsed selection: it is filled exactly when the dynamic source was
	// chosen, that is when --dashboard-list was not given.
	Coordinator coordarg.Flags
	Etcd        coordarg.Args
}

var feFlagAliases = map[string]string{
	"d": "dashboard-list",
	"l": "log",
}

func newFEFlagSet(cfg *feConfig) *flag.FlagSet {
	fs := flag.NewFlagSet("codis-fe", flag.ContinueOnError)
	fs.Var(&cfg.Version, "version", "print version")
	fs.Var(&cfg.NCPU, "ncpu", "set runtime.GOMAXPROCS to N")
	fs.Var(&cfg.Log, "log", "set path/name of daily rotated log file")
	fs.Var(&cfg.LogLevel, "log-level", "set the log-level")
	fs.Var(&cfg.AssetsDir, "assets-dir", "set the assets directory")
	fs.Var(&cfg.Pidfile, "pidfile", "write process id to file")
	fs.Var(&cfg.DashboardList, "dashboard-list", "set list of dashboards")
	fs.Var(&cfg.Listen, "listen", "set the listen address")
	cfg.Coordinator.Bind(fs)
	return fs
}

func parseFEArgs(args []string) (*feConfig, bool, error) {
	cfg := &feConfig{}
	parsed, help, err := cli.Parse(newFEFlagSet(cfg), args, feFlagAliases)
	if help || err != nil {
		return nil, help, err
	}
	if len(parsed.Positionals) != 0 {
		return nil, false, fmt.Errorf("unexpected argument %q", parsed.Positionals[0])
	}

	if parsed.IsSet("version") {
		if err := parsed.Only("version"); err != nil {
			return nil, false, err
		}
		return cfg, false, nil
	}

	etcd, etcdSet, err := cfg.Coordinator.Parse()
	if err != nil {
		return nil, false, err
	}

	if !cfg.Listen.Provided {
		return nil, false, fmt.Errorf("option --listen is required")
	}
	// The static dashboard list and the dynamic etcd source are mutually
	// exclusive. Giving both shared options counts as one source: --etcd
	// supplies the addresses while --etcd-config supplies the connection
	// options of the same selection.
	if cfg.DashboardList.Provided && etcdSet {
		return nil, false, fmt.Errorf("option --dashboard-list is mutually exclusive with --etcd/--etcd-config, use exactly one dashboard source")
	}
	if !cfg.DashboardList.Provided && !etcdSet {
		return nil, false, fmt.Errorf("exactly one dashboard source is required: --dashboard-list or --etcd/--etcd-config")
	}
	cfg.Etcd = etcd
	return cfg, false, nil
}
