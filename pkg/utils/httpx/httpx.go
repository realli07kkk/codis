// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package httpx provides the standard library replacement for the martini
// helpers that used to back the codis admin HTTP APIs: recovery, gzip, JSON
// body binding, the audit log middleware and a small router shim that keeps
// the martini routing contract on top of http.ServeMux.
package httpx

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"runtime/debug"
	"strings"

	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// Root is the ServeMux pattern that matches "/" only. A bare "/" pattern is a
// subtree in ServeMux, so the admin APIs register this pattern instead.
const Root = "/{$}"

// catchAll is the method-less fallback pattern. It is the least specific
// pattern possible, so it only runs when no method pattern matched.
const catchAll = "/{path...}"

// Middleware wraps an http.Handler, matching the composition style of the
// martini handlers it replaces.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware in order: the first entry is the outermost.
func Chain(h http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

var notFound = http.HandlerFunc(http.NotFound)

// Recovery logs panics raised by the wrapped handler and replies with a bare
// 500 unless the handler already started the response.
func Recovery() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			tw := &trackingWriter{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Errorf("PANIC: %+v\n%s", v, debug.Stack())
				if !tw.wrote {
					tw.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(tw, req)
		})
	}
}

type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *trackingWriter) WriteHeader(status int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *trackingWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(p)
}

// Flush keeps the writer usable by handlers that type-assert http.Flusher, as
// the martini response writer allowed.
func (w *trackingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *trackingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Gzip compresses responses for clients that advertise gzip, mirroring
// martini-contrib/gzip: Content-Encoding and Vary are set before the handler
// runs and Content-Length is dropped once the body has been written.
func Gzip() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
				next.ServeHTTP(w, req)
				return
			}
			header := w.Header()
			header.Set("Content-Encoding", "gzip")
			header.Set("Vary", "Accept-Encoding")
			gz := gzip.NewWriter(w)
			gw := &gzipResponseWriter{ResponseWriter: w, writer: gz}
			defer func() {
				header.Del("Content-Length")
				if !gw.wrote {
					// Closing an unused gzip.Writer emits a header and footer. During
					// panic unwinding that would mark the outer recovery writer as
					// written and turn the eventual 500 into a false 200 response.
					header.Del("Content-Encoding")
					header.Del("Vary")
					return
				}
				_ = gz.Close()
			}()
			next.ServeHTTP(gw, req)
		})
	}
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
	wrote  bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return w.writer.Write(p)
}

func (w *gzipResponseWriter) Flush() {
	if w.wrote {
		_ = w.writer.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ContentType sets the response Content-Type before the wrapped handler runs.
func ContentType(value string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", value)
			next.ServeHTTP(w, req)
		})
	}
}

// AuditLog logs every non-GET /api/ request with the xauth secret redacted.
// The caller address is taken from X-Real-IP, then X-Forwarded-For, and is
// logged next to the connection address.
func AuditLog(owner any, xauth func() string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			path := req.URL.Path
			if req.Method != http.MethodGet && strings.HasPrefix(path, "/api/") {
				path = utils.RedactPathSecret(path, xauth())
				var headerAddr string
				for _, key := range []string{"X-Real-IP", "X-Forwarded-For"} {
					if val := req.Header.Get(key); val != "" {
						headerAddr = val
						break
					}
				}
				log.Warnf("[%p] API call %s from %s [%s]", owner, path, req.RemoteAddr, headerAddr)
			}
			next.ServeHTTP(w, req)
		})
	}
}

// BindJSON decodes a JSON request body into v the way martini-contrib/binding
// did: the declared Content-Type is ignored and a decoding error leaves v at
// whatever the decoder produced instead of failing the request.
func BindJSON(req *http.Request, v any) {
	if req.Body == nil {
		return
	}
	_ = json.NewDecoder(req.Body).Decode(v)
}

// WriteResponse writes the legacy (status, body) pair produced by
// rpc.ApiResponseJson and rpc.ApiResponseError. Status 800 is part of the
// codis admin wire protocol and must be preserved verbatim.
func WriteResponse(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// Router registers routes on an http.ServeMux while keeping the externally
// visible behaviour of the martini router:
//
//   - every route also accepts exactly one trailing slash;
//   - a request whose path matches a route but whose method does not gets the
//     plain 404 body instead of ServeMux's 405 response;
//   - HEAD is served by GET routes, as martini did;
//   - paths ServeMux would clean up or redirect are answered with 404.
type Router struct {
	mux     *http.ServeMux
	claimed map[string]bool
}

// NewRouter returns a Router with no routes other than the 404 fallback.
func NewRouter() *Router {
	r := &Router{mux: http.NewServeMux(), claimed: make(map[string]bool)}
	// Without a method-less fallback ServeMux answers 405 when the path
	// matches a pattern registered for another method. martini answered 404,
	// and the fallback also keeps ServeMux from ever returning 405 itself.
	r.mux.Handle(catchAll, notFound)
	return r
}

// Get registers a GET route. HEAD requests are served by the same handler,
// matching martini's GET/HEAD overload.
func (r *Router) Get(pattern string, h http.HandlerFunc) {
	r.handle(http.MethodGet, pattern, h)
}

// Put registers a PUT route.
func (r *Router) Put(pattern string, h http.HandlerFunc) {
	r.handle(http.MethodPut, pattern, h)
}

// Post registers a POST route.
func (r *Router) Post(pattern string, h http.HandlerFunc) {
	r.handle(http.MethodPost, pattern, h)
}

// Any registers a route for every method, matching martini's Router.Any.
func (r *Router) Any(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, h)
	if p, ok := withTrailingSlash(pattern); ok {
		r.mux.Handle(p, h)
	}
}

// NotFound registers pattern as a plain 404 route for every method. It is
// used where ServeMux would redirect to a wildcard route: "/debug/**" did not
// match "/debug" under martini, and a bare "/debug" request must stay a 404
// instead of becoming a 307 to "/debug/".
func (r *Router) NotFound(pattern string) {
	if r.claimed[pattern] {
		return
	}
	r.claimed[pattern] = true
	r.mux.Handle(pattern, notFound)
}

func (r *Router) handle(method, pattern string, h http.HandlerFunc) {
	r.mux.Handle(method+" "+pattern, h)
	if p, ok := withTrailingSlash(pattern); ok {
		r.mux.Handle(method+" "+p, h)
	}
}

// ServeHTTP rejects request paths that are not already canonical and then
// dispatches to the ServeMux. ServeMux redirects "//a", "/a/./b" or "/a/../b"
// to the cleaned path; martini never matched such paths, so they stay 404.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if p := req.URL.Path; p != cleanPath(p) {
		http.NotFound(w, req)
		return
	}
	// martini matched the decoded path, so an encoded slash never reached a
	// route. ServeMux matches the escaped path and would decode "%2F" into a
	// path parameter, which must not turn into a different route decision.
	if p := req.URL.EscapedPath(); p != cleanPath(p) || strings.Contains(p, "%2f") || strings.Contains(p, "%2F") {
		http.NotFound(w, req)
		return
	}
	r.mux.ServeHTTP(w, req)
}

// withTrailingSlash returns the pattern matching pattern followed by a single
// slash. martini appended an optional "/" to every route pattern.
func withTrailingSlash(pattern string) (string, bool) {
	if pattern == Root || strings.HasSuffix(pattern, "/{$}") || strings.HasSuffix(pattern, "...}") {
		return "", false
	}
	return pattern + "/{$}", true
}

// cleanPath returns the canonical form of p, keeping a trailing slash. It
// mirrors the internal net/http helper ServeMux uses to decide redirects.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		if len(p) == len(np)+1 && strings.HasPrefix(p, np) {
			return p
		}
		return np + "/"
	}
	return np
}
