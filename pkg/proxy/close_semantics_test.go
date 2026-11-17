// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

func TestBackendConnCloseUnblocksFullQueue(t *testing.T) {
	bc := &BackendConn{
		done:  make(chan struct{}),
		input: make(chan *Request, 1),
	}
	bc.input <- &Request{}

	r := &Request{Batch: &sync.WaitGroup{}}
	pushed := make(chan struct{})
	go func() {
		bc.PushBack(r)
		close(pushed)
	}()

	// Let PushBack reach the blocking send on the full input queue.
	time.Sleep(50 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		bc.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on a full input queue")
	}
	select {
	case <-pushed:
	case <-time.After(time.Second):
		t.Fatal("PushBack did not return after Close")
	}

	if !errors.Equal(r.Err, ErrBackendConnReset) {
		t.Fatalf("err = %v, want %v", r.Err, ErrBackendConnReset)
	}
}

func TestBackendConnPushBackAfterClose(t *testing.T) {
	bc := NewBackendConn("127.0.0.1:1", 0, NewDefaultConfig())
	bc.Close()

	r := &Request{}
	r.Batch = &sync.WaitGroup{}
	bc.PushBack(r)
	r.Batch.Wait()

	if !errors.Equal(r.Err, ErrBackendConnReset) {
		t.Fatalf("err = %v, want %v", r.Err, ErrBackendConnReset)
	}
}

func TestRequestChanPushAfterClose(t *testing.T) {
	ch := NewRequestChanBuffer(1)
	ch.Close()
	if n := ch.PushBack(&Request{}); n != 0 {
		t.Fatalf("buffered = %d, want 0", n)
	}
}
