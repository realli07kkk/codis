// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package errors

import (
	"errors"
	"fmt"

	"github.com/CodisLabs/codis/pkg/utils/trace"
)

var TraceEnabled = true

type TracedError struct {
	Stack trace.Stack
	Cause error
}

func (e *TracedError) Error() string {
	return e.Cause.Error()
}

// Unwrap lets standard library errors.Is/As see through the trace wrapper.
func (e *TracedError) Unwrap() error {
	return e.Cause
}

func New(s string) error {
	return errors.New(s)
}

func Trace(err error) error {
	if err == nil || !TraceEnabled {
		return err
	}
	_, ok := err.(*TracedError)
	if ok {
		return err
	}
	return &TracedError{
		Stack: trace.TraceN(1, 32),
		Cause: err,
	}
}

func Errorf(format string, v ...any) error {
	err := fmt.Errorf(format, v...)
	if !TraceEnabled {
		return err
	}
	return &TracedError{
		Stack: trace.TraceN(1, 32),
		Cause: err,
	}
}

// Stack returns the stack captured when the TracedError in err's chain was
// created. Wrapping the traced error with %w keeps the stack discoverable.
func Stack(err error) trace.Stack {
	if err == nil {
		return nil
	}
	var e *TracedError
	if errors.As(err, &e) {
		return e.Stack
	}
	return nil
}

// Cause is kept for compatibility. It unwraps consecutive TracedError layers
// only; new code should use errors.Is/errors.As for classification.
//
// Deprecated: use errors.Is/errors.As directly.
func Cause(err error) error {
	for err != nil {
		e, ok := err.(*TracedError)
		if ok {
			err = e.Cause
		} else {
			return err
		}
	}
	return nil
}

// Equal reports whether err1 and err2 match according to errors.Is, checking
// both directions so the legacy symmetric comparison remains symmetric.
//
// Deprecated: use errors.Is directly.
func Equal(err1, err2 error) bool {
	if err1 == nil || err2 == nil {
		return err1 == nil && err2 == nil
	}
	return errors.Is(err1, err2) || errors.Is(err2, err1)
}

// NotEqual is the negation of Equal.
//
// Deprecated: use !errors.Is directly.
func NotEqual(err1, err2 error) bool {
	return !Equal(err1, err2)
}
