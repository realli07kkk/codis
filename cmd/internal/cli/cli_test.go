// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package cli

import (
	"flag"
	"testing"
)

type testFlags struct {
	name    String
	verbose Bool
	debug   Bool
}

func newTestFlagSet(cfg *testFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Var(&cfg.name, "name", "a name")
	fs.Var(&cfg.verbose, "v", "verbose")
	fs.Var(&cfg.debug, "debug", "debug")
	return fs
}

var testAliases = map[string]string{
	"n": "name",
	"v": "v",
}

func parseTest(t *testing.T, argv ...string) (*testFlags, *Args) {
	t.Helper()
	cfg := &testFlags{}
	args, help, err := Parse(newTestFlagSet(cfg), argv, testAliases)
	if err != nil {
		t.Fatalf("Parse(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("Parse(%v) unexpectedly asked for help", argv)
	}
	return cfg, args
}

func TestStringValueSpellings(t *testing.T) {
	for _, argv := range [][]string{
		{"--name=alpha"},
		{"--name", "alpha"},
		{"-n", "alpha"},
	} {
		cfg, _ := parseTest(t, argv...)
		if !cfg.name.Provided || cfg.name.Value != "alpha" {
			t.Fatalf("argv %v: name = %+v, want alpha", argv, cfg.name)
		}
	}
}

func TestShortValueSpellings(t *testing.T) {
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"-n", "alpha"}, "alpha"},
		{[]string{"-nalpha"}, "alpha"},
		{[]string{"-n=alpha"}, "=alpha"},
	}
	for _, tt := range tests {
		cfg, _ := parseTest(t, tt.argv...)
		if !cfg.name.Provided || cfg.name.Value != tt.want {
			t.Fatalf("argv %v: name = %+v, want %q", tt.argv, cfg.name, tt.want)
		}
	}

	cfg, _ := parseTest(t, "-vnalpha")
	if !cfg.verbose.Value || !cfg.name.Provided || cfg.name.Value != "alpha" {
		t.Fatalf("cluster = verbose %+v, name %+v", cfg.verbose, cfg.name)
	}
}

func TestOptionsAfterPositionals(t *testing.T) {
	cfg, args := parseTest(t, "config", "-n", "alpha", "model", "--debug")
	if !cfg.name.Provided || cfg.name.Value != "alpha" {
		t.Fatalf("name = %+v", cfg.name)
	}
	if !cfg.debug.Value || !cfg.debug.Provided {
		t.Fatalf("debug = %+v", cfg.debug)
	}
	if len(args.Positionals) != 2 || args.Positionals[0] != "config" || args.Positionals[1] != "model" {
		t.Fatalf("positionals = %v", args.Positionals)
	}
}

func TestBoolDefaultsAndProvideState(t *testing.T) {
	cfg, args := parseTest(t, "--name=x")
	if cfg.debug.Value || cfg.debug.Provided {
		t.Fatalf("debug = %+v, want absent false", cfg.debug)
	}
	if args.IsSet("debug") {
		t.Fatal("debug reported as set")
	}

	cfg, args = parseTest(t, "--debug")
	if !cfg.debug.Value || !cfg.debug.Provided || !args.IsSet("debug") {
		t.Fatalf("debug = %+v, set = %v", cfg.debug, args.IsSet("debug"))
	}
}

func TestBoolRejectsExplicitValue(t *testing.T) {
	for _, argv := range [][]string{
		{"--debug=true"},
		{"--debug=false"},
		{"--debug="},
		{"-v=false"},
	} {
		cfg := &testFlags{}
		if _, _, err := Parse(newTestFlagSet(cfg), argv, testAliases); err == nil {
			t.Fatalf("argv %v: expected boolean option error", argv)
		}
	}
}

func TestUnknownAndAliasSpellings(t *testing.T) {
	for _, argv := range [][]string{
		{"--missing=x"},
		{"--n=x"},
		{"--v"},
		{"-verbosex"},
		{"-x"},
	} {
		cfg := &testFlags{}
		if _, _, err := Parse(newTestFlagSet(cfg), argv, testAliases); err == nil {
			t.Fatalf("argv %v: expected unknown flag error", argv)
		}
	}
}

func TestEmptyStringRejected(t *testing.T) {
	cfg := &testFlags{}
	if _, _, err := Parse(newTestFlagSet(cfg), []string{"--name="}, testAliases); err == nil {
		t.Fatal("expected empty string to be rejected")
	}
}

func TestMissingValueAtEndRejected(t *testing.T) {
	cfg := &testFlags{}
	if _, _, err := Parse(newTestFlagSet(cfg), []string{"config", "--name"}, testAliases); err == nil {
		t.Fatal("expected an option missing its value to be rejected")
	}
	if cfg.name.Provided {
		t.Fatalf("name = %+v, want unprovided", cfg.name)
	}
}

func TestDoubleDashTerminatesOptionsWithoutBecomingPositional(t *testing.T) {
	cfg := &testFlags{}
	args, help, err := Parse(newTestFlagSet(cfg), []string{"--", "--name=positional"}, testAliases)
	if err != nil || help {
		t.Fatalf("Parse: help = %v, err = %v", help, err)
	}
	if cfg.name.Provided {
		t.Fatalf("option after -- was parsed: %+v", cfg.name)
	}
	if len(args.Positionals) != 1 || args.Positionals[0] != "--name=positional" {
		t.Fatalf("positionals = %q", args.Positionals)
	}
}

func TestHelp(t *testing.T) {
	for _, argv := range [][]string{{"-h"}, {"--help"}} {
		cfg := &testFlags{}
		args, help, err := Parse(newTestFlagSet(cfg), argv, testAliases)
		if err != nil {
			t.Fatalf("argv %v: %v", argv, err)
		}
		if !help || args != nil {
			t.Fatalf("argv %v: args = %+v, help = %v", argv, args, help)
		}
	}
}

func TestDashDashIsNotPositional(t *testing.T) {
	_, args := parseTest(t, "--name=x", "--", "config")
	if len(args.Positionals) != 1 || args.Positionals[0] != "config" {
		t.Fatalf("positionals = %v", args.Positionals)
	}
}

func TestDashDashStopsOptionScanning(t *testing.T) {
	_, args := parseTest(t, "--name=x", "--", "--help")
	if len(args.Positionals) != 1 || args.Positionals[0] != "--help" {
		t.Fatalf("positionals = %v", args.Positionals)
	}
}

func TestValidationHelpers(t *testing.T) {
	_, args := parseTest(t, "--name=x", "--debug")

	if err := args.Only("name", "debug"); err != nil {
		t.Fatalf("Only() = %v", err)
	}
	if err := args.Only("name"); err == nil {
		t.Fatal("expected Only() to reject debug")
	}
	if _, err := args.ExactlyOne("name", "debug"); err == nil {
		t.Fatal("expected ExactlyOne() to reject two options")
	}
	if got, err := args.ExactlyOne("name", "missing"); err != nil || got != "name" {
		t.Fatalf("ExactlyOne() = %q, %v", got, err)
	}
	if err := args.AtMostOne("name", "debug"); err == nil {
		t.Fatal("expected AtMostOne() to reject two options")
	}
	if args.CountSet("name", "debug", "missing") != 2 {
		t.Fatalf("CountSet() = %d", args.CountSet("name", "debug", "missing"))
	}
}
