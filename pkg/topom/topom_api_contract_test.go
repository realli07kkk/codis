// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

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
// topom admin API. They are the regression baseline for replacing martini
// with a standard library http.ServeMux based implementation. They use the
// filesystem model store, so no Redis / coordinator is required.
//
// Routes that dial a live Redis / proxy / sentinel (group add & info, proxy
// create & online, sentinel info, rdb-analysis upload/remote-fetch/start,
// pitr create) are intentionally left to integration tests. Their handlers
// are dominated by remote state rather than routing or middleware behavior.

type topomAPIRequest struct {
	method     string
	path       string
	body       string
	headers    map[string]string
	remoteAddr string
}

func serveTopomAPIRequest(t *testing.T, h http.Handler, r topomAPIRequest) *httptest.ResponseRecorder {
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

func requireTopomAPIStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d, body = %q", rec.Code, want, rec.Body.String())
	}
}

func decodeTopomAPIResponse(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func requireTopomAPIContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("output does not contain %q: %q", want, s)
	}
}

func newTopomContractServer(t *testing.T) (*Topom, http.Handler, string) {
	t.Helper()
	s := openTopom(t)
	t.Cleanup(func() {
		s.Close()
	})
	return s, newApiServer(s), s.XAuth()
}

func TestTopomAPIRootRedirect(t *testing.T) {
	_, h, _ := newTopomContractServer(t)

	rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/"})
	requireTopomAPIStatus(t, rec, http.StatusFound)
	if loc := rec.Header().Get("Location"); loc != "/topom" {
		t.Fatalf("Location = %q, want /topom", loc)
	}
	// The JSON middleware sets Content-Type before the redirect handler runs,
	// and http.Redirect then skips writing its HTML body.
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if body := rec.Body.String(); body != "" {
		t.Fatalf("redirect body = %q, want empty", body)
	}
}

func TestTopomAPIGetRoutes(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)

	checkTopomModel := func(t *testing.T, rec *httptest.ResponseRecorder) {
		var m models.Topom
		decodeTopomAPIResponse(t, rec, &m)
		if m.ProductName != config.ProductName {
			t.Fatalf("product_name = %q, want %q", m.ProductName, config.ProductName)
		}
		if m.AdminAddr == "" {
			t.Fatal("admin_addr is empty")
		}
	}
	checkSlots := func(t *testing.T, rec *httptest.ResponseRecorder) {
		var slots []*models.Slot
		decodeTopomAPIResponse(t, rec, &slots)
		if len(slots) != models.MaxSlotNum {
			t.Fatalf("slots = %d, want %d", len(slots), models.MaxSlotNum)
		}
	}
	checkOK := func(t *testing.T, rec *httptest.ResponseRecorder) {
		if body := rec.Body.String(); body != `"OK"` {
			t.Fatalf("body = %q, want %q", body, `"OK"`)
		}
	}

	cases := []struct {
		name  string
		path  string
		check func(*testing.T, *httptest.ResponseRecorder)
	}{
		{"overview", "/topom", nil},
		{"overview trailing slash", "/topom/", nil},
		{"overview model", "/topom/model", checkTopomModel},
		{"overview stats", "/topom/stats", nil},
		{"overview slots", "/topom/slots", checkSlots},
		{"api model without xauth", "/api/topom/model", checkTopomModel},
		{"api model trailing slash", "/api/topom/model/", checkTopomModel},
		{"xping", "/api/topom/xping/" + xauth, checkOK},
		{"xping trailing slash", "/api/topom/xping/" + xauth + "/", checkOK},
		{"stats", "/api/topom/stats/" + xauth, nil},
		{"slots", "/api/topom/slots/" + xauth, checkSlots},
		{"acl", "/api/topom/acl/" + xauth, nil},
		{"qps limit", "/api/topom/proxy/qps-limit/" + xauth, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: tc.path})
			requireTopomAPIStatus(t, rec, http.StatusOK)
			if tc.check != nil {
				tc.check(t, rec)
			}
		})
	}
}

func TestTopomAPIRejectsInvalidXAuth(t *testing.T) {
	_, h, _ := newTopomContractServer(t)

	for _, path := range []string{
		"/api/topom/xping/not-the-xauth",
		"/api/topom/stats/not-the-xauth",
		"/api/topom/slots/not-the-xauth",
	} {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: path})
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		requireTopomAPIContains(t, remoteError.Cause, "invalid xauth")
	}
}

func TestTopomAPIRejectsClosedTopom(t *testing.T) {
	s, h, xauth := newTopomContractServer(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close topom: %v", err)
	}

	rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/xping/" + xauth})
	requireTopomAPIStatus(t, rec, 800)
	var remoteError struct {
		Cause string
	}
	decodeTopomAPIResponse(t, rec, &remoteError)
	requireTopomAPIContains(t, remoteError.Cause, "use of closed topom")
}

func TestTopomAPIPutRoutes(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)
	t.Cleanup(func() {
		log.SetLevel(log.LevelError)
	})

	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"reload", "/api/topom/reload/" + xauth, "", http.StatusOK},
		{"loglevel", "/api/topom/loglevel/" + xauth + "/error", "", http.StatusOK},
		{"loglevel invalid", "/api/topom/loglevel/" + xauth + "/nosuchlevel", "", 800},
		{"slot action interval", "/api/topom/slots/action/interval/" + xauth + "/1234", "", http.StatusOK},
		{"slot action disabled", "/api/topom/slots/action/disabled/" + xauth + "/1", "", http.StatusOK},
		{"invalid gid", "/api/topom/group/create/" + xauth + "/abc", "", 800},
		{"invalid sid", "/api/topom/slots/action/create/" + xauth + "/abc/1", "", 800},
		{"invalid force", "/api/topom/proxy/remove/" + xauth + "/token/xyz", "", 800},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{
				method: "PUT", path: tc.path, body: tc.body,
				headers: map[string]string{"Content-Type": "application/json"},
			})
			requireTopomAPIStatus(t, rec, tc.want)
		})
	}
}

func TestTopomAPIJSONBodyBinding(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)

	putQPSLimit := func(t *testing.T, contentType, body string) ProxyQPSLimitView {
		t.Helper()
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "PUT", path: "/api/topom/proxy/qps-limit/" + xauth, body: body,
			headers: map[string]string{"Content-Type": contentType},
		})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var view ProxyQPSLimitView
		decodeTopomAPIResponse(t, rec, &view)
		return view
	}

	view := putQPSLimit(t, "application/json", `{"limit": 42}`)
	if view.Revision != 1 || view.Limit != 42 || !view.Enabled {
		t.Fatalf("qps limit view = %+v, want revision 1 limit 42 enabled", view)
	}

	// binding.Json ignores the request Content-Type, so JSON is still bound
	// when the client labels the body as text/plain.
	view = putQPSLimit(t, "text/plain", `{"limit": 33}`)
	if view.Revision != 2 || view.Limit != 33 {
		t.Fatalf("qps limit view = %+v, want revision 2 limit 33", view)
	}

	// binding.Json does not install an error handler: malformed JSON is
	// silently bound as the zero value and the handler still runs.
	view = putQPSLimit(t, "application/json", `{"limit":`)
	if view.Revision != 3 || view.Limit != 0 {
		t.Fatalf("qps limit view = %+v, want revision 3 limit 0", view)
	}

	aclJSON := `{
		"enabled": true,
		"users": [{
			"name": "app_ro",
			"enabled": true,
			"new_password": "secret",
			"rules": ["+@read", "~app:*"]
		}]
	}`
	rec := serveTopomAPIRequest(t, h, topomAPIRequest{
		method: "PUT", path: "/api/topom/acl/" + xauth, body: aclJSON,
		headers: map[string]string{"Content-Type": "application/json"},
	})
	requireTopomAPIStatus(t, rec, http.StatusOK)
	var aclView ACLView
	decodeTopomAPIResponse(t, rec, &aclView)
	if aclView.Revision != 1 || !aclView.Enabled || len(aclView.Users) != 1 {
		t.Fatalf("acl view = %+v, want revision 1 enabled with one user", aclView)
	}
	if aclView.Users[0].Name != "app_ro" || aclView.Users[0].PasswordCount != 1 {
		t.Fatalf("acl user = %+v", aclView.Users[0])
	}

	rec = serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/acl/" + xauth})
	requireTopomAPIStatus(t, rec, http.StatusOK)
	decodeTopomAPIResponse(t, rec, &aclView)
	if aclView.Revision != 1 || !aclView.Enabled || len(aclView.Users) != 1 {
		t.Fatalf("persisted acl view = %+v", aclView)
	}
}

func TestTopomAPIMethodAndUnknownRoutes(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"get on put route", "GET", "/api/topom/reload/" + xauth},
		{"post on put json route", "POST", "/api/topom/acl/" + xauth},
		{"post on qps limit", "POST", "/api/topom/proxy/qps-limit/" + xauth},
		{"group root", "GET", "/api/topom"},
		{"unknown api", "GET", "/api/topom/unknown"},
		{"unknown overview", "GET", "/topom/unknown"},
		{"debug without slash", "GET", "/debug"},
		{"missing xauth", "GET", "/api/topom/xping/"},
		{"missing stats xauth", "GET", "/api/topom/stats/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: tc.method, path: tc.path})
			requireTopomAPIStatus(t, rec, http.StatusNotFound)
			requireTopomAPIContains(t, rec.Body.String(), "404 page not found")
		})
	}
}

func TestTopomAPIResponseHeaders(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)

	t.Run("success is json", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/model"})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("api error is json", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/xping/bad"})
		requireTopomAPIStatus(t, rec, 800)
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("unknown route is plain text", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/unknown"})
		requireTopomAPIStatus(t, rec, http.StatusNotFound)
		if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
		}
	})

	t.Run("invalid xauth does not leak expected value", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/xping/bad"})
		requireTopomAPIStatus(t, rec, 800)
		if strings.Contains(rec.Body.String(), xauth) {
			t.Fatalf("response leaked xauth: %s", rec.Body.String())
		}
	})
}

func TestTopomAPIGzip(t *testing.T) {
	_, h, _ := newTopomContractServer(t)

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
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "GET", path: "/api/topom/model",
			headers: map[string]string{"Accept-Encoding": "gzip"},
		})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q", got)
		}
		requireTopomAPIContains(t, rec.Header().Get("Vary"), "Accept-Encoding")

		var m models.Topom
		if err := json.Unmarshal(decodeGzipBody(t, rec), &m); err != nil {
			t.Fatalf("decode compressed response: %v", err)
		}
		if m.ProductName != config.ProductName {
			t.Fatalf("product_name = %q, want %q", m.ProductName, config.ProductName)
		}
	})

	t.Run("error response is compressed", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "GET", path: "/api/topom/xping/bad",
			headers: map[string]string{"Accept-Encoding": "gzip"},
		})
		requireTopomAPIStatus(t, rec, 800)
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q", got)
		}
		requireTopomAPIContains(t, string(decodeGzipBody(t, rec)), "invalid xauth")
	})

	t.Run("no accept encoding means no compression", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/model"})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q, want empty", got)
		}
		if !strings.HasPrefix(rec.Body.String(), "{") {
			t.Fatalf("body is not plain json: %q", rec.Body.String())
		}
	})
}

func TestTopomAPIDebugWildcard(t *testing.T) {
	_, h, _ := newTopomContractServer(t)

	t.Run("loopback can reach pprof", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "GET", path: "/debug/pprof/", remoteAddr: "127.0.0.1:45678",
		})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type = %q", ct)
		}
		requireTopomAPIContains(t, rec.Body.String(), "goroutine")
	})

	t.Run("non loopback is forbidden", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "GET", path: "/debug/pprof/", remoteAddr: "203.0.113.10:45678",
		})
		requireTopomAPIStatus(t, rec, http.StatusForbidden)
		requireTopomAPIContains(t, rec.Body.String(), "debug endpoints are only available from loopback")
	})

	t.Run("any method matches wildcard", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "POST", path: "/debug/pprof/", remoteAddr: "127.0.0.1:45678",
		})
		// A 405 from pprof (rather than martini's 404) proves r.Any matched.
		requireTopomAPIStatus(t, rec, http.StatusMethodNotAllowed)
	})
}

type lockedTopomLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedTopomLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedTopomLogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *lockedTopomLogBuffer) Since(start int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if start < 0 || start > b.buf.Len() {
		start = 0
	}
	return string(b.buf.Bytes()[start:])
}

// topomContractLog is installed once during package initialization so the
// audit test never has to swap the log.StdLog global while background topom
// goroutines are reading it.
var topomContractLog = &lockedTopomLogBuffer{}

func init() {
	logger := log.New(topomContractLog, "")
	logger.SetLevel(log.LevelError)
	log.SetStdLog(logger)
}

func captureTopomWarnLog(t *testing.T) func() string {
	t.Helper()
	log.SetLevel(log.LevelWarn)
	t.Cleanup(func() {
		log.SetLevel(log.LevelError)
	})
	start := topomContractLog.Len()
	return func() string {
		return topomContractLog.Since(start)
	}
}

func countTopomAPICallLogs(s string) int {
	var n int
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "API call ") {
			n++
		}
	}
	return n
}

func TestTopomAPIAuditLogRedaction(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)
	logs := captureTopomWarnLog(t)

	rec := serveTopomAPIRequest(t, h, topomAPIRequest{
		method: "PUT", path: "/api/topom/reload/" + xauth,
		remoteAddr: "198.51.100.10:45678",
		headers: map[string]string{
			"X-Real-IP":       "203.0.113.9",
			"X-Forwarded-For": "198.51.100.7",
		},
	})
	requireTopomAPIStatus(t, rec, http.StatusOK)

	out := logs()
	requireTopomAPIContains(t, out, "API call /api/topom/reload/:xauth")
	requireTopomAPIContains(t, out, "from 198.51.100.10:45678")
	requireTopomAPIContains(t, out, "[203.0.113.9]")
	if strings.Contains(out, xauth) {
		t.Fatalf("audit log leaked xauth %q: %s", xauth, out)
	}

	before := countTopomAPICallLogs(logs())
	rec = serveTopomAPIRequest(t, h, topomAPIRequest{
		method: "GET", path: "/api/topom/xping/" + xauth,
	})
	requireTopomAPIStatus(t, rec, http.StatusOK)
	if after := countTopomAPICallLogs(logs()); after != before {
		t.Fatalf("GET request was audited: before=%d after=%d logs=%s", before, after, logs())
	}
}
