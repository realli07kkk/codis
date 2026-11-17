// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package atomic2

import "sync/atomic"

// Bool is kept as a thin compatibility wrapper around sync/atomic.Bool.
// New code should use sync/atomic directly.
type Bool struct {
	v atomic.Bool
}

func (b *Bool) Bool() bool {
	return b.v.Load()
}

func (b *Bool) IsTrue() bool {
	return b.v.Load()
}

func (b *Bool) IsFalse() bool {
	return !b.v.Load()
}

func (b *Bool) Set(v bool) {
	b.v.Store(v)
}

func (b *Bool) CompareAndSwap(o, n bool) bool {
	return b.v.CompareAndSwap(o, n)
}

func (b *Bool) Swap(v bool) bool {
	return b.v.Swap(v)
}
