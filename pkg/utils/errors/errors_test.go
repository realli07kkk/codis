// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestTracedErrorSupportsErrorsIs(t *testing.T) {
	sentinel := errors.New("sentinel")
	wrapped := Trace(fmt.Errorf("wrapped: %w", sentinel))
	if !errors.Is(wrapped, sentinel) {
		t.Fatal("errors.Is should see through TracedError")
	}
}

func TestTracedErrorUnwrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	wrapped := Trace(sentinel)
	if got := errors.Unwrap(wrapped); got != sentinel {
		t.Fatalf("unwrap = %v, want %v", got, sentinel)
	}
}
