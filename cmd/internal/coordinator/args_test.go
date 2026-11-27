// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package coordinator

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	"github.com/CodisLabs/codis/pkg/models/etcd"
)

// parse runs argv through the shared CLI parser and the coordinator selection.
func parse(t *testing.T, argv ...string) (Args, bool, error) {
	t.Helper()
	var flags Flags
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bind(fs)
	if _, help, err := cli.Parse(fs, argv, nil); err != nil {
		t.Fatalf("cli.Parse(%v) failed: %v", argv, err)
	} else if help {
		t.Fatalf("cli.Parse(%v) requested help", argv)
	}
	return flags.Parse()
}

func writeEtcdConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "etcd.toml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write etcd config: %v", err)
	}
	return path
}

func TestParseSharedOptions(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want Args
		ok   bool
	}{
		{
			name: "etcd",
			argv: []string{"--etcd=http://127.0.0.1:2379"},
			want: Args{EndpointsFlag: "http://127.0.0.1:2379", HasEndpoints: true},
			ok:   true,
		},
		{
			name: "etcd list",
			argv: []string{"--etcd=http://a:2379,http://b:2379"},
			want: Args{EndpointsFlag: "http://a:2379,http://b:2379", HasEndpoints: true},
			ok:   true,
		},
		{
			name: "etcd config",
			argv: []string{"--etcd-config=/etc/codis/etcd.toml"},
			want: Args{ConfigFile: "/etc/codis/etcd.toml", HasConfigFile: true},
			ok:   true,
		},
		{
			name: "both",
			argv: []string{"--etcd=http://a:2379", "--etcd-config=/etc/codis/etcd.toml"},
			want: Args{
				EndpointsFlag: "http://a:2379",
				ConfigFile:    "/etc/codis/etcd.toml",
				HasEndpoints:  true,
				HasConfigFile: true,
			},
			ok: true,
		},
		{
			name: "none",
			want: Args{},
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := parse(t, tt.argv...)
			if err != nil {
				t.Fatalf("Parse() failed: %v", err)
			}
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("args = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestParseKeepsExplicitEmptyEtcd pins that --etcd= is remembered as given, so
// Build rejects it instead of silently falling back to the file or the default.
func TestParseKeepsExplicitEmptyEtcd(t *testing.T) {
	args, ok, err := parse(t, "--etcd=")
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if !ok || !args.HasEndpoints || args.EndpointsFlag != "" {
		t.Fatalf("args = %+v, ok = %v; want an explicitly given empty --etcd", args, ok)
	}
	if _, err := args.Build("http://fallback:2379"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Build() error = %v, want an empty address error", err)
	}
}

// TestParseRejectsDeprecatedOptions pins that every removed backend option is
// rejected by name and by the last supporting commit, without echoing values.
func TestParseRejectsDeprecatedOptions(t *testing.T) {
	names := []string{"zookeeper", "zookeeper-auth", "filesystem", "consul", "consul-auth"}
	for _, name := range names {
		for _, value := range []string{"user:pass-deprecated-secret", ""} {
			arg := "--" + name + "=" + value
			args, ok, err := parse(t, arg)
			if err == nil || ok {
				t.Fatalf("Parse(%q) = %+v, %v, %v; want a deprecated option error", arg, args, ok, err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "--"+name) {
				t.Fatalf("error %q does not name --%s", msg, name)
			}
			if !strings.Contains(msg, DeprecatedCoordinatorSHA) {
				t.Fatalf("error %q does not mention the last supporting commit", msg)
			}
			if !strings.Contains(msg, "--etcd or --etcd-config") {
				t.Fatalf("error %q does not point at the replacement options", msg)
			}
			if value != "" && strings.Contains(msg, value) {
				t.Fatalf("error %q leaked the option value", msg)
			}
			if args != (Args{}) {
				t.Fatalf("args = %+v, want the zero value", args)
			}
		}
	}
}

// TestParseRejectsDeprecatedAlongsideEtcd pins that a deprecated option is
// never silently ignored when a supported option is given as well.
func TestParseRejectsDeprecatedAlongsideEtcd(t *testing.T) {
	args, ok, err := parse(t, "--etcd=http://127.0.0.1:2379", "--consul=127.0.0.1:8500")
	if err == nil || ok {
		t.Fatalf("Parse() = %+v, %v, %v; want a deprecated option error", args, ok, err)
	}
	if !strings.Contains(err.Error(), "--consul") {
		t.Fatalf("error %q does not name --consul", err)
	}
}

func TestFlagNames(t *testing.T) {
	want := []string{"etcd", "etcd-config"}
	got := FlagNames()
	if len(got) != len(want) {
		t.Fatalf("FlagNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FlagNames() = %v, want %v", got, want)
		}
	}
}

// TestBindRegistersFlagNames pins that FlagNames matches the supported options
// registered by Bind and that the removed ones stay registered for their error.
func TestBindRegistersFlagNames(t *testing.T) {
	var flags Flags
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bind(fs)

	registered := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) { registered[f.Name] = true })
	for _, name := range FlagNames() {
		if !registered[name] {
			t.Fatalf("Bind() did not register --%s", name)
		}
	}
	for _, name := range []string{"zookeeper", "zookeeper-auth", "filesystem", "consul", "consul-auth"} {
		if !registered[name] {
			t.Fatalf("Bind() did not register the deprecated option --%s", name)
		}
	}
	if registered["etcd-auth"] {
		t.Fatal("Bind() registered --etcd-auth; credentials belong in --etcd-config")
	}
}

func TestBuildUsesConfigFile(t *testing.T) {
	path := writeEtcdConfig(t, `
endpoints = ["http://file-a:2379", "http://file-b:2379"]
dial_timeout = "3s"
request_timeout = "7s"
username = "dashboard"
password = "file-secret"
`)
	args := Args{ConfigFile: path, HasConfigFile: true}

	cfg, err := args.Build("http://fallback:2379")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if got, want := strings.Join(cfg.Endpoints, ","), "http://file-a:2379,http://file-b:2379"; got != want {
		t.Fatalf("endpoints = %s, want %s", got, want)
	}
	if cfg.DialTimeout.String() != "3s" || cfg.RequestTimeout.String() != "7s" {
		t.Fatalf("timeouts = %s/%s, want 3s/7s", cfg.DialTimeout, cfg.RequestTimeout)
	}
	if cfg.Username != "dashboard" || cfg.Password.Value() != "file-secret" {
		t.Fatalf("credentials were not taken from the connection file")
	}
}

// TestBuildEtcdFlagOverridesConfigFile pins the precedence: --etcd wins over
// the file endpoints while the remaining fields still come from the file.
func TestBuildEtcdFlagOverridesConfigFile(t *testing.T) {
	path := writeEtcdConfig(t, `
endpoints = ["http://file-a:2379"]
request_timeout = "7s"
username = "dashboard"
password = "file-secret"
`)
	args := Args{
		EndpointsFlag: "http://cli:2379",
		HasEndpoints:  true,
		ConfigFile:    path,
		HasConfigFile: true,
	}

	cfg, err := args.Build("http://fallback:2379")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if got, want := strings.Join(cfg.Endpoints, ","), "http://cli:2379"; got != want {
		t.Fatalf("endpoints = %s, want %s", got, want)
	}
	if cfg.RequestTimeout.String() != "7s" || cfg.Username != "dashboard" {
		t.Fatalf("connection fields did not come from the file: %s", cfg)
	}
}

func TestBuildFallsBackToCoordinatorAddr(t *testing.T) {
	args := Args{}
	cfg, err := args.Build(" http://fb-a:2379 , http://fb-b:2379 ")
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	if got, want := strings.Join(cfg.Endpoints, ","), "http://fb-a:2379,http://fb-b:2379"; got != want {
		t.Fatalf("endpoints = %s, want %s", got, want)
	}
}

func TestBuildUsesDefaultEndpoint(t *testing.T) {
	for _, fallback := range []string{"", "   "} {
		cfg, err := Args{}.Build(fallback)
		if err != nil {
			t.Fatalf("Build(%q) failed: %v", fallback, err)
		}
		if len(cfg.Endpoints) != 1 || cfg.Endpoints[0] != etcd.DefaultEndpoint {
			t.Fatalf("Build(%q) endpoints = %v, want %s", fallback, cfg.Endpoints, etcd.DefaultEndpoint)
		}
		if cfg.DialTimeout != etcd.DefaultDialTimeout || cfg.RequestTimeout != etcd.DefaultRequestTimeout {
			t.Fatalf("Build(%q) timeouts = %s/%s, want the defaults", fallback, cfg.DialTimeout, cfg.RequestTimeout)
		}
	}
}

// TestBuildRejectsBadAddresses pins that a malformed address is an error in
// every slot, without scheme inference and without echoing credentials.
func TestBuildRejectsBadAddresses(t *testing.T) {
	tests := []struct {
		name         string
		args         Args
		fallback     string
		wantContains string
		secret       string
	}{
		{
			name:         "etcd without scheme",
			args:         Args{EndpointsFlag: "127.0.0.1:2379", HasEndpoints: true},
			wantContains: "invalid endpoints",
			secret:       "127.0.0.1:2379",
		},
		{
			name:         "etcd with credentials",
			args:         Args{EndpointsFlag: "http://user:pass-secret@a:2379", HasEndpoints: true},
			wantContains: "credentials",
			secret:       "pass-secret",
		},
		{
			name:         "etcd with path",
			args:         Args{EndpointsFlag: "http://a:2379/codis3", HasEndpoints: true},
			wantContains: "path",
		},
		{
			name:         "etcd mixed schemes",
			args:         Args{EndpointsFlag: "http://a:2379,https://b:2379", HasEndpoints: true},
			wantContains: "mix",
		},
		{
			name:         "etcd empty entry",
			args:         Args{EndpointsFlag: "http://a:2379,", HasEndpoints: true},
			wantContains: "empty",
		},
		{
			name:         "fallback without scheme",
			fallback:     "127.0.0.1:2379",
			wantContains: "invalid endpoints",
			secret:       "127.0.0.1:2379",
		},
		{
			name:         "fallback empty entry",
			fallback:     "http://a:2379, ,http://b:2379",
			wantContains: "empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.args.Build(tt.fallback)
			if err == nil {
				t.Fatal("Build() succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want it to mention %q", err, tt.wantContains)
			}
			if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("error %q leaked the credential", err)
			}
		})
	}
}

// TestBuildRejectsBadConfigFiles pins that an unusable connection file is
// reported before any connection is attempted.
func TestBuildRejectsBadConfigFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	tests := []struct {
		name         string
		args         Args
		wantContains string
		secret       string
	}{
		{
			name:         "missing file",
			args:         Args{ConfigFile: missing, HasConfigFile: true},
			wantContains: "not readable",
		},
		{
			name:         "empty file path",
			args:         Args{HasConfigFile: true},
			wantContains: "empty file path",
		},
		{
			name: "unknown field",
			args: Args{
				ConfigFile:    writeEtcdConfig(t, "endpoints = [\"http://a:2379\"]\nnot_a_connection_option = \"x\"\n"),
				HasConfigFile: true,
			},
			wantContains: "unknown field",
		},
		{
			name: "unreadable tls file",
			args: Args{
				ConfigFile:    writeEtcdConfig(t, "endpoints = [\"https://a:2379\"]\nca_file = \"missing-ca.pem\"\n"),
				HasConfigFile: true,
			},
			wantContains: "ca_file",
		},
		{
			name: "tls files on http endpoints",
			args: Args{
				ConfigFile:    writeEtcdConfig(t, "endpoints = [\"http://a:2379\"]\nca_file = \"ca.pem\"\n"),
				HasConfigFile: true,
			},
			wantContains: "requires https",
		},
		{
			name: "malformed toml",
			args: Args{
				ConfigFile:    writeEtcdConfig(t, "endpoints = [\"http://a:2379\"]\npassword = \"toml-secret-value\n"),
				HasConfigFile: true,
			},
			wantContains: "malformed toml",
			secret:       "toml-secret-value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.args.Build("http://fallback:2379")
			if err == nil {
				t.Fatal("Build() succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want it to mention %q", err, tt.wantContains)
			}
			if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("error %q leaked the file content", err)
			}
		})
	}
}

// TestNewClientReturnsConfigErrors pins that NewClient reports configuration
// problems instead of connecting.
func TestNewClientReturnsConfigErrors(t *testing.T) {
	args := Args{ConfigFile: filepath.Join(t.TempDir(), "missing.toml"), HasConfigFile: true}
	if client, err := args.NewClient("http://127.0.0.1:2379"); err == nil {
		client.Close()
		t.Fatal("NewClient() succeeded, want an error")
	}
}

func TestNewClientUsesDefaultEndpoint(t *testing.T) {
	client, err := Args{}.NewClient("")
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
}
