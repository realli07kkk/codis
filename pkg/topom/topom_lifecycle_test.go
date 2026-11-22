// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"sync/atomic"
	"testing"
	"time"
)

// waitBackground returns when every registered background task has returned.
func waitBackground(t *testing.T, s *Topom) {
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

func newBareTopom() *Topom {
	s := &Topom{}
	s.exit.C = make(chan struct{})
	s.closeDone = make(chan struct{})
	s.online = true
	return s
}

func TestRunPeriodicStopsOnClose(t *testing.T) {
	s := newBareTopom()

	hits := make(chan struct{}, 1)
	s.startPeriodic("test periodic", time.Millisecond, func() {
		select {
		case hits <- struct{}{}:
		default:
		}
	})

	select {
	case <-hits:
	case <-time.After(5 * time.Second):
		t.Fatal("periodic task did not run")
	}

	close(s.exit.C)
	waitBackground(t, s)
}

func TestRunPeriodicStopsAfterPanic(t *testing.T) {
	s := newBareTopom()

	var calls int32
	s.startPeriodic("test panic", time.Millisecond, func() {
		atomic.AddInt32(&calls, 1)
		panic("periodic task panic")
	})

	// A panicking task must leave the topom background group instead of
	// pretending the job is still healthy.
	waitBackground(t, s)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("periodic task ran %d times, want 1", n)
	}
	if s.IsClosed() {
		t.Fatal("a panicking periodic task must not close the topom")
	}
	close(s.exit.C)
}

func TestTopomCloseWaitsForPeriodicTasks(t *testing.T) {
	s, err := New(newDiskClient(t), config)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(true); err != nil {
		t.Fatal(err)
	}

	// Let the periodic stats/action loops tick at least once so Close has to
	// wait for work in flight.
	time.Sleep(1200 * time.Millisecond)

	closed := make(chan error, 1)
	go func() {
		closed <- s.Close()
	}()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked on periodic tasks")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("second Close returned %v", err)
	}
	if !s.IsClosed() {
		t.Fatal("topom should report closed")
	}
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() should be closed after Close")
	}
	waitBackground(t, s)
}
