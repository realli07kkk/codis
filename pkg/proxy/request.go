// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"sync"
	"sync/atomic"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/sync2/atomic2"
)

type Request struct {
	Multi []*redis.Resp
	Batch *sync.WaitGroup
	Group *sync.WaitGroup

	Broken *atomic2.Bool

	OpStr string
	OpFlag

	Database int32
	UnixNano int64

	// seed is computed once when the session creates the request. Seed16 only
	// reads it, so repeated calls return the same value for the same request.
	seed uint32

	ACLIdentity         *SessionACLIdentity
	SkipRedisErrorStats bool

	*redis.Resp
	Err error

	Coalesce func() error

	done uint32
}

// requestAlloc joins a Request with the WaitGroup that tracks its backend
// writes in a single allocation. The inner Request is never copied: callers
// only ever hold a pointer into the allocation and Batch points at the
// embedded WaitGroup.
type requestAlloc struct {
	r  Request
	wg sync.WaitGroup
}

// newRequest allocates a Request whose Batch is ready for backend writes with
// one heap allocation instead of one for the Request and one for the
// WaitGroup.
func newRequest() *Request {
	a := &requestAlloc{}
	a.r.Batch = &a.wg
	return &a.r
}

// IsDone reports whether the backend response has been written. It is safe to
// call concurrently with setResponse; a true result means Resp can be read.
func (r *Request) IsDone() bool {
	return atomic.LoadUint32(&r.done) == 1
}

func (r *Request) markDone() {
	atomic.StoreUint32(&r.done, 1)
}

func (r *Request) IsBroken() bool {
	return r.Broken != nil && r.Broken.IsTrue()
}

func (r *Request) MakeSubRequest(n int) []Request {
	var sub = make([]Request, n)
	for i := range sub {
		x := &sub[i]
		x.Batch = r.Batch
		x.OpStr = r.OpStr
		x.OpFlag = r.OpFlag
		x.Broken = r.Broken
		x.Database = r.Database
		x.UnixNano = r.UnixNano
		// Keep each subrequest idempotent while spreading children over
		// backend connections, as the old per-address seeds did.
		x.seed = r.seed + uint32(i)
		x.ACLIdentity = r.ACLIdentity
		x.SkipRedisErrorStats = r.SkipRedisErrorStats
	}
	return sub
}

// seed16Mask keeps Seed16 inside the 16-bit range its callers expect when
// they rotate over replica groups and backend connections.
const seed16Mask = 0xffff

// Seed16 returns the rotation seed precomputed for this request. It only reads
// a field, so it is idempotent and safe to call after the request was handed
// to a backend.
func (r *Request) Seed16() uint {
	return uint(r.seed & seed16Mask)
}

type RequestChan struct {
	lock sync.Mutex
	cond *sync.Cond

	data []*Request
	buff []*Request

	waits  int
	closed bool
}

const DefaultRequestChanBuffer = 128

func NewRequestChan() *RequestChan {
	return NewRequestChanBuffer(0)
}

func NewRequestChanBuffer(n int) *RequestChan {
	if n <= 0 {
		n = DefaultRequestChanBuffer
	}
	var ch = &RequestChan{
		buff: make([]*Request, n),
	}
	ch.cond = sync.NewCond(&ch.lock)
	return ch
}

func (c *RequestChan) Close() {
	c.lock.Lock()
	if !c.closed {
		c.closed = true
		c.cond.Broadcast()
	}
	c.lock.Unlock()
}

func (c *RequestChan) Buffered() int {
	c.lock.Lock()
	n := len(c.data)
	c.lock.Unlock()
	return n
}

func (c *RequestChan) PushBack(r *Request) int {
	c.lock.Lock()
	n := c.lockedPushBack(r)
	c.lock.Unlock()
	return n
}

func (c *RequestChan) PopFront() (*Request, bool) {
	c.lock.Lock()
	r, ok := c.lockedPopFront()
	c.lock.Unlock()
	return r, ok
}

func (c *RequestChan) lockedPushBack(r *Request) int {
	if c.closed {
		return len(c.data)
	}
	if c.waits != 0 {
		c.cond.Signal()
	}
	c.data = append(c.data, r)
	return len(c.data)
}

func (c *RequestChan) lockedPopFront() (*Request, bool) {
	for len(c.data) == 0 {
		if c.closed {
			return nil, false
		}
		c.data = c.buff[:0]
		c.waits++
		c.cond.Wait()
		c.waits--
	}
	var r = c.data[0]
	c.data, c.data[0] = c.data[1:], nil
	return r, true
}

func (c *RequestChan) IsEmpty() bool {
	return c.Buffered() == 0
}

func (c *RequestChan) PopFrontAll(onRequest func(r *Request) error) error {
	for {
		r, ok := c.PopFront()
		if ok {
			if err := onRequest(r); err != nil {
				return err
			}
		} else {
			return nil
		}
	}
}

func (c *RequestChan) PopFrontAllVoid(onRequest func(r *Request)) {
	c.PopFrontAll(func(r *Request) error {
		onRequest(r)
		return nil
	})
}
