// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// requestAllocSink keeps the probed request reachable so the compiler cannot
// stack-allocate it and hide the constructor's heap allocation.
var requestAllocSink *Request

func TestRequestAllocConstructorAllocatesOnce(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		requestAllocSink = newRequest()
	})
	if allocs != 1 {
		t.Fatalf("newRequest allocated %v objects, want exactly 1 for Request+WaitGroup", allocs)
	}

	r := requestAllocSink
	if r == nil {
		t.Fatal("newRequest returned nil")
	}
	if r.Batch == nil {
		t.Fatal("newRequest left Batch nil, backend writes could not be joined")
	}
	if r.Multi != nil || r.UnixNano != 0 {
		t.Fatalf("newRequest filled request fields, got Multi=%v UnixNano=%d", r.Multi, r.UnixNano)
	}
}

func TestMakeSubRequestSharesBatchAndSpreadsSeed(t *testing.T) {
	r := newRequest()
	r.seed = 0x1234abcd

	sub := r.MakeSubRequest(3)
	if len(sub) != 3 {
		t.Fatalf("len(sub) = %d, want 3", len(sub))
	}
	for i := range sub {
		if sub[i].Batch != r.Batch {
			t.Fatalf("sub[%d].Batch does not share the parent WaitGroup", i)
		}
		wantSeed := (r.Seed16() + uint(i)) & 0xffff
		if sub[i].Seed16() != wantSeed {
			t.Fatalf("sub[%d].Seed16() = %d, want %d", i, sub[i].Seed16(), wantSeed)
		}
	}

	// Subrequests join through the parent WaitGroup: Wait must block until every
	// subrequest has released its own Add.
	var finished atomic.Int32
	for i := range sub {
		sub[i].Batch.Add(1)
	}
	for i := range sub {
		go func(x *Request) {
			defer x.Batch.Done()
			time.Sleep(time.Millisecond)
			finished.Add(1)
		}(&sub[i])
	}
	r.Batch.Wait()
	if got := finished.Load(); got != int32(len(sub)) {
		t.Fatalf("parent Batch.Wait() returned with %d/%d subrequests finished", got, len(sub))
	}
}

func TestSeed16IsPrecomputedAndIdempotent(t *testing.T) {
	r := newRequest()
	r.seed = 0xfeedface
	r.UnixNano = time.Now().UnixNano()

	first := r.Seed16()
	if first > 0xffff {
		t.Fatalf("Seed16() = %d, want a 16-bit value", first)
	}
	for i := 0; i < 1000; i++ {
		if got := r.Seed16(); got != first {
			t.Fatalf("Seed16() call %d = %d, want the same value %d", i, got, first)
		}
	}

	// The seed is precomputed: it must not depend on the pointer or timestamp.
	other := newRequest()
	other.seed = r.seed
	other.UnixNano = r.UnixNano + 1
	if got := other.Seed16(); got != first {
		t.Fatalf("Seed16() = %d for equal seed with a different UnixNano, want %d", got, first)
	}

	// Changing the precomputed seed is visible through the 16-bit result.
	third := newRequest()
	third.seed = r.seed + 1
	if got := third.Seed16(); got == first {
		t.Fatalf("Seed16() = %d for seed+1, want a different value", got)
	}
}

func TestSessionSeedSequenceStaggersRequests(t *testing.T) {
	s := &Session{seedBase: 0x98765432}

	const n = 4096
	var prev uint
	seen := make(map[uint]struct{}, n)
	for i := 0; i < n; i++ {
		r := newRequest()
		// Every request shares one timestamp: the seed sequence must still
		// tell them apart.
		r.UnixNano = 1
		r.seed = s.nextSeed()

		got := r.Seed16()
		if got > 0xffff {
			t.Fatalf("Seed16() = %d, want a 16-bit value", got)
		}
		if i > 0 && got == prev {
			t.Fatalf("request %d reused seed %d of its predecessor", i, got)
		}
		prev = got
		seen[got] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("%d requests produced only %d distinct 16-bit seeds", n, len(seen))
	}

	// The same request keeps its seed, no matter how often it is queried.
	r := newRequest()
	r.seed = s.nextSeed()
	first := r.Seed16()
	for i := 0; i < 100; i++ {
		if got := r.Seed16(); got != first {
			t.Fatalf("Seed16() = %d after reuse, want %d", got, first)
		}
	}
}

func TestSessionSeedBasesDecorrelateSessions(t *testing.T) {
	config := NewDefaultConfig()

	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	otherLeft, otherRight := net.Pipe()
	defer otherLeft.Close()
	defer otherRight.Close()

	s1 := NewSession(left, config)
	s2 := NewSession(otherLeft, config)

	if s1.seedBase == 0 || s2.seedBase == 0 {
		t.Fatalf("session seed bases are not random: %#x, %#x", s1.seedBase, s2.seedBase)
	}
	if s1.seedBase == s2.seedBase {
		t.Fatalf("two sessions share seed base %#x", s1.seedBase)
	}

	// Distinct bases must produce decorrelated seed streams.
	for i := 0; i < 64; i++ {
		if a, b := s1.nextSeed(), s2.nextSeed(); a == b {
			t.Fatalf("seed %d collides across sessions: %#x", i, a)
		}
	}
}

func TestRequestChanDrainsAfterClose(t *testing.T) {
	ch := NewRequestChanBuffer(4)
	for i := 0; i < 3; i++ {
		if n := ch.PushBack(&Request{UnixNano: int64(i)}); n != i+1 {
			t.Fatalf("PushBack() = %d, want %d", n, i+1)
		}
	}
	ch.Close()

	for i := 0; i < 3; i++ {
		r, ok := ch.PopFront()
		if !ok || r == nil {
			t.Fatalf("PopFront() after Close dropped buffered request %d", i)
		}
		if r.UnixNano != int64(i) {
			t.Fatalf("PopFront() = request %d, want FIFO order %d", r.UnixNano, i)
		}
	}
	if r, ok := ch.PopFront(); ok || r != nil {
		t.Fatalf("PopFront() on drained closed chan = (%v, %v), want (nil, false)", r, ok)
	}
	if !ch.IsEmpty() {
		t.Fatalf("Buffered() = %d after drain, want 0", ch.Buffered())
	}
}

func TestRequestChanPushBackAfterCloseDoesNotPanic(t *testing.T) {
	closed := NewRequestChanBuffer(2)
	closed.Close()
	if n := closed.PushBack(&Request{}); n != 0 {
		t.Fatalf("PushBack() on empty closed chan = %d, want 0", n)
	}
	if n := closed.Buffered(); n != 0 {
		t.Fatalf("Buffered() = %d after rejected PushBack, want 0", n)
	}

	buffered := NewRequestChanBuffer(2)
	buffered.PushBack(&Request{})
	buffered.PushBack(&Request{})
	buffered.Close()
	if n := buffered.PushBack(&Request{}); n != 2 {
		t.Fatalf("PushBack() after Close = %d, want the kept buffer length 2", n)
	}
	if n := buffered.Buffered(); n != 2 {
		t.Fatalf("Buffered() = %d, want 2", n)
	}
}

func TestRequestChanCloseWakesBlockedPop(t *testing.T) {
	ch := NewRequestChan()
	result := make(chan bool, 1)
	go func() {
		_, ok := ch.PopFront()
		result <- ok
	}()

	ch.Close()

	select {
	case ok := <-result:
		if ok {
			t.Fatal("PopFront() reported a request after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PopFront() did not return after Close")
	}
}

var (
	seed16Sink   uint
	nextSeedSink uint32
)

// BenchmarkRequestAllocConstructor measures the single-allocation constructor
// against the previous "&Request{} plus &sync.WaitGroup{}" pair.
func BenchmarkRequestAllocConstructor(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		requestAllocSink = newRequest()
	}
}

func BenchmarkRequestAllocSeparate(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		r := &Request{}
		r.Batch = &sync.WaitGroup{}
		requestAllocSink = r
	}
}

func BenchmarkSeed16Precomputed(b *testing.B) {
	r := newRequest()
	r.seed = 0xdeadbeef
	b.ReportAllocs()
	for b.Loop() {
		seed16Sink = r.Seed16()
	}
}

func BenchmarkSessionNextSeed(b *testing.B) {
	s := &Session{seedBase: 0x12345678}
	b.ReportAllocs()
	for b.Loop() {
		nextSeedSink = s.nextSeed()
	}
}
