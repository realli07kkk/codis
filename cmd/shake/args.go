// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"flag"
	"fmt"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

type shakeConfig struct {
	Version cli.Bool
	Spec    cli.String
}

func newShakeFlagSet(cfg *shakeConfig) *flag.FlagSet {
	fs := flag.NewFlagSet("codis-shake", flag.ContinueOnError)
	fs.Var(&cfg.Version, "version", "print version")
	fs.Var(&cfg.Spec, "spec", "path of the migration spec.json written by codis-dashboard")
	return fs
}

func parseShakeArgs(args []string) (*shakeConfig, bool, error) {
	cfg := &shakeConfig{}
	parsed, help, err := cli.Parse(newShakeFlagSet(cfg), args, nil)
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
	if !cfg.Spec.Provided {
		return nil, false, fmt.Errorf("--spec is required")
	}
	return cfg, false, nil
}
