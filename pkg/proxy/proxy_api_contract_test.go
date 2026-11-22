// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// These tests pin the externally visible HTTP contract of the martini based
// proxy admin API. They are the regression baseline for replacing martini
// with a standard library http.ServeMux based implementation.
//
// Routes that need a live Redis / topom / sentinel peer (fillslots pointing at
// real backends, hot-key-cache broadcast fan-out, sentinel rewatch with
// servers) are intentionally left to integration tests; they are not routing
// or middleware contracts and cannot be constructed cheaply.

type apiContractRequest struct {
	method     string
	path       string
	body       string
	headers    map[string]string
	remoteAddr string
}

func serveAPIRequest(t *testing.T, h http.Handler, r apiContractRequest) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req := httptest.NewRequest(r.method, r.path, body)
	req.RemoteAddr = r.remoteAddr
	if req.RemoteAddr == "" {
		req.RemoteAddr = "192.0.2.1:1234"
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func requireAPIStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d, body = %q", rec.Code, want, rec.Body.String())
	}
}

func decodeAPIResponse(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func requireAPIContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("output does not contain %q: %q", want, s)
	}
}

func newProxyContractServer(t *testing.T) (*Proxy, http.Handler, string) {
	t.Helper()
	s, _ := openProxy(t)
	t.Cleanup(func() {
		s.Close()
	})
	return s, newApiServer(s), s.XAuth()
}

func TestProxyAPIRootRedirect(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/"})
	requireAPIStatus(t, rec, http.StatusFound)
	if loc := rec.Header().Get("Location"); loc != "/proxy" {
		t.Fatalf("Location = %q, want /proxy", loc)
	}
	// The JSON middleware sets Content-Type before the redirect handler runs.
	// http.Redirect then skips its HTML body because a Content-Type already
	// exists, so the current contract is a JSON header with an empty body.
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if body := rec.Body.String(); body != "" {
		t.Fatalf("redirect body = %q, want empty", body)
	}
}

func TestProxyAPIGetRoutes(t *testing.T) {
	_, h, xauth := newProxyContractServer(t)

	checkProxyModel := func(t *testing.T, rec *httptest.ResponseRecorder) {
		var p models.Proxy
		decodeAPIResponse(t, rec, &p)
		if p.ProductName != config.ProductName {
			t.Fatalf("product_name = %q, want %q", p.ProductName, config.ProductName)
		}
	}
	checkSlots := func(t *testing.T, rec *httptest.ResponseRecorder) {
		var slots []*models.Slot
		decodeAPIResponse(t, rec, &slots)
		if len(slots) != models.MaxSlotNum {
			t.Fatalf("slots = %d, want %d", len(slots), models.MaxSlotNum)
		}
	}
	checkOK := func(t *testing.T, rec *httptest.ResponseRecorder) {
		if got := rec.Body.String(); got != `"OK"` {
			t.Fatalf("body = %q, want %q", got, `"OK"`)
		}
	}

	cases := []struct {
		name  string
		path  string
		check func(*testing.T, *httptest.ResponseRecorder)
	}{
		{"overview", "/proxy", nil},
		{"overview trailing slash", "/proxy/", nil},
		{"overview model", "/proxy/model", checkProxyModel},
		{"overview stats", "/proxy/stats", nil},
		{"overview slots", "/proxy/slots", checkSlots},
		{"api model", "/api/proxy/model", checkProxyModel},
		{"api model trailing slash", "/api/proxy/model/", checkProxyModel},
		{"xping", "/api/proxy/xping/" + xauth, checkOK},
		{"xping trailing slash", "/api/proxy/xping/" + xauth + "/", checkOK},
		{"stats", "/api/proxy/stats/" + xauth, nil},
		{"stats flags", "/api/proxy/stats/" + xauth + "/0", nil},
		{"stats flags trailing slash", "/api/proxy/stats/" + xauth + "/0/", nil},
		{"slots", "/api/proxy/slots/" + xauth, checkSlots},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: tc.path})
			requireAPIStatus(t, rec, http.StatusOK)
			if tc.check != nil {
				tc.check(t, rec)
			}
		})
	}
}

func TestProxyAPIOverviewDocumentsRoutes(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/proxy"})
	requireAPIStatus(t, rec, http.StatusOK)

	var o Overview
	decodeAPIResponse(t, rec, &o)
	if o.Model == nil || o.Model.ProductName != config.ProductName {
		t.Fatalf("unexpected overview model: %+v", o.Model)
	}
	if o.Stats == nil {
		t.Fatal("overview stats is nil")
	}
	if len(o.Slots) != models.MaxSlotNum {
		t.Fatalf("overview slots = %d, want %d", len(o.Slots), models.MaxSlotNum)
	}
}

func TestProxyAPIRejectsInvalidXAuth(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	for _, path := range []string{
		"/api/proxy/xping/not-the-xauth",
		"/api/proxy/stats/not-the-xauth",
		"/api/proxy/slots/not-the-xauth",
	} {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: path})
		requireAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeAPIResponse(t, rec, &remoteError)
		requireAPIContains(t, remoteError.Cause, "invalid xauth")
	}
}

func TestProxyAPIRejectsClosedProxy(t *testing.T) {
	s, h, xauth := newProxyContractServer(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close proxy: %v", err)
	}

	rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/api/proxy/xping/" + xauth})
	requireAPIStatus(t, rec, 800)
	var remoteError struct {
		Cause string
	}
	decodeAPIResponse(t, rec, &remoteError)
	requireAPIContains(t, remoteError.Cause, "use of closed proxy")
}

func TestProxyAPIPutRoutes(t *testing.T) {
	_, h, xauth := newProxyContractServer(t)
	t.Cleanup(func() {
		log.SetLevel(log.LevelError)
	})

	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"forcegc", "/api/proxy/forcegc/" + xauth, "", http.StatusOK},
		{"stats reset", "/api/proxy/stats/reset/" + xauth, "", http.StatusOK},
		{"sentinels", "/api/proxy/sentinels/" + xauth, `{}`, http.StatusOK},
		{"sentinels rewatch", "/api/proxy/sentinels/" + xauth + "/rewatch", "", http.StatusOK},
		{"loglevel", "/api/proxy/loglevel/" + xauth + "/error", "", http.StatusOK},
		{"loglevel invalid", "/api/proxy/loglevel/" + xauth + "/nosuchlevel", "", 800},
		{"start", "/api/proxy/start/" + xauth, "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveAPIRequest(t, h, apiContractRequest{
				method: "PUT", path: tc.path, body: tc.body,
				headers: map[string]string{"Content-Type": "application/json"},
			})
			requireAPIStatus(t, rec, tc.want)
		})
	}
}

func TestProxyAPIJSONBodyBinding(t *testing.T) {
	s, h, xauth := newProxyContractServer(t)

	aclJSON := `{
		"revision": 7,
		"enabled": true,
		"updated_at": "2026-01-01T00:00:00Z",
		"users": [{
			"name": "app_ro",
			"enabled": true,
			"password_hashes": ["hash"],
			"rules": ["+@read"]
		}]
	}`
	rec := serveAPIRequest(t, h, apiContractRequest{
		method: "PUT", path: "/api/proxy/acl/" + xauth, body: aclJSON,
		headers: map[string]string{"Content-Type": "application/json"},
	})
	requireAPIStatus(t, rec, http.StatusOK)
	snapshot := s.ACLSnapshot()
	if snapshot == nil {
		t.Fatal("acl snapshot is nil")
	}
	if snapshot.Revision != 7 || !snapshot.Enabled {
		t.Fatalf("acl snapshot = revision %d enabled %t, want 7/true", snapshot.Revision, snapshot.Enabled)
	}
	if user := snapshot.Users["app_ro"]; user == nil || user.Rules[0] != "+@read" {
		t.Fatalf("acl user not bound: %+v", user)
	}

	// binding.Json ignores the request Content-Type header, so a JSON body is
	// accepted even when the client labels it as text/plain.
	rec = serveAPIRequest(t, h, apiContractRequest{
		method: "PUT", path: "/api/proxy/qps-limit/" + xauth,
		body:    `{"revision": 11, "limit": 25}`,
		headers: map[string]string{"Content-Type": "text/plain"},
	})
	requireAPIStatus(t, rec, http.StatusOK)
	qps := s.router.qpsLimiter.Stats()
	if qps.Revision != 11 || qps.Limit != 25 || !qps.Enabled {
		t.Fatalf("qps limit = %+v, want revision 11 limit 25 enabled", qps)
	}

	// binding.Json does not install an error handler: malformed JSON is
	// silently bound as the zero value and the handler still runs. Pin this
	// so a ServeMux migration either preserves it or changes it deliberately.
	rec = serveAPIRequest(t, h, apiContractRequest{
		method: "PUT", path: "/api/proxy/qps-limit/" + xauth, body: `{"revision":`,
		headers: map[string]string{"Content-Type": "application/json"},
	})
	requireAPIStatus(t, rec, http.StatusOK)
	qps = s.router.qpsLimiter.Stats()
	if qps.Revision != 0 || qps.Limit != 0 {
		t.Fatalf("qps limit after malformed JSON = %+v, want zero value", qps)
	}
}

func TestProxyAPIMethodAndUnknownRoutes(t *testing.T) {
	_, h, xauth := newProxyContractServer(t)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"get on put route", "GET", "/api/proxy/forcegc/" + xauth},
		{"post on put json route", "POST", "/api/proxy/acl/" + xauth},
		{"delete on get route", "DELETE", "/proxy"},
		{"group root", "GET", "/api/proxy"},
		{"unknown api", "GET", "/api/proxy/unknown"},
		{"unknown overview", "GET", "/proxy/unknown"},
		{"debug without slash", "GET", "/debug"},
		{"missing xauth", "GET", "/api/proxy/xping/"},
		{"missing stats xauth", "GET", "/api/proxy/stats/"},
		{"extra path segment", "GET", "/api/proxy/slots/" + xauth + "/extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveAPIRequest(t, h, apiContractRequest{method: tc.method, path: tc.path})
			requireAPIStatus(t, rec, http.StatusNotFound)
			requireAPIContains(t, rec.Body.String(), "404 page not found")
		})
	}
}

func TestProxyAPIResponseHeaders(t *testing.T) {
	_, h, xauth := newProxyContractServer(t)

	t.Run("success is json", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/api/proxy/model"})
		requireAPIStatus(t, rec, http.StatusOK)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("api error is json", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/api/proxy/xping/bad"})
		requireAPIStatus(t, rec, 800)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("unknown route is plain text", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/api/proxy/unknown"})
		requireAPIStatus(t, rec, http.StatusNotFound)
		if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
		}
	})

	t.Run("invalid xauth does not leak expected value", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/api/proxy/xping/bad"})
		requireAPIStatus(t, rec, 800)
		if strings.Contains(rec.Body.String(), xauth) {
			t.Fatalf("response leaked xauth: %s", rec.Body.String())
		}
	})
}

func TestProxyAPIGzip(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	decodeGzipBody := func(t *testing.T, rec *httptest.ResponseRecorder) []byte {
		t.Helper()
		zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("open gzip body: %v", err)
		}
		defer zr.Close()
		b, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read gzip body: %v", err)
		}
		return b
	}

	t.Run("json response is compressed", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/api/proxy/model",
			headers: map[string]string{"Accept-Encoding": "gzip"},
		})
		requireAPIStatus(t, rec, http.StatusOK)
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q", got)
		}
		requireAPIContains(t, rec.Header().Get("Vary"), "Accept-Encoding")

		var p models.Proxy
		if err := json.Unmarshal(decodeGzipBody(t, rec), &p); err != nil {
			t.Fatalf("decode compressed response: %v", err)
		}
		if p.ProductName != config.ProductName {
			t.Fatalf("product_name = %q, want %q", p.ProductName, config.ProductName)
		}
	})

	t.Run("error response is compressed", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/api/proxy/xping/bad",
			headers: map[string]string{"Accept-Encoding": "gzip"},
		})
		requireAPIStatus(t, rec, 800)
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q", got)
		}
		requireAPIContains(t, string(decodeGzipBody(t, rec)), "invalid xauth")
	})

	t.Run("no accept encoding means no compression", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: "/api/proxy/model"})
		requireAPIStatus(t, rec, http.StatusOK)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q, want empty", got)
		}
		if !strings.HasPrefix(rec.Body.String(), "{") {
			t.Fatalf("body is not plain json: %q", rec.Body.String())
		}
	})

	t.Run("unrelated accept encoding means no compression", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/api/proxy/model",
			headers: map[string]string{"Accept-Encoding": "br"},
		})
		requireAPIStatus(t, rec, http.StatusOK)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q, want empty", got)
		}
	})
}

func TestProxyAPIDebugWildcard(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	t.Run("loopback can reach pprof", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/debug/pprof/", remoteAddr: "127.0.0.1:45678",
		})
		requireAPIStatus(t, rec, http.StatusOK)
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q", ct)
		}
		requireAPIContains(t, rec.Body.String(), "goroutine")
	})

	t.Run("loopback pprof subpath", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/debug/pprof/cmdline", remoteAddr: "127.0.0.1:45678",
		})
		requireAPIStatus(t, rec, http.StatusOK)
	})

	t.Run("non loopback is forbidden", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/debug/pprof/", remoteAddr: "203.0.113.10:45678",
		})
		requireAPIStatus(t, rec, http.StatusForbidden)
		requireAPIContains(t, rec.Body.String(), "debug endpoints are only available from loopback")
	})

	t.Run("any method matches wildcard", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "POST", path: "/debug/pprof/", remoteAddr: "127.0.0.1:45678",
		})
		// A 405 from pprof (rather than martini's 404) proves r.Any matched.
		requireAPIStatus(t, rec, http.StatusMethodNotAllowed)
	})
}

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *lockedLogBuffer) Since(start int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if start < 0 || start > b.buf.Len() {
		start = 0
	}
	return string(b.buf.Bytes()[start:])
}

// proxyContractLog is installed once during package initialization so the
// audit test never has to swap the log.StdLog global while background proxy
// goroutines are reading it.
var proxyContractLog = &lockedLogBuffer{}

func init() {
	logger := log.New(proxyContractLog, "")
	logger.SetLevel(log.LevelError)
	log.SetStdLog(logger)
}

func captureWarnLog(t *testing.T) func() string {
	t.Helper()
	log.SetLevel(log.LevelWarn)
	t.Cleanup(func() {
		log.SetLevel(log.LevelError)
	})
	start := proxyContractLog.Len()
	return func() string {
		return proxyContractLog.Since(start)
	}
}

func countAPICallLogs(s string) int {
	var n int
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "API call ") {
			n++
		}
	}
	return n
}

func TestProxyAPIAuditLogRedaction(t *testing.T) {
	_, h, xauth := newProxyContractServer(t)
	logs := captureWarnLog(t)

	rec := serveAPIRequest(t, h, apiContractRequest{
		method: "PUT", path: "/api/proxy/forcegc/" + xauth,
		remoteAddr: "198.51.100.10:45678",
		headers: map[string]string{
			"X-Real-IP":       "203.0.113.9",
			"X-Forwarded-For": "198.51.100.7",
		},
	})
	requireAPIStatus(t, rec, http.StatusOK)

	out := logs()
	requireAPIContains(t, out, "API call /api/proxy/forcegc/:xauth")
	requireAPIContains(t, out, "from 198.51.100.10:45678")
	requireAPIContains(t, out, "[203.0.113.9]")
	if strings.Contains(out, xauth) {
		t.Fatalf("audit log leaked xauth %q: %s", xauth, out)
	}

	before := countAPICallLogs(logs())
	rec = serveAPIRequest(t, h, apiContractRequest{
		method: "GET", path: "/api/proxy/xping/" + xauth,
	})
	requireAPIStatus(t, rec, http.StatusOK)
	if after := countAPICallLogs(logs()); after != before {
		t.Fatalf("GET request was audited: before=%d after=%d logs=%s", before, after, logs())
	}
}
