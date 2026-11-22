// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"flag"
	"fmt"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
)

type proxyConfig struct {
	DefaultConfig cli.Bool
	Version       cli.Bool

	NCPU        cli.String
	MaxNCPU     cli.String
	Config      cli.String
	Log         cli.String
	LogLevel    cli.String
	HostAdmin   cli.String
	HostProxy   cli.String
	Dashboard   cli.String
	FillSlots   cli.String
	Ulimit      cli.String
	Pidfile     cli.String
	ProductName cli.String
	ProductAuth cli.String
	SessionAuth cli.String

	Coordinator    coordarg.Flags
	CoordinatorArg coordarg.Args
	HasCoordinator bool
}

var proxyFlagAliases = map[string]string{
	"c": "config",
	"l": "log",
}

func newProxyFlagSet(cfg *proxyConfig) *flag.FlagSet {
	fs := flag.NewFlagSet("codis-proxy", flag.ContinueOnError)
	fs.Var(&cfg.DefaultConfig, "default-config", "print the default configuration")
	fs.Var(&cfg.Version, "version", "print version")
	fs.Var(&cfg.NCPU, "ncpu", "set runtime.GOMAXPROCS to N")
	fs.Var(&cfg.MaxNCPU, "max-ncpu", "maximum GOMAXPROCS")
	fs.Var(&cfg.Config, "config", "run with the specific configuration")
	fs.Var(&cfg.Log, "log", "set path/name of daily rotated log file")
	fs.Var(&cfg.LogLevel, "log-level", "set the log-level")
	fs.Var(&cfg.HostAdmin, "host-admin", "set the admin listen address")
	fs.Var(&cfg.HostProxy, "host-proxy", "set the proxy listen address")
	fs.Var(&cfg.Dashboard, "dashboard", "dashboard address")
	fs.Var(&cfg.FillSlots, "fillslots", "fill slots from file")
	fs.Var(&cfg.Ulimit, "ulimit", "check the maximum number of open file descriptors")
	fs.Var(&cfg.Pidfile, "pidfile", "write process id to file")
	fs.Var(&cfg.ProductName, "product_name", "set product name")
	fs.Var(&cfg.ProductAuth, "product_auth", "set product auth")
	fs.Var(&cfg.SessionAuth, "session_auth", "set session auth")
	cfg.Coordinator.Bind(fs)
	return fs
}

func parseProxyArgs(args []string) (*proxyConfig, bool, error) {
	cfg := &proxyConfig{}
	parsed, help, err := cli.Parse(newProxyFlagSet(cfg), args, proxyFlagAliases)
	if help || err != nil {
		return nil, help, err
	}
	if len(parsed.Positionals) != 0 {
		return nil, false, fmt.Errorf("unexpected argument %q", parsed.Positionals[0])
	}

	if parsed.IsSet("default-config") || parsed.IsSet("version") {
		if _, err := parsed.ExactlyOne("default-config", "version"); err != nil {
			return nil, false, err
		}
		if err := parsed.Only("default-config", "version"); err != nil {
			return nil, false, err
		}
		return cfg, false, nil
	}

	if err := parsed.AtMostOne("dashboard", "zookeeper", "etcd", "filesystem", "consul", "fillslots"); err != nil {
		return nil, false, err
	}
	if parsed.IsSet("max-ncpu") && !parsed.IsSet("ncpu") {
		return nil, false, fmt.Errorf("option --max-ncpu requires --ncpu")
	}

	coordinator, ok, err := cfg.Coordinator.Parse()
	if err != nil {
		return nil, false, err
	}
	cfg.CoordinatorArg, cfg.HasCoordinator = coordinator, ok
	return cfg, false, nil
}
