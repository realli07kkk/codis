// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// codis-fe builds its martini HTTP stack inside main(), so the integration
// test below re-executes this test binary as a real codis-fe process. The
// remaining tests exercise the production StaticLoader and ReverseProxy
// building blocks directly. No coordinator is required: --dashboard-list is
// backed by a temp JSON file.

const (
	feContractHelperEnv     = "CODIS_FE_CONTRACT_HELPER"
	feContractDashboardEnv  = "CODIS_FE_CONTRACT_DASHBOARD_LIST"
	feContractAssetsEnv     = "CODIS_FE_CONTRACT_ASSETS"
	feContractListenEnv     = "CODIS_FE_CONTRACT_LISTEN"
	feContractHelperProcess = "^TestCodisFEContractHelperProcess$"
)

// TestCodisFEContractHelperProcess is not a normal test: when the helper
// environment variable is set it rewrites os.Args and runs the real main().
func TestCodisFEContractHelperProcess(t *testing.T) {
	if os.Getenv(feContractHelperEnv) != "1" {
		t.Skip("codis-fe contract helper process")
	}
	os.Args = []string{
		"codis-fe",
		"--dashboard-list=" + os.Getenv(feContractDashboardEnv),
		"--assets-dir=" + os.Getenv(feContractAssetsEnv),
		"--listen=" + os.Getenv(feContractListenEnv),
		"--log-level=ERROR",
	}
	main()
}

func TestFEStaticLoaderReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.json")
	content := `[
		{"name": "beta", "dashboard": "127.0.0.1:18081"},
		{"name": "alpha", "dashboard": "127.0.0.1:18080"}
	]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &StaticLoader{path: path}
	routes, err := loader.Reload()
	if err != nil {
		t.Fatalf("reload dashboard list: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %v", routes)
	}
	if routes["alpha"] != "127.0.0.1:18080" || routes["beta"] != "127.0.0.1:18081" {
		t.Fatalf("routes = %v", routes)
	}
}

func TestFEStaticLoaderRejectsMalformedList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(path, []byte(`{"not": "a list"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&StaticLoader{path: path}).Reload(); err == nil {
		t.Fatal("expected malformed dashboard list to fail")
	}
}

type contractTestLoader struct {
	routes map[string]string
	err    error
}

func (l contractTestLoader) Reload() (map[string]string, error) {
	return l.routes, l.err
}

func TestFEReverseProxyRoutesAndForwarding(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Fe-Backend", "1")
		fmt.Fprintf(w, "%s %s", r.Method, r.URL.RequestURI())
	}))
	defer backend.Close()

	host := strings.TrimPrefix(backend.URL, "http://")
	r := NewReverseProxy(contractTestLoader{routes: map[string]string{
		"alpha":  host,
		"empty":  "",
		"":       host,
		"broken": "not-a-host-port",
	}})

	names := r.GetNames()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "broken" {
		t.Fatalf("names = %v, want [alpha broken]", names)
	}
	if p := r.GetProxy("missing"); p != nil {
		t.Fatal("unexpected proxy for missing route")
	}

	p := r.GetProxy("alpha")
	if p == nil {
		t.Fatal("missing proxy for alpha")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/topom/model?forward=alpha", nil)
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Fe-Backend"); got != "1" {
		t.Fatalf("X-Fe-Backend = %q", got)
	}
	if got := rec.Body.String(); got != "POST /api/topom/model?forward=alpha" {
		t.Fatalf("backend body = %q", got)
	}
}

type contractSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *contractSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *contractSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func contractFreeTCPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve listen address: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func contractStartCodisFE(t *testing.T, assets, dashboardList string) string {
	t.Helper()

	addr := contractFreeTCPAddr(t)
	output := &contractSafeBuffer{}
	cmd := exec.Command(os.Args[0], "-test.run="+feContractHelperProcess, "-test.count=1")
	cmd.Env = append(os.Environ(),
		feContractHelperEnv+"=1",
		feContractDashboardEnv+"="+dashboardList,
		feContractAssetsEnv+"="+assets,
		feContractListenEnv+"="+addr,
	)
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start codis-fe helper: %v", err)
	}

	var waitErr error
	var waitMu sync.Mutex
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		waitMu.Lock()
		waitErr = err
		waitMu.Unlock()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
	})

	base := "http://" + addr
	client := &http.Client{
		Timeout:       500 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case <-exited:
			waitMu.Lock()
			err := waitErr
			waitMu.Unlock()
			t.Fatalf("codis-fe helper exited before serving: %v\n%s", err, output.String())
		default:
		}

		resp, err := client.Get(base + "/index.html")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return base
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("codis-fe did not start: %v\n%s", err, output.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestCodisFEStaticAndProxyHTTPContract(t *testing.T) {
	assets := t.TempDir()
	contractWriteFile(t, filepath.Join(assets, "index.html"), "<html><body>codis-fe-index</body></html>")
	contractWriteFile(t, filepath.Join(assets, "app.js"), `console.log("fe-asset");`)
	contractWriteFile(t, filepath.Join(assets, "sub", "index.html"), "<html><body>sub-index</body></html>")

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Fe-Backend", "1")
		fmt.Fprintf(w, "%s %s", r.Method, r.URL.RequestURI())
	}))
	defer backend.Close()

	dashboardList := filepath.Join(t.TempDir(), "dashboard.json")
	dashboardJSON, err := json.Marshal([]map[string]string{{
		"name":      "fe-test",
		"dashboard": strings.TrimPrefix(backend.URL, "http://"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	contractWriteFile(t, dashboardList, string(dashboardJSON))

	base := contractStartCodisFE(t, assets, dashboardList)
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	request := func(t *testing.T, method, path string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s %s: %v", method, path, err)
		}
		return resp.StatusCode, resp.Header, string(b)
	}

	// The reverse proxy map is populated lazily by /list. Check the
	// unprimed behavior first, then prime it so the subtests below do not
	// depend on execution order.
	status, _, body := request(t, "GET", "/api/topom/model?forward=fe-test")
	if status != http.StatusForbidden {
		t.Fatalf("unprimed reverse proxy status = %d, body = %q", status, body)
	}
	status, _, body = request(t, "GET", "/list")
	if status != http.StatusOK {
		t.Fatalf("prime reverse proxy via /list: status = %d, body = %q", status, body)
	}

	t.Run("static index at root", func(t *testing.T) {
		status, header, body := request(t, "GET", "/")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %q", status, body)
		}
		if !strings.Contains(body, "codis-fe-index") {
			t.Fatalf("body = %q", body)
		}
		if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("static explicit index", func(t *testing.T) {
		status, _, body := request(t, "GET", "/index.html")
		if status != http.StatusOK || !strings.Contains(body, "codis-fe-index") {
			t.Fatalf("status = %d, body = %q", status, body)
		}
	})

	t.Run("static javascript asset", func(t *testing.T) {
		status, header, body := request(t, "GET", "/app.js")
		if status != http.StatusOK || !strings.Contains(body, "fe-asset") {
			t.Fatalf("status = %d, body = %q", status, body)
		}
		if ct := header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("static directory redirect", func(t *testing.T) {
		status, header, body := request(t, "GET", "/sub")
		if status != http.StatusFound {
			t.Fatalf("status = %d, body = %q", status, body)
		}
		if loc := header.Get("Location"); loc != "/sub/" {
			t.Fatalf("Location = %q, want /sub/", loc)
		}
		// Static redirects happen before any JSON middleware, so the default
		// http.Redirect HTML body is present.
		if !strings.Contains(body, `href="/sub/"`) {
			t.Fatalf("redirect body = %q", body)
		}
	})

	t.Run("static directory index", func(t *testing.T) {
		status, _, body := request(t, "GET", "/sub/")
		if status != http.StatusOK || !strings.Contains(body, "sub-index") {
			t.Fatalf("status = %d, body = %q", status, body)
		}
	})

	t.Run("missing static asset falls through to catch all", func(t *testing.T) {
		status, _, body := request(t, "GET", "/missing.js")
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, body = %q", status, body)
		}
	})

	t.Run("list returns dashboard names", func(t *testing.T) {
		status, _, body := request(t, "GET", "/list")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %q", status, body)
		}
		var names []string
		if err := json.Unmarshal([]byte(body), &names); err != nil {
			t.Fatalf("decode /list %q: %v", body, err)
		}
		if len(names) != 1 || names[0] != "fe-test" {
			t.Fatalf("names = %v", names)
		}

		// /list is registered as GET; martini's optional trailing slash
		// makes /list/ match as well.
		status, _, body = request(t, "GET", "/list/")
		if status != http.StatusOK {
			t.Fatalf("GET /list/ status = %d, body = %q", status, body)
		}
	})

	t.Run("reverse proxy forwards GET", func(t *testing.T) {
		status, header, body := request(t, "GET", "/api/topom/model?forward=fe-test")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %q", status, body)
		}
		if got := header.Get("X-Fe-Backend"); got != "1" {
			t.Fatalf("X-Fe-Backend = %q", got)
		}
		if !strings.Contains(body, "GET /api/topom/model?forward=fe-test") {
			t.Fatalf("backend body = %q", body)
		}
	})

	t.Run("reverse proxy catch all accepts any method", func(t *testing.T) {
		status, _, body := request(t, "POST", "/api/topom/model?forward=fe-test")
		if status != http.StatusOK {
			t.Fatalf("status = %d, body = %q", status, body)
		}
		if !strings.Contains(body, "POST /api/topom/model?forward=fe-test") {
			t.Fatalf("backend body = %q", body)
		}
	})

	t.Run("unknown forward name is forbidden", func(t *testing.T) {
		status, _, body := request(t, "GET", "/api/topom/model?forward=missing")
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, body = %q", status, body)
		}
	})

	t.Run("missing forward parameter is forbidden", func(t *testing.T) {
		status, _, body := request(t, "GET", "/api/topom/model")
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, body = %q", status, body)
		}
	})

	t.Run("post is not accepted by list route", func(t *testing.T) {
		status, _, body := request(t, "POST", "/list")
		if status != http.StatusForbidden {
			t.Fatalf("status = %d, body = %q", status, body)
		}
	})
}

func contractWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
