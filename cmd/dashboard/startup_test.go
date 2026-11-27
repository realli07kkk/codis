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

// codis-dashboard resolves its config and logs the effective options inside
// main(), so the startup contract is verified by re-executing this test binary
// as a real codis-dashboard process, like cmd/fe/startup_test.go.
const (
	dashboardStartupHelperEnv = "CODIS_DASHBOARD_STARTUP_HELPER"
	dashboardStartupArgsEnv   = "CODIS_DASHBOARD_STARTUP_ARGS"
	dashboardStartupHelperRun = "^TestCodisDashboardStartupHelperProcess$"
)

// TestCodisDashboardStartupHelperProcess is not a normal test: when the helper
// environment variable is set it rewrites os.Args and runs the real main().
func TestCodisDashboardStartupHelperProcess(t *testing.T) {
	if os.Getenv(dashboardStartupHelperEnv) != "1" {
		t.Skip("codis-dashboard startup helper process")
	}
	os.Args = append([]string{"codis-dashboard"}, strings.Split(os.Getenv(dashboardStartupArgsEnv), "\n")...)
	main()
}

// runCodisDashboard starts the helper process and returns its combined output
// and the error from Wait, which is non-nil for a non-zero exit status.
func runCodisDashboard(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+dashboardStartupHelperRun, "-test.count=1")
	cmd.Env = append(os.Environ(),
		dashboardStartupHelperEnv+"=1",
		dashboardStartupArgsEnv+"="+strings.Join(argv, "\n"),
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("codis-dashboard %v did not finish: %v\n%s", argv, ctx.Err(), out)
	}
	return string(out), err
}

// TestCodisDashboardStartupLogDoesNotPrintCredentials pins that startup only
// records whether credentials are set: neither the option line, the config
// echo nor an error text may carry the --product_auth value. An unlistenable
// admin_addr keeps the run short and independent of etcd, which is reached
// only after the options were logged.
func TestCodisDashboardStartupLogDoesNotPrintCredentials(t *testing.T) {
	const secret = "dashboard-product-secret-4a"

	configFile := filepath.Join(t.TempDir(), "dashboard.toml")
	if err := os.WriteFile(configFile, []byte("admin_addr = \"not-an-address\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	argv := []string{"--config=" + configFile, "--product_auth=" + secret}
	out, err := runCodisDashboard(t, argv...)
	if err == nil {
		t.Fatalf("codis-dashboard %v exited successfully, want the listen failure\n%s", argv, out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("startup output leaks the product_auth value:\n%s", out)
	}
	for _, want := range []string{"product_auth=<set>", "<redacted>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("startup output does not contain %q:\n%s", want, out)
		}
	}
}
