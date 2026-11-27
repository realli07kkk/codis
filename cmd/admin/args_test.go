// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"strings"
	"testing"

	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
)

// TestAdminShowLockRequiresProductAndBackend pins the two options --show-lock
// needs: a product and one explicit shared connection option.
func TestAdminShowLockRequiresProductAndBackend(t *testing.T) {
	cfg := parseAdminUsage(t, "--show-lock", "--product=demo", "--etcd-config=etcd.toml")
	if cfg.Command != "show-lock" {
		t.Fatalf("command = %q, want show-lock", cfg.Command)
	}
	adminMustEtcdConfig(t, cfg, "etcd.toml")

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"no product", []string{"--show-lock", "--etcd=http://127.0.0.1:2379"}, "option --product is required"},
		{"no backend", []string{"--show-lock", "--product=demo"}, "at least one of --etcd, --etcd-config is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseAdminUsageError(t, tt.argv...)
			if err == nil {
				t.Fatalf("argv %v: expected parse error", tt.argv)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("argv %v: error = %v, want %q", tt.argv, err, tt.want)
			}
		})
	}
}

// TestAdminRemoveLockRequiresRevisionConfirmAndProduct pins that the operator
// recovery command refuses to run without all three options.
func TestAdminRemoveLockRequiresRevisionConfirmAndProduct(t *testing.T) {
	cfg := parseAdminUsage(t, "--remove-lock", "--product=demo",
		"--expected-revision=417", "--confirm", "--etcd-config=etcd.toml")
	if cfg.Command != "remove-lock" {
		t.Fatalf("command = %q, want remove-lock", cfg.Command)
	}
	if !cfg.ExpectedRevision.Provided || cfg.ExpectedRevision.Value != 417 {
		t.Fatalf("expected revision = %d (provided %v), want 417", cfg.ExpectedRevision.Value, cfg.ExpectedRevision.Provided)
	}
	adminMustBool(t, adminBoolArg(cfg, "--confirm"), "--confirm", true)

	const wantAll = "option --remove-lock requires --product, --expected-revision and --confirm"
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"no revision", []string{"--remove-lock", "--product=demo", "--confirm", "--etcd=http://127.0.0.1:2379"}, wantAll},
		{"no confirm", []string{"--remove-lock", "--product=demo", "--expected-revision=417", "--etcd=http://127.0.0.1:2379"}, wantAll},
		{"no product", []string{"--remove-lock", "--expected-revision=417", "--confirm", "--etcd=http://127.0.0.1:2379"}, wantAll},
		{"zero revision", []string{"--remove-lock", "--product=demo", "--expected-revision=0", "--confirm", "--etcd=http://127.0.0.1:2379"}, "invalid --expected-revision: must be a positive integer"},
		{"negative revision", []string{"--remove-lock", "--product=demo", "--expected-revision=-417", "--confirm", "--etcd=http://127.0.0.1:2379"}, "invalid --expected-revision: must be a positive integer"},
		{"no backend", []string{"--remove-lock", "--product=demo", "--expected-revision=417", "--confirm"}, "at least one of --etcd, --etcd-config is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseAdminUsageError(t, tt.argv...)
			if err == nil {
				t.Fatalf("argv %v: expected parse error", tt.argv)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("argv %v: error = %v, want %q", tt.argv, err, tt.want)
			}
		})
	}
}

// TestAdminConfigDumpV1IsGone pins that the removed zookeeper tree dump is no
// longer accepted.
func TestAdminConfigDumpV1IsGone(t *testing.T) {
	err := parseAdminUsageError(t, "--config-dump", "--product=p",
		"--etcd=http://127.0.0.1:2379", "-1")
	if err == nil {
		t.Fatal("-1 is still accepted by --config-dump")
	}
	if strings.Contains(adminUsage, "-1]") {
		t.Fatal("usage still documents --config-dump -1")
	}
	if strings.Contains(adminUsage, "/zk/codis") {
		t.Fatal("usage still documents the zookeeper tree")
	}
}

// TestAdminConfigConvertNeedsNoBackend pins that the file-only converter keeps
// working without any connection option.
func TestAdminConfigConvertNeedsNoBackend(t *testing.T) {
	cfg := parseAdminUsage(t, "--config-convert=/tmp/config.json")
	if cfg.Command != "config-convert" {
		t.Fatalf("command = %q, want config-convert", cfg.Command)
	}
	if cfg.CoordinatorArg.HasEndpoints || cfg.CoordinatorArg.HasConfigFile {
		t.Fatal("config-convert recorded a connection option")
	}
}

// TestAdminRemovedBackendsNameTheCommit pins that the removed coordinator
// options fail with the last supporting commit and never echo their values.
func TestAdminRemovedBackendsNameTheCommit(t *testing.T) {
	tests := [][]string{
		{"--show-lock", "--product=p", "--zookeeper=zk-host:2181"},
		{"--config-dump", "--product=p", "--filesystem=/tmp/codis"},
		{"--dashboard-list", "--consul=consul-host:8500"},
		{"--show-lock", "--product=p", "--zookeeper=zk-host:2181", "--zookeeper-auth=user:zk-secret"},
		{"--dashboard-list", "--consul=consul-host:8500", "--consul-auth=consul-secret"},
	}
	for _, argv := range tests {
		err := parseAdminUsageError(t, argv...)
		if err == nil {
			t.Fatalf("argv %v: expected parse error", argv)
		}
		if !strings.Contains(err.Error(), coordarg.DeprecatedCoordinatorSHA) {
			t.Fatalf("argv %v: error %q does not name the deprecated commit", argv, err)
		}
		for _, secret := range []string{"zk-host:2181", "consul-host:8500", "zk-secret", "consul-secret"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("argv %v: error echoes the value: %q", argv, err)
			}
		}
	}
}

// TestAdminUsageDocumentsTheRecoveryFlow pins the two documented operator
// commands and that no removed flag or credential placeholder remains.
func TestAdminUsageDocumentsTheRecoveryFlow(t *testing.T) {
	for _, line := range []string{
		"codis-admin --etcd-config=etcd.toml --product=demo --show-lock",
		"codis-admin --etcd-config=etcd.toml --product=demo --remove-lock --expected-revision=417 --confirm",
	} {
		if !strings.Contains(adminUsage, line) {
			t.Fatalf("usage does not document %q", line)
		}
	}
	for _, removed := range []string{"--zookeeper", "--filesystem", "--consul", "--zookeeper-auth", "--consul-auth"} {
		if strings.Contains(adminUsage, removed) {
			t.Fatalf("usage still documents %s", removed)
		}
	}
	for _, secret := range []string{"USR:PWD", "password", "PASSWORD"} {
		if strings.Contains(adminUsage, secret) {
			t.Fatalf("usage contains a credential placeholder %q", secret)
		}
	}
}
