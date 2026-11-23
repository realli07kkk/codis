// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"strings"
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

// parseShakeUsage drives the typed parser, mirroring main() except that argv is
// explicit.
func parseShakeUsage(t *testing.T, argv ...string) *shakeConfig {
	t.Helper()
	cfg, help, err := parseShakeArgs(argv)
	if err != nil {
		t.Fatalf("parseShakeArgs(%v) failed: %v", argv, err)
	}
	if help {
		t.Fatalf("parseShakeArgs(%v) unexpectedly asked for help", argv)
	}
	return cfg
}

// parseShakeUsageError parses an argv that is expected to be rejected.
func parseShakeUsageError(t *testing.T, argv ...string) error {
	t.Helper()
	_, help, err := parseShakeArgs(argv)
	if help {
		t.Fatalf("parseShakeArgs(%v) unexpectedly asked for help", argv)
	}
	return err
}

func shakeMustBool(t *testing.T, got cli.Bool, flag string, want bool) {
	t.Helper()
	if got.Value != want {
		t.Fatalf("%s = %v, want %v", flag, got.Value, want)
	}
	if got.Provided != want {
		t.Fatalf("%s: provided = %v, want %v", flag, got.Provided, want)
	}
}

func shakeMustString(t *testing.T, got cli.String, flag string, want string) {
	t.Helper()
	if !got.Provided {
		t.Fatalf("%s: want string(%q), got not provided", flag, want)
	}
	if got.Value != want {
		t.Fatalf("%s = %q, want %q", flag, got.Value, want)
	}
}

func shakeMustUnset(t *testing.T, got cli.String, flag string) {
	t.Helper()
	if got.Provided {
		t.Fatalf("%s = %#v, want not provided", flag, got.Value)
	}
}

// TestShakeHelp pins that both help spellings are answered without a config.
func TestShakeHelp(t *testing.T) {
	for _, argv := range [][]string{{"--help"}, {"-h"}} {
		cfg, help, err := parseShakeArgs(argv)
		if err != nil {
			t.Fatalf("parseShakeArgs(%v) failed: %v", argv, err)
		}
		if !help || cfg != nil {
			t.Fatalf("parseShakeArgs(%v): cfg = %+v, help = %v; want nil, true", argv, cfg, help)
		}
	}
}

// TestShakeVersionFlagIsBool pins that --version is a bare boolean, is the only
// option of the version form, and stays unset in the run form.
func TestShakeVersionFlagIsBool(t *testing.T) {
	cfg := parseShakeUsage(t, "--version")
	shakeMustBool(t, cfg.Version, "--version", true)
	shakeMustUnset(t, cfg.Spec, "--spec")

	cfg = parseShakeUsage(t, "--spec=/tmp/job/spec.json")
	shakeMustBool(t, cfg.Version, "--version", false)
	shakeMustString(t, cfg.Spec, "--spec", "/tmp/job/spec.json")
}

// TestShakeVersionIsExclusiveWithSpec pins that the version form rejects the
// run form, i.e. parsed.Only("version") in parseShakeArgs.
func TestShakeVersionIsExclusiveWithSpec(t *testing.T) {
	err := parseShakeUsageError(t, "--version", "--spec=/tmp/job/spec.json")
	if err == nil {
		t.Fatalf("argv [--version --spec=/tmp/job/spec.json]: expected parse error")
	}
	if !strings.Contains(err.Error(), "--spec") {
		t.Fatalf("err = %v, want it to mention --spec", err)
	}
}

// TestShakeSpecFlagBindsValue covers both long option spellings.
func TestShakeSpecFlagBindsValue(t *testing.T) {
	tests := [][]string{
		{"--spec=/tmp/job/spec.json"},
		{"--spec", "/tmp/job/spec.json"},
	}
	for _, argv := range tests {
		cfg := parseShakeUsage(t, argv...)
		shakeMustString(t, cfg.Spec, "--spec", "/tmp/job/spec.json")
		shakeMustBool(t, cfg.Version, "--version", false)
	}
}

// TestShakeUsageErrors pins the required --spec plus the rejected argv shapes:
// a stray positional, an empty value (cli.String.Set refuses it) and an
// undefined option.
func TestShakeUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"missing spec", nil, "--spec is required"},
		{"stray positional", []string{"--spec=/tmp/job/spec.json", "extra"}, "unexpected argument"},
		{"empty spec", []string{"--spec="}, "requires a non-empty argument"},
		{"version with a value", []string{"--version=true"}, "must not have an argument"},
		{"unknown option", []string{"--bogus=1"}, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseShakeUsageError(t, tt.argv...)
			if err == nil {
				t.Fatalf("argv %v: expected parse error", tt.argv)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("argv %v: err = %v, want it to contain %q", tt.argv, err, tt.want)
			}
		})
	}
}
