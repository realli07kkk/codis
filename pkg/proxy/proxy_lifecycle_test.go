// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"context"
	stderrors "errors"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// newBareProxy builds a Proxy with only the lifecycle fields. It is enough for
// tests that exercise Close and the background-task registry without listeners.
func newBareProxy(t *testing.T) *Proxy {
	t.Helper()
	s := &Proxy{}
	s.exit.C = make(chan struct{})
	s.closeDone = make(chan struct{})
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s
}

// waitBackground returns when every registered background task has returned.
func waitBackground(t *testing.T, s *Proxy) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.bg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("background tasks did not exit in time")
	}
}

func waitClosed(t *testing.T, s *Proxy) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !s.IsClosed() {
		if time.Now().After(deadline) {
			t.Fatal("proxy did not close in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestProxyCloseIsIdempotent(t *testing.T) {
	s, _ := openProxy(t)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Close returned %v", err)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatalf("second Close returned %v", err)
	}
	if !s.IsClosed() {
		t.Fatal("proxy should report closed")
	}
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() should be closed after Close")
	}
	waitBackground(t, s)
}

func TestProxyCloseStopsMetricsReporter(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	cfg := *newProxyConfig()
	cfg.MetricsReportStatsdServer = pc.LocalAddr().String()
	cfg.MetricsReportStatsdPrefix = "codis_test"
	cfg.MetricsReportStatsdPeriod.Set(time.Second)

	s, err := New(&cfg)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	// The client sends two empty datagrams when it opens its socket, so only a
	// non-empty datagram counts as a report.
	readReport := func(wait time.Duration) (int, error) {
		for {
			if err := pc.SetReadDeadline(time.Now().Add(wait)); err != nil {
				return 0, err
			}
			n, _, err := pc.ReadFrom(buf)
			if err != nil {
				return 0, err
			}
			if n > 0 {
				return n, nil
			}
		}
	}

	if _, err := readReport(5 * time.Second); err != nil {
		t.Fatalf("statsd reporter did not report: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		_ = s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked on the metrics reporter")
	}

	// The report period is one second, so a reporter that survived Close would
	// deliver another datagram inside this window.
	if n, err := readReport(1200 * time.Millisecond); err == nil {
		t.Fatalf("metrics reporter still running after Close, read %d bytes", n)
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("unexpected read error: %v", err)
	}
}

func TestProxyBackgroundTaskPanicIsContained(t *testing.T) {
	s := newBareProxy(t)

	started := make(chan struct{})
	ok := s.startBackground("test panic", func() {
		close(started)
		panic("background task panic")
	})
	if !ok {
		t.Fatal("background task was rejected")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background task did not start")
	}

	// The recovered task must leave the WaitGroup instead of pretending to be
	// healthy, and the panic must not close the proxy.
	waitBackground(t, s)
	if s.IsClosed() {
		t.Fatal("a panicking background task must not close the proxy")
	}
	if !s.startBackground("test after panic", func() {}) {
		t.Fatal("proxy should still accept background tasks")
	}
	waitBackground(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}
}

func TestProxyStartBackgroundRejectedAfterClose(t *testing.T) {
	s := newBareProxy(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.startBackground("late task", func() {
		t.Error("rejected task must not run")
	}) {
		t.Fatal("startBackground should reject tasks after Close")
	}
}

func TestProxyServeStopsWhenListenerClosed(t *testing.T) {
	s, _ := openProxy(t)
	defer s.Close()

	if err := s.lproxy.Close(); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, s)
	waitBackground(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close after listener loss returned %v", err)
	}
}

func TestProxyAPIShutdownDoesNotDeadlock(t *testing.T) {
	s, addr := openProxy(t)
	defer s.Close()

	c := NewApiClient(addr)
	c.SetXAuth(config.ProductName, config.ProductAuth, s.Model().Token)

	done := make(chan error, 1)
	go func() {
		done <- c.Shutdown()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown API returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown API blocked, most likely Close waiting on the admin server")
	}
	if !s.IsClosed() {
		t.Fatal("proxy should be closed after the shutdown API call")
	}
}

// scriptedListener returns a scripted sequence of accept results and then
// blocks until it is closed.
type scriptedListener struct {
	mu     sync.Mutex
	errs   []error
	conns  []net.Conn
	calls  int
	closed chan struct{}
	once   sync.Once
}

func newScriptedListener(errs []error, conns ...net.Conn) *scriptedListener {
	return &scriptedListener{errs: errs, conns: conns, closed: make(chan struct{})}
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	l.calls++
	if len(l.errs) != 0 {
		err := l.errs[0]
		l.errs = l.errs[1:]
		l.mu.Unlock()
		return nil, err
	}
	if len(l.conns) != 0 {
		c := l.conns[0]
		l.conns = l.conns[1:]
		l.mu.Unlock()
		return c, nil
	}
	l.mu.Unlock()
	<-l.closed
	return nil, net.ErrClosed
}

func (l *scriptedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *scriptedListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

func (l *scriptedListener) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func TestProxyReleasesSharedWorkersOnClose(t *testing.T) {
	before := opStatsReporter.refCount()
	if got := keepAliveCallbacks.refCount(); got != before {
		t.Fatalf("shared worker refcounts out of sync: stats=%d keepalive=%d", before, got)
	}

	s, _ := openProxy(t)
	if got := opStatsReporter.refCount(); got != before+1 {
		t.Fatalf("stats reporter refCount = %d, want %d", got, before+1)
	}
	if got := keepAliveCallbacks.refCount(); got != before+1 {
		t.Fatalf("keepalive dispatcher refCount = %d, want %d", got, before+1)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := opStatsReporter.refCount(); got != before {
		t.Fatalf("stats reporter refCount = %d after Close, want %d", got, before)
	}
	if got := keepAliveCallbacks.refCount(); got != before {
		t.Fatalf("keepalive dispatcher refCount = %d after Close, want %d", got, before)
	}
}

func TestProxyCloseStopsSentinelWatcher(t *testing.T) {
	s, _ := openProxy(t)
	if err := s.SetSentinels([]string{"127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	// Let the watcher enter its subscribe/backoff loop before closing.
	time.Sleep(50 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		_ = s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked on the sentinel watcher")
	}
	waitClosed(t, s)
}

func TestAcceptConnRetriesTransientErrors(t *testing.T) {
	s, _ := openProxy(t)
	defer s.Close()

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	l := newScriptedListener([]error{
		syscall.EMFILE,
		&net.OpError{Op: "accept", Net: "tcp", Err: syscall.ECONNABORTED},
	}, client)

	c, err := s.acceptConn(l)
	if err != nil {
		t.Fatalf("acceptConn returned %v", err)
	}
	if c != client {
		t.Fatalf("acceptConn returned %v, want the scripted connection", c)
	}
	if n := l.callCount(); n != 3 {
		t.Fatalf("accept called %d times, want 3", n)
	}
}

func TestAcceptConnStopsOnClosedListener(t *testing.T) {
	s, _ := openProxy(t)
	defer s.Close()

	l := newScriptedListener([]error{net.ErrClosed})
	defer l.Close()

	c, err := s.acceptConn(l)
	if c != nil {
		t.Fatalf("acceptConn returned a connection: %v", c)
	}
	if !stderrors.Is(err, net.ErrClosed) {
		t.Fatalf("acceptConn returned %v, want net.ErrClosed", err)
	}
	if n := l.callCount(); n != 1 {
		t.Fatalf("accept called %d times, want 1", n)
	}
}

func TestAcceptConnTreatsUnknownErrorsAsFatal(t *testing.T) {
	s, _ := openProxy(t)
	defer s.Close()

	l := newScriptedListener([]error{syscall.EACCES})
	defer l.Close()

	_, err := s.acceptConn(l)
	if !stderrors.Is(err, syscall.EACCES) {
		t.Fatalf("acceptConn returned %v, want EACCES", err)
	}
	if n := l.callCount(); n != 1 {
		t.Fatalf("accept called %d times, want 1", n)
	}
}

func TestAcceptErrorRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{net.ErrClosed, false},
		{&net.OpError{Op: "accept", Net: "tcp", Err: net.ErrClosed}, false},
		{syscall.EMFILE, true},
		{syscall.ENFILE, true},
		{syscall.EINTR, true},
		{syscall.ECONNABORTED, true},
		{&net.OpError{Op: "accept", Net: "tcp", Err: syscall.ECONNRESET}, true},
		{syscall.EACCES, false},
	}
	for _, c := range cases {
		if got := acceptErrorRetryable(c.err); got != c.want {
			t.Errorf("acceptErrorRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestStatsReporterLifecycle(t *testing.T) {
	r := newStatsReporter()
	r.qpsPeriod = 5 * time.Millisecond
	r.sysSample = 5 * time.Millisecond
	r.sysRetry = 5 * time.Millisecond

	r.Start()
	r.Start() // starting twice must not run two copies of the loops
	if !r.running() {
		t.Fatal("reporter should be running after Start")
	}

	saved := cmdstats.total.Int64()
	defer func() {
		cmdstats.total.Set(saved)
		cmdstats.qps.Set(0)
	}()

	// The loop samples deltas between ticks, so keep the counter moving until
	// a sample observes a positive delta.
	deadline := time.Now().Add(5 * time.Second)
	for OpQPS() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("qps loop did not update OpQPS")
		}
		cmdstats.total.Add(100)
		time.Sleep(time.Millisecond)
	}

	r.Close()
	if r.running() {
		t.Fatal("reporter should be stopped after Close")
	}
	r.Close() // Close is idempotent

	r.Start()
	if !r.running() {
		t.Fatal("reporter should restart after Close")
	}
	r.Close()
}

func TestKeepAliveDispatcherLifecycle(t *testing.T) {
	d := newKeepAliveDispatcher()
	d.Start()
	d.Start()

	var calls int32
	ran := make(chan struct{}, 4)
	d.dispatch(func() {
		atomic.AddInt32(&calls, 1)
		ran <- struct{}{}
	})
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not run the callback")
	}

	d.Close()
	d.Close() // Close is idempotent

	// With the dispatcher stopped the callback runs on the caller: the INFO
	// state transition still happens, it just stops being asynchronous.
	d.dispatch(func() {
		atomic.AddInt32(&calls, 1)
	})
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("callback ran %d times, want 2", n)
	}

	d.Start()
	d.dispatch(func() {
		atomic.AddInt32(&calls, 1)
		ran <- struct{}{}
	})
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not restart")
	}
	d.Close()
}

func TestKeepAliveDispatcherPanicFallsBackToCaller(t *testing.T) {
	d := newKeepAliveDispatcher()
	d.Start()

	d.dispatch(func() {
		panic("callback panic")
	})

	// A panicking callback ends the loop; the dispatcher must notice and run
	// later callbacks on the caller instead of queueing them forever.
	deadline := time.Now().Add(5 * time.Second)
	for !d.dead.IsTrue() {
		if time.Now().After(deadline) {
			t.Fatal("dispatcher did not mark itself dead after a panic")
		}
		time.Sleep(time.Millisecond)
	}

	ran := false
	d.dispatch(func() {
		ran = true
	})
	if !ran {
		t.Fatal("dispatch after a panicking loop should run the callback on the caller")
	}
	d.Close()
}

func TestSharedWorkerReferenceCount(t *testing.T) {
	var started, stopped int
	w := newSharedWorker(0, func(int) { started++ }, func(int) { stopped++ })

	release1 := w.acquire()
	release2 := w.acquire()
	if started != 1 {
		t.Fatalf("worker started %d times, want 1", started)
	}
	if n := w.refCount(); n != 2 {
		t.Fatalf("refCount = %d, want 2", n)
	}

	release1()
	release1() // release is idempotent
	if stopped != 0 {
		t.Fatalf("worker stopped while an owner remained")
	}
	release2()
	if stopped != 1 {
		t.Fatalf("worker stopped %d times, want 1", stopped)
	}
	if w.running() {
		t.Fatal("worker should not be running after the last release")
	}

	w.acquire()()
	if started != 2 || stopped != 2 {
		t.Fatalf("restart started=%d stopped=%d, want 2/2", started, stopped)
	}
}

func TestProxyClosePanicStillReleasesConcurrentClose(t *testing.T) {
	s := newBareProxy(t)
	s.releases = []func(){func() { panic("release failed") }}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the test release hook to panic")
			}
		}()
		_ = s.Close()
	}()

	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Close remained blocked after a recovered Close panic")
	}
}

func TestMetricsReporterRunsCleanupWhenRegistrationIsRejected(t *testing.T) {
	s := newBareProxy(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var doCalls, cleanupCalls atomic.Int32
	s.startMetricsReporter("test", time.Millisecond, func(context.Context) error {
		doCalls.Add(1)
		return nil
	}, func() error {
		cleanupCalls.Add(1)
		return nil
	})
	if got := doCalls.Load(); got != 0 {
		t.Fatalf("rejected reporter ran %d times", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times, want 1", got)
	}
}
