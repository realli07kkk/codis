// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package httpx

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/utils/log"
)

func newTestRouter() *Router {
	r := NewRouter()
	r.Get(Root, func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "root")
	})
	r.Get("/model", func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "model")
	})
	r.Put("/items/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "put:"+req.PathValue("id"))
	})
	r.NotFound("/debug")
	r.Any("/debug/{rest...}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "debug:"+req.PathValue("rest"))
	})
	return r
}

func serve(h http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouterRoutes(t *testing.T) {
	r := newTestRouter()

	cases := []struct {
		name   string
		method string
		path   string
		status int
		body   string
	}{
		{"root", "GET", "/", 200, "root"},
		{"root head", "HEAD", "/", 200, "root"},
		{"model", "GET", "/model", 200, "model"},
		{"model trailing slash", "GET", "/model/", 200, "model"},
		{"model head", "HEAD", "/model", 200, "model"},
		{"put", "PUT", "/items/42", 200, "put:42"},
		{"put trailing slash", "PUT", "/items/42/", 200, "put:42"},
		{"debug wildcard", "GET", "/debug/pprof/", 200, "debug:pprof/"},
		{"debug wildcard any method", "POST", "/debug/pprof/", 200, "debug:pprof/"},

		// Wrong methods and unknown paths are 404, never 405.
		{"post on get", "POST", "/model", 404, "404 page not found\n"},
		{"get on put", "GET", "/items/42", 404, "404 page not found\n"},
		{"head on put", "HEAD", "/items/42", 404, "404 page not found\n"},
		{"delete on root", "DELETE", "/", 404, "404 page not found\n"},
		{"unknown path", "GET", "/unknown", 404, "404 page not found\n"},
		{"unknown deep path", "GET", "/model/extra", 404, "404 page not found\n"},
		{"debug without slash", "GET", "/debug", 404, "404 page not found\n"},
		{"empty wildcard segment", "GET", "/items/", 404, "404 page not found\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(r, tc.method, tc.path)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d, body = %q", rec.Code, tc.status, rec.Body.String())
			}
			if rec.Body.String() != tc.body {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.body)
			}
			if tc.status == http.StatusNotFound {
				if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
					t.Fatalf("404 Content-Type = %q", ct)
				}
				if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("404 X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
				}
			}
		})
	}
}

func TestRouterDoesNotRedirect(t *testing.T) {
	r := newTestRouter()
	for _, path := range []string{"//model", "/model//", "/./model", "/x/../model"} {
		rec := serve(r, "GET", path)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Fatalf("%s: unexpected redirect to %q", path, loc)
		}
	}
	// "/debug" must not be redirected to the "/debug/" wildcard route.
	rec := serve(r, "GET", "/debug")
	if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
		t.Fatalf("/debug: status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRouterRejectsEncodedSlash(t *testing.T) {
	r := newTestRouter()
	// An encoded slash would otherwise be decoded into the {id} parameter.
	req := httptest.NewRequest("PUT", "/items/a%2Fb", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %q", rec.Code, rec.Body.String())
	}
}

func TestRecovery(t *testing.T) {
	t.Run("panic becomes 500", func(t *testing.T) {
		h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			panic("boom")
		}), Recovery())
		rec := serve(h, "GET", "/")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("body = %q, want empty", rec.Body.String())
		}
	})

	t.Run("panic with gzip becomes 500 instead of false 200", func(t *testing.T) {
		h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			panic("boom")
		}), Recovery(), Gzip())
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q, want empty after panic before write", got)
		}
	})

	t.Run("panic after write keeps the first status", func(t *testing.T) {
		h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "partial")
			panic("late")
		}), Recovery())
		rec := serve(h, "GET", "/")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != "partial" {
			t.Fatalf("body = %q", rec.Body.String())
		}
	})

	t.Run("no panic passes through", func(t *testing.T) {
		h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}), Recovery())
		rec := serve(h, "GET", "/")
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
		}
	})
}

func TestGzip(t *testing.T) {
	body := strings.Repeat("codis", 128)
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, body)
	}), Gzip())

	t.Run("compressed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q", got)
		}
		if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Fatalf("Vary = %q", got)
		}
		zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		defer zr.Close()
		got, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read gzip body: %v", err)
		}
		if string(got) != body {
			t.Fatalf("decoded body mismatch: %d bytes", len(got))
		}
	})

	t.Run("unrelated encoding is not compressed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept-Encoding", "br")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q", got)
		}
		if rec.Body.String() != body {
			t.Fatalf("body was not plain text")
		}
	})
}

func TestContentType(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// The header is already present before the handler runs.
		if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Fatalf("in-handler Content-Type = %q", got)
		}
	}), ContentType("application/json; charset=utf-8"))
	rec := serve(h, "GET", "/")
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}

	// http.NotFound overrides the pre-set Content-Type.
	h = Chain(http.HandlerFunc(http.NotFound), ContentType("application/json; charset=utf-8"))
	rec = serve(h, "GET", "/")
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("not found Content-Type = %q", got)
	}
}

func TestBindJSON(t *testing.T) {
	type payload struct {
		Revision int64 `json:"revision"`
		Limit    int64 `json:"limit"`
	}

	t.Run("content type is ignored", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", strings.NewReader(`{"revision":7,"limit":25}`))
		req.Header.Set("Content-Type", "text/plain")
		var v payload
		BindJSON(req, &v)
		if v.Revision != 7 || v.Limit != 25 {
			t.Fatalf("payload = %+v", v)
		}
	})

	t.Run("malformed body keeps the zero value", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", strings.NewReader(`{"revision":`))
		var v payload
		BindJSON(req, &v)
		if v.Revision != 0 || v.Limit != 0 {
			t.Fatalf("payload = %+v", v)
		}
	})

	t.Run("empty body keeps the zero value", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", nil)
		var v payload
		BindJSON(req, &v)
		if v.Revision != 0 || v.Limit != 0 {
			t.Fatalf("payload = %+v", v)
		}
	})
}

func TestWriteResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteResponse(rec, 800, `{"cause":"invalid xauth"}`)
	if rec.Code != 800 {
		t.Fatalf("status = %d, want 800", rec.Code)
	}
	if rec.Body.String() != `{"cause":"invalid xauth"}` {
		t.Fatalf("body = %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	WriteResponse(rec, http.StatusOK, "")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestAuditLog(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "")
	logger.SetLevel(log.LevelWarn)
	std := log.SwapStdLog(logger)
	t.Cleanup(func() {
		log.SetStdLog(std)
	})

	xauth := "0123456789abcdef"
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), AuditLog("owner", func() string { return xauth }))

	req := httptest.NewRequest("PUT", "/api/proxy/forcegc/"+xauth, nil)
	req.RemoteAddr = "198.51.100.10:45678"
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := buf.String()
	for _, want := range []string{
		"API call /api/proxy/forcegc/:xauth",
		"from 198.51.100.10:45678",
		"[203.0.113.9]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("audit log does not contain %q: %s", want, out)
		}
	}
	if strings.Contains(out, xauth) {
		t.Fatalf("audit log leaked xauth: %s", out)
	}

	buf.Reset()
	req = httptest.NewRequest("GET", "/api/proxy/forcegc/"+xauth, nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if buf.Len() != 0 {
		t.Fatalf("GET request was audited: %s", buf.String())
	}

	buf.Reset()
	req = httptest.NewRequest("GET", "/api/proxy/xping/"+xauth, nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if buf.Len() != 0 {
		t.Fatalf("GET request was audited: %s", buf.String())
	}
}

func TestChainOrder(t *testing.T) {
	var order []string
	mark := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				order = append(order, name+"-in")
				next.ServeHTTP(w, req)
				order = append(order, name+"-out")
			})
		}
	}
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
	}), mark("outer"), mark("inner"))
	serve(h, "GET", "/")
	want := "outer-in inner-in handler inner-out outer-out"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
}
