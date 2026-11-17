// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package atomic2

import "sync/atomic"

// Int64 is kept as a thin compatibility wrapper around sync/atomic.Int64.
// New code should use sync/atomic directly.
type Int64 struct {
	v atomic.Int64
}

func (a *Int64) Int64() int64 {
	return a.v.Load()
}

func (a *Int64) AsInt() int {
	return int(a.v.Load())
}

func (a *Int64) Set(v int64) {
	a.v.Store(v)
}

func (a *Int64) CompareAndSwap(o, n int64) bool {
	return a.v.CompareAndSwap(o, n)
}

func (a *Int64) Swap(v int64) int64 {
	return a.v.Swap(v)
}

func (a *Int64) Add(v int64) int64 {
	return a.v.Add(v)
}

func (a *Int64) Sub(v int64) int64 {
	return a.v.Add(-v)
}

func (a *Int64) Incr() int64 {
	return a.v.Add(1)
}

func (a *Int64) Decr() int64 {
	return a.v.Add(-1)
}
