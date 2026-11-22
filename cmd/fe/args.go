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

	Coordinator    coordarg.Flags
	CoordinatorArg coordarg.Args
	HasCoordinator bool
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

	if !cfg.Listen.Provided {
		return nil, false, fmt.Errorf("option --listen is required")
	}
	if _, err := parsed.ExactlyOne("dashboard-list", "zookeeper", "filesystem", "consul"); err != nil {
		return nil, false, err
	}

	coordinator, ok, err := cfg.Coordinator.Parse()
	if err != nil {
		return nil, false, err
	}
	cfg.CoordinatorArg, cfg.HasCoordinator = coordinator, ok
	return cfg, false, nil
}
