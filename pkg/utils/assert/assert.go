// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package assert

import "testing"

// Must fails the test immediately when b is false.
func Must(t testing.TB, b bool) {
	if b {
		return
	}
	t.Helper()
	t.Fatal("assertion failed")
}

// MustNoError fails the test immediately when err is non-nil.
func MustNoError(t testing.TB, err error) {
	if err == nil {
		return
	}
	t.Helper()
	t.Fatalf("error happens, assertion failed: %v", err)
}
