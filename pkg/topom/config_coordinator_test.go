// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deprecatedCoordinatorCommit = "c635ade0424a1448fb251f32b88d64ca7cf9a555"

func writeDashboardConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dashboard.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write dashboard config: %v", err)
	}
	return path
}

// TestDefaultConfigUsesEtcdCoordinator pins that the shipped default parses,
// validates and selects etcd with the documented default endpoint.
func TestDefaultConfigUsesEtcdCoordinator(t *testing.T) {
	c := NewDefaultConfig()
	if c.CoordinatorName != CoordinatorNameEtcd {
		t.Fatalf("coordinator_name = %q, want %q", c.CoordinatorName, CoordinatorNameEtcd)
	}
	if c.CoordinatorAddr != "http://127.0.0.1:2379" {
		t.Fatalf("coordinator_addr = %q", c.CoordinatorAddr)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
	for _, gone := range []string{
		`coordinator_name = "zookeeper"`,
		`coordinator_name = "filesystem"`,
		`coordinator_name = "consul"`,
		"coordinator_auth =",
	} {
		if strings.Contains(DefaultConfig, gone) {
			t.Fatalf("default config still mentions %q", gone)
		}
	}
}

func TestConfigRejectsOldCoordinatorNames(t *testing.T) {
	for _, name := range []string{"zookeeper", "filesystem", "consul", "redis", ""} {
		c := NewDefaultConfig()
		c.CoordinatorName = name
		err := c.Validate()
		if err == nil {
			t.Fatalf("Validate() accepted coordinator_name %q", name)
		}
		msg := err.Error()
		if !strings.Contains(msg, "coordinator_name") {
			t.Fatalf("error %q does not name the field", msg)
		}
		if !strings.Contains(msg, deprecatedCoordinatorCommit) {
			t.Fatalf("error %q does not mention the last supporting commit", msg)
		}
		if name == "redis" && strings.Contains(msg, name) {
			t.Fatalf("error %q echoes the configured value", msg)
		}
	}
}

// TestConfigRejectsCoordinatorAuth pins that the removed credential field is
// rejected whether or not it carries a value, and that the value never leaks.
func TestConfigRejectsCoordinatorAuth(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty value", DefaultConfig + "coordinator_auth = \"\"\n"},
		{"configured value", DefaultConfig + "coordinator_auth = \"coordinator-secret-value\"\n"},
		{"blank value", DefaultConfig + "coordinator_auth = \"   \"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeDashboardConfig(t, tt.body)
			c := NewDefaultConfig()
			err := c.LoadFromFile(path)
			if err == nil {
				t.Fatal("LoadFromFile() accepted coordinator_auth")
			}
			msg := err.Error()
			if !strings.Contains(msg, "coordinator_auth") {
				t.Fatalf("error %q does not name the field", msg)
			}
			if !strings.Contains(msg, deprecatedCoordinatorCommit) {
				t.Fatalf("error %q does not mention the last supporting commit", msg)
			}
			if !strings.Contains(msg, "--etcd-config") {
				t.Fatalf("error %q does not point at the connection file", msg)
			}
			if strings.Contains(msg, "coordinator-secret-value") {
				t.Fatalf("error %q echoes the credential", msg)
			}
		})
	}
}

// TestConfigValidatesCoordinatorAddr covers the endpoint rules applied to the
// dashboard coordinator_addr.
func TestConfigValidatesCoordinatorAddr(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantErr  bool
		noEcho   string
		mustName bool
	}{
		{name: "single", addr: "http://127.0.0.1:2379"},
		{name: "multiple", addr: "http://a:2379,http://b:2379"},
		{name: "multiple with spaces", addr: "https://a:2379, https://b:2379"},
		{name: "trailing slash", addr: "http://a:2379/"},
		{name: "empty", addr: "", wantErr: true, mustName: true},
		{name: "blank", addr: "   ", wantErr: true, mustName: true},
		{name: "filesystem path", addr: "/tmp/codis", wantErr: true, noEcho: "/tmp/codis", mustName: true},
		{name: "missing scheme", addr: "127.0.0.1:2379", wantErr: true, noEcho: "127.0.0.1:2379", mustName: true},
		{name: "path", addr: "http://a:2379/codis3", wantErr: true, noEcho: "codis3", mustName: true},
		{name: "credentials", addr: "http://user:pass-secret@a:2379", wantErr: true, noEcho: "pass-secret", mustName: true},
		{name: "mixed schemes", addr: "http://a:2379,https://b:2379", wantErr: true, mustName: true},
		{name: "empty entry", addr: "http://a:2379,,http://b:2379", wantErr: true, mustName: true},
		{name: "query", addr: "http://a:2379?x=1", wantErr: true, mustName: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewDefaultConfig()
			c.CoordinatorAddr = tt.addr
			err := c.Validate()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Validate() failed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() accepted coordinator_addr %q", tt.addr)
			}
			msg := err.Error()
			if tt.mustName && !strings.Contains(msg, "coordinator_addr") {
				t.Fatalf("error %q does not name the field", msg)
			}
			if tt.noEcho != "" && strings.Contains(msg, tt.noEcho) {
				t.Fatalf("error %q echoes the configured value", msg)
			}
		})
	}
}

// TestLoadFromFileRejectsOldCoordinators pins that an old backend selection or
// an old filesystem path is rejected while loading the dashboard config.
func TestLoadFromFileRejectsOldCoordinators(t *testing.T) {
	oldName := strings.Replace(DefaultConfig, `coordinator_name = "etcd"`, `coordinator_name = "zookeeper"`, 1)
	oldAddr := strings.Replace(DefaultConfig, `coordinator_addr = "http://127.0.0.1:2379"`, `coordinator_addr = "/tmp/codis"`, 1)
	for name, body := range map[string]string{"old name": oldName, "old address": oldAddr} {
		t.Run(name, func(t *testing.T) {
			path := writeDashboardConfig(t, body)
			c := NewDefaultConfig()
			if err := c.LoadFromFile(path); err == nil {
				t.Fatal("LoadFromFile() accepted an old coordinator configuration")
			}
		})
	}
}

func TestLoadFromFileAcceptsMultipleEndpoints(t *testing.T) {
	body := strings.Replace(DefaultConfig,
		`coordinator_addr = "http://127.0.0.1:2379"`,
		`coordinator_addr = "http://a:2379,http://b:2379"`, 1)
	path := writeDashboardConfig(t, body)
	c := NewDefaultConfig()
	if err := c.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile() failed: %v", err)
	}
	if c.CoordinatorAddr != "http://a:2379,http://b:2379" {
		t.Fatalf("coordinator_addr = %q", c.CoordinatorAddr)
	}
}

func TestLoadFromFileErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		c := NewDefaultConfig()
		if err := c.LoadFromFile(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
			t.Fatal("LoadFromFile() accepted a missing file")
		}
	})

	t.Run("malformed toml does not echo the line", func(t *testing.T) {
		path := writeDashboardConfig(t, DefaultConfig+"product_auth = \"leaked-secret-without-quote\n")
		c := NewDefaultConfig()
		err := c.LoadFromFile(path)
		if err == nil {
			t.Fatal("LoadFromFile() accepted malformed toml")
		}
		msg := err.Error()
		if strings.Contains(msg, "leaked-secret-without-quote") {
			t.Fatalf("error %q echoes the file content", msg)
		}
		if !strings.Contains(msg, "line") {
			t.Fatalf("error %q does not report a position", msg)
		}
	})
}
