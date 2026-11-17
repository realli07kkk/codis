// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package atomic2

import "testing"

func TestInt64ZeroValue(t *testing.T) {
	var v Int64
	if v.Int64() != 0 || v.AsInt() != 0 {
		t.Fatalf("zero value = %d/%d", v.Int64(), v.AsInt())
	}
	if got := v.Incr(); got != 1 {
		t.Fatalf("incr = %d", got)
	}
	if got := v.Add(4); got != 5 {
		t.Fatalf("add = %d", got)
	}
	if got := v.Decr(); got != 4 {
		t.Fatalf("decr = %d", got)
	}
	if got := v.Sub(2); got != 2 {
		t.Fatalf("sub = %d", got)
	}
	if !v.CompareAndSwap(2, 7) || v.Int64() != 7 {
		t.Fatalf("cas failed, value = %d", v.Int64())
	}
	if got := v.Swap(9); got != 7 || v.Int64() != 9 {
		t.Fatalf("swap = %d, value = %d", got, v.Int64())
	}
}

func TestBoolZeroValue(t *testing.T) {
	var v Bool
	if v.IsTrue() || !v.IsFalse() {
		t.Fatal("zero value should be false")
	}
	v.Set(true)
	if !v.IsTrue() || v.Bool() != true {
		t.Fatal("set true failed")
	}
	if !v.CompareAndSwap(true, false) || v.IsTrue() {
		t.Fatal("cas failed")
	}
	if got := v.Swap(true); got != false || !v.IsTrue() {
		t.Fatalf("swap = %v", got)
	}
}
