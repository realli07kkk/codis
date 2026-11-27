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

	// Coordinator holds the shared --etcd/--etcd-config options. Etcd is the
	// parsed result: HasEndpoints selects etcd auto-online, HasConfigFile only
	// supplies connection options for the online registration.
	Coordinator coordarg.Flags
	Etcd        coordarg.Args
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

	// The three automatic online entries are mutually exclusive; all of them
	// may be omitted so the dashboard adds the proxy manually. --etcd-config
	// is not an entry: it only carries connection options, so it may be
	// combined with any of them (for the online registration).
	if err := parsed.AtMostOne("dashboard", "etcd", "fillslots"); err != nil {
		return nil, false, err
	}
	if parsed.IsSet("max-ncpu") && !parsed.IsSet("ncpu") {
		return nil, false, fmt.Errorf("option --max-ncpu requires --ncpu")
	}

	etcd, _, err := cfg.Coordinator.Parse()
	if err != nil {
		return nil, false, err
	}
	cfg.Etcd = etcd
	return cfg, false, nil
}

// onlineSource is the automatic online entry selected by the command line.
type onlineSource int

const (
	onlineManual onlineSource = iota
	onlineDashboard
	onlineEtcd
	onlineFillSlots
)

// onlineMode reports how the proxy is brought online. The order follows the
// documented precedence and the parser rejects more than one entry. A config
// file alone is not an entry: it only carries connection options, so such a
// proxy waits for the dashboard to add it manually.
func (c *proxyConfig) onlineMode() onlineSource {
	switch {
	case c.Dashboard.Provided:
		return onlineDashboard
	case c.Etcd.HasEndpoints:
		return onlineEtcd
	case c.FillSlots.Provided:
		return onlineFillSlots
	}
	return onlineManual
}
