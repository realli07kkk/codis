// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package rpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleConnCleanerLifecycle(t *testing.T) {
	var ticks int32
	c := newIdleConnCleaner(func() {
		atomic.AddInt32(&ticks, 1)
	}, 5*time.Millisecond)

	c.Start()
	c.Start() // starting twice must not run two copies of the loop
	if !c.running() {
		t.Fatal("cleaner should be running after Start")
	}

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&ticks) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("cleaner loop did not tick")
		}
		time.Sleep(time.Millisecond)
	}

	c.Close()
	if c.running() {
		t.Fatal("cleaner should be stopped after Close")
	}
	// Close also drops the idle connections once, so take the count after it.
	stopped := atomic.LoadInt32(&ticks)
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&ticks); got != stopped {
		t.Fatalf("cleaner loop still ticking after Close: %d -> %d", stopped, got)
	}

	c.Close() // Close is idempotent
	c.Start()
	if !c.running() {
		t.Fatal("cleaner should restart after Close")
	}
	c.Close()
}

func TestDefaultClientIdleCleanerStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	if err := ApiGetJson(srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if !defaultIdleCleaner.running() {
		t.Fatal("idle cleaner should be started by the first request")
	}
	if err := CloseDefaultClient(); err != nil {
		t.Fatal(err)
	}
	if defaultIdleCleaner.running() {
		t.Fatal("idle cleaner should be stopped by CloseDefaultClient")
	}

	// A later request starts the cleaner again, so stopping it is not a
	// one-way switch for the shared client.
	if err := ApiGetJson(srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if !defaultIdleCleaner.running() {
		t.Fatal("idle cleaner should restart on the next request")
	}
	if err := CloseDefaultClient(); err != nil {
		t.Fatal(err)
	}
}

func TestApiPostJsonWithContextCanceled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ApiPostJsonWithContext(ctx, srv.URL, struct{}{})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled request should fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request was not canceled")
	}
}
