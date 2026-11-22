// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"flag"
	"fmt"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

type haConfig struct {
	Version     cli.Bool
	Log         cli.String
	LogLevel    cli.String
	Interval    cli.String
	Dashboard   cli.String
	NoMaintains cli.Bool
}

var haFlagAliases = map[string]string{
	"l": "log",
}

func newHAFlagSet(cfg *haConfig) *flag.FlagSet {
	fs := flag.NewFlagSet("codis-ha", flag.ContinueOnError)
	fs.Var(&cfg.Version, "version", "print version")
	fs.Var(&cfg.Log, "log", "set path/name of daily rotated log file")
	fs.Var(&cfg.LogLevel, "log-level", "set the log-level")
	fs.Var(&cfg.Interval, "interval", "set health check interval in seconds")
	fs.Var(&cfg.Dashboard, "dashboard", "dashboard address")
	fs.Var(&cfg.NoMaintains, "no-maintains", "disable automatic maintenance")
	return fs
}

func parseHAArgs(args []string) (*haConfig, bool, error) {
	cfg := &haConfig{}
	parsed, help, err := cli.Parse(newHAFlagSet(cfg), args, haFlagAliases)
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

	if !cfg.Dashboard.Provided {
		return nil, false, fmt.Errorf("option --dashboard is required")
	}
	return cfg, false, nil
}
