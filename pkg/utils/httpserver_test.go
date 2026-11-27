// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"bytes"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/log"
)

// syncBuffer collects log output written by several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs redirects the package-level logger to a buffer for the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	logger := log.New(buf, "")
	std := log.SwapStdLog(logger)
	t.Cleanup(func() {
		log.SetStdLog(std)
	})
	return buf
}

// serveOnListener starts an admin server on a private listener and waits until
// Serve has registered it, which a completed request proves.
func serveOnListener(t *testing.T, hs *http.Server) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = hs.Serve(l) }()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + l.Addr().String() + "/")
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	_ = resp.Body.Close()
	return l
}

// TestShutdownHTTPServerIgnoresClosedListener pins the shutdown order used by
// Topom.Close and Proxy.Close: the listener is closed first (to unblock accept),
// so the graceful drain finds it already closed and reports net.ErrClosed. That
// is the normal shutdown path and must not be logged as a failure.
func TestShutdownHTTPServerIgnoresClosedListener(t *testing.T) {
	logs := captureLogs(t)

	hs := NewAdminHTTPServer(http.NotFoundHandler())
	l := serveOnListener(t, hs)

	_ = l.Close()
	ShutdownHTTPServer(hs, time.Second)

	if out := logs.String(); strings.Contains(out, "shutdown failed") {
		t.Fatalf("an already closed listener was reported as a shutdown failure: %q", out)
	}
}

// TestShutdownHTTPServerReportsRealFailures keeps the other branch: a drain
// that really fails (here: it runs out of time with a request in flight) is
// still reported.
func TestShutdownHTTPServerReportsRealFailures(t *testing.T) {
	logs := captureLogs(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	hs := NewAdminHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(entered)
			<-release
		}
	}))
	l := serveOnListener(t, hs)
	defer func() {
		releaseHandler()
		_ = l.Close()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + l.Addr().String() + "/slow")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the blocking handler was never reached")
	}

	ShutdownHTTPServer(hs, 50*time.Millisecond)
	if out := logs.String(); !strings.Contains(out, "http admin server shutdown failed") {
		t.Fatalf("a drained-out shutdown was not reported: %q", out)
	}

	releaseHandler()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the in-flight request did not finish")
	}
}
