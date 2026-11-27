// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codis-fe builds its dashboard loader inside main(), so the startup contract
// is verified by re-executing this test binary as a real codis-fe process.
const (
	feStartupHelperEnv = "CODIS_FE_STARTUP_HELPER"
	feStartupArgsEnv   = "CODIS_FE_STARTUP_ARGS"
	feStartupHelperRun = "^TestCodisFEStartupHelperProcess$"
)

// TestCodisFEStartupHelperProcess is not a normal test: when the helper
// environment variable is set it rewrites os.Args and runs the real main().
func TestCodisFEStartupHelperProcess(t *testing.T) {
	if os.Getenv(feStartupHelperEnv) != "1" {
		t.Skip("codis-fe startup helper process")
	}
	os.Args = append([]string{"codis-fe"}, strings.Split(os.Getenv(feStartupArgsEnv), "\n")...)
	main()
}

// runCodisFE starts the helper process and returns its combined output and the
// error from Wait, which is non-nil for a non-zero exit status.
func runCodisFE(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+feStartupHelperRun, "-test.count=1")
	cmd.Env = append(os.Environ(),
		feStartupHelperEnv+"=1",
		feStartupArgsEnv+"="+strings.Join(argv, "\n"),
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("codis-fe %v did not finish: %v\n%s", argv, ctx.Err(), out)
	}
	return string(out), err
}

// feStartupAssets returns an assets directory holding the index.html that main
// requires before it builds the dashboard loader.
func feStartupAssets(t *testing.T) string {
	t.Helper()
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	return assets
}

// TestCodisFEExitsNonZeroWhenTheDynamicSourceIsInvalid pins that the dynamic
// etcd source is validated before serving: an invalid address or connection
// file exits non-zero with the reason, before any connection is attempted.
func TestCodisFEExitsNonZeroWhenTheDynamicSourceIsInvalid(t *testing.T) {
	assets := feStartupAssets(t)

	secretFile := filepath.Join(t.TempDir(), "etcd.toml")
	secretConfig := "endpoints = [\"127.0.0.1:2379\"]\nusername = \"fe\"\npassword = \"fe-super-secret\"\n"
	if err := os.WriteFile(secretFile, []byte(secretConfig), 0600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		argv  []string
		want  string
		leaks []string
	}{
		{
			name: "etcd endpoints without scheme",
			argv: []string{"--etcd=127.0.0.1:2379"},
			want: "invalid endpoints",
		},
		{
			name: "missing etcd config file",
			argv: []string{"--etcd-config=" + filepath.Join(t.TempDir(), "missing.toml")},
			want: "invalid etcd config file",
		},
		{
			name:  "etcd config file is never echoed",
			argv:  []string{"--etcd-config=" + secretFile},
			want:  "invalid endpoints",
			leaks: []string{"fe-super-secret"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := append([]string{
				"--assets-dir=" + assets,
				"--listen=127.0.0.1:0",
				"--log-level=ERROR",
			}, tt.argv...)
			out, err := runCodisFE(t, argv...)
			if err == nil {
				t.Fatalf("codis-fe %v exited successfully, want a failure\n%s", argv, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("codis-fe %v output does not contain %q:\n%s", argv, tt.want, out)
			}
			for _, leak := range tt.leaks {
				if strings.Contains(out, leak) {
					t.Fatalf("codis-fe %v output leaks %q:\n%s", argv, leak, out)
				}
			}
		})
	}
}

// TestCodisFEHelpMentionsBothSources pins the help output of the real binary:
// the removed backends are gone and both dashboard sources are documented.
func TestCodisFEHelpMentionsBothSources(t *testing.T) {
	out, err := runCodisFE(t, "--help")
	if err != nil {
		t.Fatalf("codis-fe --help failed: %v\n%s", err, out)
	}
	for _, legacy := range []string{"zookeeper", "filesystem", "consul"} {
		if strings.Contains(out, legacy) {
			t.Fatalf("--help still mentions %q:\n%s", legacy, out)
		}
	}
	for _, want := range []string{"--dashboard-list", "--etcd", "--etcd-config", "--listen"} {
		if !strings.Contains(out, want) {
			t.Fatalf("--help does not mention %q:\n%s", want, out)
		}
	}
}
