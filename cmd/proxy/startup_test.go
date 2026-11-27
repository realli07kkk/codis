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

// codis-proxy resolves its config and logs the effective options inside
// main(), so the startup contract is verified by re-executing this test binary
// as a real codis-proxy process, like cmd/fe/startup_test.go.
const (
	proxyStartupHelperEnv = "CODIS_PROXY_STARTUP_HELPER"
	proxyStartupArgsEnv   = "CODIS_PROXY_STARTUP_ARGS"
	proxyStartupHelperRun = "^TestCodisProxyStartupHelperProcess$"
)

// TestCodisProxyStartupHelperProcess is not a normal test: when the helper
// environment variable is set it rewrites os.Args and runs the real main().
func TestCodisProxyStartupHelperProcess(t *testing.T) {
	if os.Getenv(proxyStartupHelperEnv) != "1" {
		t.Skip("codis-proxy startup helper process")
	}
	os.Args = append([]string{"codis-proxy"}, strings.Split(os.Getenv(proxyStartupArgsEnv), "\n")...)
	main()
}

// runCodisProxy starts the helper process and returns its combined output and
// the error from Wait, which is non-nil for a non-zero exit status.
func runCodisProxy(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+proxyStartupHelperRun, "-test.count=1")
	cmd.Env = append(os.Environ(),
		proxyStartupHelperEnv+"=1",
		proxyStartupArgsEnv+"="+strings.Join(argv, "\n"),
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("codis-proxy %v did not finish: %v\n%s", argv, ctx.Err(), out)
	}
	return string(out), err
}

// TestCodisProxyStartupLogDoesNotPrintCredentials pins that startup only
// records whether credentials are set: neither the option lines, the config
// echo nor an error text may carry the --product_auth or --session_auth value.
// An unlistenable proxy_addr keeps the run short and independent of backends
// and of the dashboard, which are reached only after the options were logged.
func TestCodisProxyStartupLogDoesNotPrintCredentials(t *testing.T) {
	const productSecret = "proxy-product-secret-4a"
	const sessionSecret = "proxy-session-secret-4a"

	configFile := filepath.Join(t.TempDir(), "proxy.toml")
	if err := os.WriteFile(configFile, []byte("proxy_addr = \"not-an-address\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	argv := []string{
		"--config=" + configFile,
		"--product_auth=" + productSecret,
		"--session_auth=" + sessionSecret,
	}
	out, err := runCodisProxy(t, argv...)
	if err == nil {
		t.Fatalf("codis-proxy %v exited successfully, want the listen failure\n%s", argv, out)
	}
	for _, secret := range []string{productSecret, sessionSecret} {
		if strings.Contains(out, secret) {
			t.Fatalf("startup output leaks the credential value %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{"product_auth=<set>", "session_auth=<set>", "<redacted>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("startup output does not contain %q:\n%s", want, out)
		}
	}
}
