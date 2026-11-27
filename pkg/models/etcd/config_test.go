// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "etcd.toml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return p
}

func TestLoadConfigFileDefaults(t *testing.T) {
	p := writeConfigFile(t, `endpoints = ["http://127.0.0.1:2379"]
`)
	c, err := LoadConfigFile(p)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if c.DialTimeout != DefaultDialTimeout || c.RequestTimeout != DefaultRequestTimeout {
		t.Fatalf("timeouts = %s/%s, want %s/%s", c.DialTimeout, c.RequestTimeout, DefaultDialTimeout, DefaultRequestTimeout)
	}
	if len(c.Endpoints) != 1 || c.Endpoints[0] != "http://127.0.0.1:2379" {
		t.Fatalf("endpoints = %v", c.Endpoints)
	}
	if c.Username != "" || c.Password != "" {
		t.Fatalf("credentials = %q/%q, want empty", c.Username, c.Password)
	}
}

func TestLoadConfigFileExplicitValues(t *testing.T) {
	p := writeConfigFile(t, `endpoints = ["http://127.0.0.1:2379/", " http://127.0.0.1:2380 "]
dial_timeout = "1s"
request_timeout = "250ms"
username = "root"
password = "s3cr3t"
`)
	c, err := LoadConfigFile(p)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if c.DialTimeout != time.Second || c.RequestTimeout != 250*time.Millisecond {
		t.Fatalf("timeouts = %s/%s", c.DialTimeout, c.RequestTimeout)
	}
	want := []string{"http://127.0.0.1:2379", "http://127.0.0.1:2380"}
	if !equalStrings(c.Endpoints, want) {
		t.Fatalf("endpoints = %v, want %v", c.Endpoints, want)
	}
	if c.Username != "root" || c.Password.Value() != "s3cr3t" {
		t.Fatalf("credentials = %q/%q", c.Username, c.Password.Value())
	}
	if c.Password.String() != redactedSecret {
		t.Fatalf("password renders as %q, want redacted", c.Password.String())
	}
}

func TestLoadConfigFileTLSRelativePath(t *testing.T) {
	pki := newTestPKI(t)
	dir := t.TempDir()
	blob, err := os.ReadFile(pki.caFile)
	if err != nil {
		t.Fatalf("read ca: %v", err)
	}
	writeFile(t, dir, "ca.pem", blob)
	p := writeFile(t, dir, "etcd.toml", []byte(`endpoints = ["https://127.0.0.1:2379"]
ca_file = "ca.pem"
`))
	c, err := LoadConfigFile(p)
	if err != nil {
		t.Fatalf("load config with relative ca_file: %v", err)
	}
	if _, err := c.TLSConfig(); err != nil {
		t.Fatalf("tls config: %v", err)
	}
}

func TestLoadConfigFileRelativePathIsUsed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ca.pem", []byte("not a certificate"))
	p := writeFile(t, dir, "etcd.toml", []byte(`endpoints = ["https://127.0.0.1:2379"]
ca_file = "ca.pem"
`))
	_, err := LoadConfigFile(p)
	if err == nil {
		t.Fatal("expected error for bad relative ca_file")
	}
	// "no PEM certificate found" proves the relative path was resolved and read.
	if !strings.Contains(err.Error(), "no PEM certificate found") {
		t.Fatalf("error = %v, want no PEM certificate found", err)
	}
}

func TestLoadConfigFileInvalid(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "empty endpoint",
			content: `endpoints = ["", "http://127.0.0.1:2379"]`,
			wantErr: "invalid endpoints: empty entry",
		},
		{
			name:    "embedded credentials",
			content: `endpoints = ["http://user:pass@127.0.0.1:2379"]`,
			wantErr: "credentials are not allowed",
		},
		{
			name:    "path without scheme",
			content: `endpoints = ["/tmp/codis"]`,
			wantErr: "scheme must be http or https",
		},
		{
			name:    "bare host",
			content: `endpoints = ["127.0.0.1:2379"]`,
			wantErr: "invalid endpoints",
		},
		{
			name:    "mixed scheme",
			content: `endpoints = ["http://127.0.0.1:2379", "https://127.0.0.1:2380"]`,
			wantErr: "cannot mix http and https",
		},
		{
			name:    "path in url",
			content: `endpoints = ["https://127.0.0.1:2379/foo"]`,
			wantErr: "path is not allowed",
		},
		{
			name: "tls files with http endpoint",
			content: `endpoints = ["http://127.0.0.1:2379"]
ca_file = "ca.pem"`,
			wantErr: "tls config requires https endpoints",
		},
		{
			name: "missing ca file",
			content: `endpoints = ["https://127.0.0.1:2379"]
ca_file = "missing.pem"`,
			wantErr: "invalid ca_file: file is not readable",
		},
		{
			name: "dial timeout zero",
			content: `endpoints = ["http://127.0.0.1:2379"]
dial_timeout = "0s"`,
			wantErr: "invalid dial_timeout: must be greater than zero",
		},
		{
			name: "dial timeout negative",
			content: `endpoints = ["http://127.0.0.1:2379"]
dial_timeout = "-1s"`,
			wantErr: "invalid dial_timeout: must be greater than zero",
		},
		{
			name: "dial timeout not a duration",
			content: `endpoints = ["http://127.0.0.1:2379"]
dial_timeout = "5"`,
			wantErr: "invalid dial_timeout: not a duration",
		},
		{
			name: "request timeout zero",
			content: `endpoints = ["http://127.0.0.1:2379"]
request_timeout = "0s"`,
			wantErr: "invalid request_timeout: must be greater than zero",
		},
		{
			name: "username without password",
			content: `endpoints = ["http://127.0.0.1:2379"]
username = "root"`,
			wantErr: "invalid username/password: must be set together",
		},
		{
			name: "password without username",
			content: `endpoints = ["http://127.0.0.1:2379"]
password = "pw"`,
			wantErr: "invalid username/password: must be set together",
		},
		{
			name: "cert without key",
			content: `endpoints = ["https://127.0.0.1:2379"]
ca_file = "ca.pem"
cert_file = "client.pem"`,
			wantErr: "invalid cert_file/key_file: must be set together",
		},
		{
			name: "key without cert",
			content: `endpoints = ["https://127.0.0.1:2379"]
ca_file = "ca.pem"
key_file = "client-key.pem"`,
			wantErr: "invalid cert_file/key_file: must be set together",
		},
		{
			name: "unknown field",
			content: `endpoints = ["http://127.0.0.1:2379"]
unexpected_field = "boom"`,
			wantErr: "unknown field",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			// Keep a readable ca.pem around so error cases fail for their
			// intended reason, not for a missing file.
			writeFile(t, dir, "ca.pem", []byte("not a certificate"))
			p := writeFile(t, dir, "etcd.toml", []byte(c.content))
			_, err := LoadConfigFile(p)
			if err == nil {
				t.Fatalf("expected error containing %q", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

func TestConfigRedaction(t *testing.T) {
	const password = "s3cr3t-do-not-print"
	c := &Config{
		Endpoints:      []string{"http://127.0.0.1:2379"},
		DialTimeout:    time.Second,
		RequestTimeout: time.Second,
		Username:       "root",
		Password:       Secret(password),
	}
	renders := []string{
		c.String(),
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%+v", c),
		fmt.Sprintf("%#v", c),
		fmt.Sprintf("%v", *c),
		fmt.Sprintf("%+v", *c),
		fmt.Sprintf("%#v", *c),
		fmt.Sprintf("%v", c.Password),
		fmt.Sprintf("%+v", c.Password),
		fmt.Sprintf("%#v", c.Password),
	}
	for _, s := range renders {
		if strings.Contains(s, password) {
			t.Fatalf("rendered config leaks the password: %s", s)
		}
	}
	if c.Redacted().Password != Secret(redactedSecret) {
		t.Fatalf("Redacted() password = %q", c.Redacted().Password)
	}

	bad := &Config{
		Endpoints:      []string{"http://127.0.0.1:2379"},
		DialTimeout:    -time.Second,
		RequestTimeout: time.Second,
		Username:       "root",
		Password:       Secret(password),
	}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("error leaks the password: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid dial_timeout") {
		t.Fatalf("error = %v, want it to name dial_timeout", err)
	}

	p := writeConfigFile(t, `endpoints = ["http://127.0.0.1:2379"]
username = "root"
password = "s3cr3t-do-not-print"
dial_timeout = "0s"
`)
	_, loadErr := LoadConfigFile(p)
	if loadErr == nil {
		t.Fatal("expected load error")
	}
	if strings.Contains(loadErr.Error(), password) {
		t.Fatalf("load error leaks the password: %v", loadErr)
	}
}

func TestLoadConfigFileMalformedToml(t *testing.T) {
	const password = "unterminated-secret-value"
	p := writeConfigFile(t, `endpoints = ["http://127.0.0.1:2379"]
username = "root"
password = "`+password+`
`)
	_, err := LoadConfigFile(p)
	if err == nil {
		t.Fatal("expected error for malformed toml")
	}
	if !strings.Contains(err.Error(), "malformed toml") {
		t.Fatalf("error = %v, want malformed toml", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("error leaks the offending value: %v", err)
	}
}

func TestParseEndpoints(t *testing.T) {
	eps, err := ParseEndpoints(" http://127.0.0.1:2379 ,\thttp://127.0.0.1:2380 ")
	if err != nil {
		t.Fatalf("parse endpoints: %v", err)
	}
	want := []string{"http://127.0.0.1:2379", "http://127.0.0.1:2380"}
	if !equalStrings(eps, want) {
		t.Fatalf("endpoints = %v, want %v", eps, want)
	}

	for _, s := range []string{"", " ", "http://127.0.0.1:2379,", ",http://127.0.0.1:2379"} {
		if _, err := ParseEndpoints(s); err == nil {
			t.Fatalf("ParseEndpoints(%q) succeeded, want error", s)
		}
	}
	if _, err := ParseEndpoints("https://a:2379,http://b:2379"); err == nil {
		t.Fatal("mixed scheme accepted")
	}
}

func TestResolveEndpoints(t *testing.T) {
	cases := []struct {
		name      string
		cli       string
		cliSet    bool
		file      []string
		fallback  string
		want      []string
		wantError bool
	}{
		{name: "flag wins", cli: "http://flag:2379", file: []string{"http://file:2379"}, fallback: "http://dash:8500",
			want: []string{"http://flag:2379"}},
		{name: "file next", file: []string{"http://file:2379", "http://file2:2379"}, fallback: "http://dash:8500",
			want: []string{"http://file:2379", "http://file2:2379"}},
		{name: "fallback next", fallback: "http://dash:8500", want: []string{"http://dash:8500"}},
		{name: "default last", want: []string{DefaultEndpoint}},
		{name: "empty flag entry", cli: "http://flag:2379,", wantError: true},
		{name: "explicit empty flag", cliSet: true, wantError: true},
		{name: "invalid fallback", fallback: "127.0.0.1:2379", wantError: true},
		{name: "invalid file entry", file: []string{"/tmp/x"}, wantError: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveEndpointsFlag(c.cli, c.cliSet, c.file, c.fallback)
			if c.wantError {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve endpoints: %v", err)
			}
			if !equalStrings(got, c.want) {
				t.Fatalf("endpoints = %v, want %v", got, c.want)
			}
		})
	}

	// ResolveEndpoints treats an empty cli value as "not given".
	got, err := ResolveEndpoints("", nil, "")
	if err != nil || !equalStrings(got, []string{DefaultEndpoint}) {
		t.Fatalf("ResolveEndpoints default = %v, %v", got, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestProbeAddr pins the TLS-preflight address derivation: an endpoint without
// a port is probed on the scheme's default port, while an explicit port and an
// IPv6 literal are kept intact.
func TestProbeAddr(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://etcd.example.com", "etcd.example.com:443"},
		{"https://h:2379", "h:2379"},
		{"http://h", "h:80"},
		{"https://[::1]", "[::1]:443"},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", c.raw, err)
		}
		if got := probeAddr(u); got != c.want {
			t.Fatalf("probeAddr(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestValidateEndpointAllowsPortlessHTTPS pins that a portless HTTPS endpoint
// is accepted (the preflight supplies the default port): validateEndpoint must
// not reject it just because tls.Dialer needs an explicit port.
func TestValidateEndpointAllowsPortlessHTTPS(t *testing.T) {
	ep, scheme, err := validateEndpoint("https://etcd.example.com")
	if err != nil {
		t.Fatalf("validateEndpoint(portless https) = %v, want no error", err)
	}
	if ep != "https://etcd.example.com" || scheme != "https" {
		t.Fatalf("validateEndpoint = %q, %q; want the normalized https URL", ep, scheme)
	}
}
