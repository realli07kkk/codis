// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deprecatedJodisCommit = "c635ade0424a1448fb251f32b88d64ca7cf9a555"

func writeProxyConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proxy.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write proxy config: %v", err)
	}
	return path
}

// TestLoadFromFileRejectsJodisOptions pins that every removed jodis_* option
// is rejected while loading the proxy config, that even an empty value is
// detected, and that the error names the option and the last supporting commit
// without echoing any value.
func TestLoadFromFileRejectsJodisOptions(t *testing.T) {
	tests := []struct {
		name string
		line string
		// value is a configured value that must never appear in the error.
		value string
	}{
		{name: "name set", line: `jodis_name = "zookeeper"`, value: ""},
		{name: "name empty", line: `jodis_name = ""`},
		{name: "addr empty", line: `jodis_addr = ""`},
		{name: "auth secret", line: `jodis_auth = "jodis-secret-value"`, value: "jodis-secret-value"},
		{name: "timeout empty", line: `jodis_timeout = "0s"`},
		{name: "compatible false", line: `jodis_compatible = false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := strings.SplitN(tt.line, "=", 2)[0]
			option = strings.TrimSpace(option)
			path := writeProxyConfig(t, DefaultConfig+tt.line+"\n")
			c := NewDefaultConfig()
			err := c.LoadFromFile(path)
			if err == nil {
				t.Fatalf("LoadFromFile() accepted %s", option)
			}
			msg := err.Error()
			if !strings.Contains(msg, option) {
				t.Fatalf("error %q does not name the option %q", msg, option)
			}
			if !strings.Contains(msg, deprecatedJodisCommit) {
				t.Fatalf("error %q does not mention the last supporting commit", msg)
			}
			if !strings.Contains(msg, "etcd_register_online") {
				t.Fatalf("error %q does not point at the replacement option", msg)
			}
			if tt.value != "" && strings.Contains(msg, tt.value) {
				t.Fatalf("error %q echoes the configured value", msg)
			}
		})
	}
}

// TestLoadFromFileAcceptsConfigWithoutJodis pins that the shipped default
// proxy config still loads, so the rejection above only fires on the removed
// options.
func TestLoadFromFileAcceptsConfigWithoutJodis(t *testing.T) {
	path := writeProxyConfig(t, DefaultConfig)
	c := NewDefaultConfig()
	if err := c.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile() failed on the default config: %v", err)
	}
}
