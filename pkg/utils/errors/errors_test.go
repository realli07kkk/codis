// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

func TestTracedErrorSupportsErrorsIs(t *testing.T) {
	sentinel := stderrors.New("sentinel")
	wrapped := Trace(fmt.Errorf("wrapped: %w", sentinel))
	if !stderrors.Is(wrapped, sentinel) {
		t.Fatal("errors.Is should see through TracedError")
	}
}

func TestTracedErrorUnwrap(t *testing.T) {
	sentinel := stderrors.New("sentinel")
	wrapped := Trace(sentinel)
	if got := stderrors.Unwrap(wrapped); got != sentinel {
		t.Fatalf("unwrap = %v, want %v", got, sentinel)
	}
}

func TestErrorfSupportsMultiLayerWrapping(t *testing.T) {
	sentinel := stderrors.New("sentinel")
	inner := Errorf("inner: %w", sentinel)
	outer := Trace(fmt.Errorf("middle: %w", inner))
	wrapped := fmt.Errorf("outer: %w", outer)

	if !stderrors.Is(wrapped, sentinel) {
		t.Fatal("multi-layer %w chain lost the sentinel")
	}
	if got := Stack(wrapped); got == nil {
		t.Fatal("Stack should see the TracedError through the %w chain")
	} else if got[0].File == "" {
		t.Fatalf("stack record has no file: %+v", got[0])
	}
}

func TestErrorfSupportsMultiplePercentW(t *testing.T) {
	first := stderrors.New("first")
	second := stderrors.New("second")
	wrapped := Trace(fmt.Errorf("both: %w and %w", first, second))

	if !stderrors.Is(wrapped, first) || !stderrors.Is(wrapped, second) {
		t.Fatal("errors.Is should match every %w operand")
	}
}

type codedError struct {
	code int
}

func (e *codedError) Error() string {
	return fmt.Sprintf("coded error %d", e.code)
}

func TestErrorsAsFindsCustomType(t *testing.T) {
	err := &codedError{code: 42}
	wrapped := Trace(fmt.Errorf("wrapped: %w", err))

	var target *codedError
	if !stderrors.As(wrapped, &target) {
		t.Fatal("errors.As should find the custom error type")
	}
	if target.code != 42 {
		t.Fatalf("code = %d, want 42", target.code)
	}
}

func TestEqualUsesStandardSemantics(t *testing.T) {
	sentinel := stderrors.New("sentinel")
	wrapped := Trace(fmt.Errorf("wrapped: %w", sentinel))

	cases := []struct {
		name     string
		err1     error
		err2     error
		expected bool
	}{
		{"both nil", nil, nil, true},
		{"first nil", nil, sentinel, false},
		{"second nil", sentinel, nil, false},
		{"same sentinel", sentinel, sentinel, true},
		{"wrapped first", wrapped, sentinel, true},
		{"wrapped second", sentinel, wrapped, true},
		{"different pointers same text", stderrors.New("sentinel"), sentinel, false},
		{"unrelated", stderrors.New("other"), sentinel, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Equal(c.err1, c.err2); got != c.expected {
				t.Fatalf("Equal(%v, %v) = %v, want %v", c.err1, c.err2, got, c.expected)
			}
			if got := NotEqual(c.err1, c.err2); got == c.expected {
				t.Fatalf("NotEqual(%v, %v) = %v, want %v", c.err1, c.err2, got, !c.expected)
			}
		})
	}
}

func TestCauseCompatibility(t *testing.T) {
	root := stderrors.New("root")
	traced := Trace(fmt.Errorf("layer: %w", root))
	double := Trace(traced)

	if got := Cause(double); got == nil {
		t.Fatal("Cause returned nil")
	} else if !stderrors.Is(got, root) {
		t.Fatalf("Cause = %v, want a wrapper of %v", got, root)
	}

	// Cause keeps its historical scope: it does not unwrap arbitrary %w layers.
	wrapped := fmt.Errorf("outer: %w", traced)
	if got := Cause(wrapped); got != wrapped {
		t.Fatalf("Cause should stop at a non-TracedError wrapper, got %v", got)
	}
}

func TestTraceDisabledAndIdempotent(t *testing.T) {
	prev := TraceEnabled
	TraceEnabled = false
	defer func() { TraceEnabled = prev }()
	sentinel := stderrors.New("sentinel")
	if got := Trace(sentinel); got != sentinel {
		t.Fatalf("Trace with tracing disabled = %v, want original", got)
	}
	if got := Errorf("disabled: %w", sentinel); !stderrors.Is(got, sentinel) {
		t.Fatalf("Errorf with tracing disabled = %v", got)
	}
	if _, ok := Errorf("plain").(*TracedError); ok {
		t.Fatal("Errorf should not wrap when tracing is disabled")
	}
	TraceEnabled = prev

	traced := Trace(sentinel)
	if got := Trace(traced); got != traced {
		t.Fatalf("Trace should be idempotent, got %v", got)
	}
	if Trace(nil) != nil {
		t.Fatal("Trace(nil) should be nil")
	}
	if Stack(nil) != nil {
		t.Fatal("Stack(nil) should be nil")
	}
	if Cause(nil) != nil {
		t.Fatal("Cause(nil) should be nil")
	}
}
