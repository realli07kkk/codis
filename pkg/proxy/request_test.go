// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestRequestChan1(t *testing.T) {
	var ch = NewRequestChanBuffer(0)
	for i := 0; i < 8192; i++ {
		n := ch.PushBack(&Request{UnixNano: int64(i)})
		assert.Must(t, n == i+1)
	}
	for i := 0; i < 8192; i++ {
		r, ok := ch.PopFront()
		assert.Must(t, ok && r.UnixNano == int64(i))
	}
	assert.Must(t, ch.Buffered() == 0)

	ch.Close()

	_, ok := ch.PopFront()
	assert.Must(t, !ok)
}

func TestRequestChan2(t *testing.T) {
	var ch = NewRequestChanBuffer(512)
	for i := 0; i < 8192; i++ {
		n := ch.PushBack(&Request{UnixNano: int64(i)})
		assert.Must(t, n == i+1)
	}
	ch.Close()

	assert.Must(t, ch.Buffered() == 8192)

	for i := 0; i < 8192; i++ {
		r, ok := ch.PopFront()
		assert.Must(t, ok && r.UnixNano == int64(i))
	}
	assert.Must(t, ch.Buffered() == 0)

	_, ok := ch.PopFront()
	assert.Must(t, !ok)
}

func TestRequestChan3(t *testing.T) {
	var wg sync.WaitGroup
	var ch = NewRequestChanBuffer(512)

	const n = 1000 * 1000 * 4

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			ch.PushBack(&Request{UnixNano: int64(i)})
			if i%1024 == 0 {
				runtime.Gosched()
			}
		}
	}()

	errCh := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			r, ok := ch.PopFront()
			if !ok || r.UnixNano != int64(i) {
				errCh <- fmt.Errorf("pop %d: request = %+v, ok = %v", i, r, ok)
				return
			}
			if i%4096 == 0 {
				runtime.Gosched()
			}
		}
		errCh <- nil
	}()

	wg.Wait()
	assert.MustNoError(t, <-errCh)

	go func() {
		defer ch.Close()
		time.Sleep(time.Millisecond * 10)
	}()

	_, ok := ch.PopFront()
	assert.Must(t, !ok)
}

// benchmarkRequestSink keeps the request received by the benchmark loop live
// so the loop cannot be optimized away.
var benchmarkRequestSink *Request

func BenchmarkRequestGoChannel(b *testing.B) {
	var request = &Request{
		Multi: make([]*redis.Resp, 1024*1024),
	}
	var ch = make(chan *Request, 1024)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range b.N {
			ch <- request
		}
	}()

	// b.Loop resets b.N to zero on its first call, so a producer/consumer
	// benchmark that depends on a fixed shared iteration count must use b.N.
	for range b.N {
		benchmarkRequestSink = <-ch
	}
	<-done
}

func benchmarkRequestChanN(b *testing.B, n int) {
	var request = &Request{
		Multi: make([]*redis.Resp, 1024*1024),
	}
	var ch = NewRequestChanBuffer(n)
	go func() {
		defer ch.Close()
		for i := range b.N {
			ch.PushBack(request)
			if i%1024 == 0 {
				runtime.Gosched()
			}
		}
	}()

	// Preserve the original batched-drain measurement. Driving b.Loop with
	// individual PopFront calls measures cond wakeup latency instead.
	ch.PopFrontAllVoid(func(r *Request) {})
}

func BenchmarkRequestChan128(b *testing.B)  { benchmarkRequestChanN(b, 128) }
func BenchmarkRequestChan256(b *testing.B)  { benchmarkRequestChanN(b, 256) }
func BenchmarkRequestChan512(b *testing.B)  { benchmarkRequestChanN(b, 512) }
func BenchmarkRequestChan1024(b *testing.B) { benchmarkRequestChanN(b, 1024) }
func BenchmarkRequestChan2048(b *testing.B) { benchmarkRequestChanN(b, 2048) }
