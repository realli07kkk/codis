// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeDebugHTTPRejectsNonLoopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.RemoteAddr = "203.0.113.7:4321"
	w := httptest.NewRecorder()

	ServeDebugHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestServeDebugHTTPAllowsLoopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	w := httptest.NewRecorder()

	ServeDebugHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatal("loopback request should not be forbidden")
	}
}

func TestRedactPathSecret(t *testing.T) {
	path := "/api/proxy/stats/abc123"
	if got := RedactPathSecret(path, "abc123"); got != "/api/proxy/stats/:xauth" {
		t.Fatalf("redacted path = %q", got)
	}
	if got := RedactPathSecret(path, ""); got != path {
		t.Fatalf("empty secret should not change path, got %q", got)
	}
}
