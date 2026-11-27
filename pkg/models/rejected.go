// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import stderrors "errors"

// Rejected is implemented by coordinator errors that the server definitively
// refused before applying any change (permission denied, authentication
// failed, invalid argument, ...). The lock API reports them as-is, without a
// read-back and without labelling them "unknown".
type Rejected interface {
	Rejected() bool
}

// IsRejected reports whether err carries such a definite refusal. Drivers
// signal it through the Rejected interface, so pkg/models never has to import
// a concrete driver package (architecture rule D2).
func IsRejected(err error) bool {
	var rejected Rejected
	return stderrors.As(err, &rejected) && rejected.Rejected()
}

// unknownOutcome marks an error whose overall result cannot be confirmed. It
// reports Rejected() == false even when a cause was a definite refusal (for
// example a refused read-back), because the lock state itself stays unknown
// and must never be read as "the server applied nothing".
type unknownOutcome struct {
	err error
}

func (e *unknownOutcome) Error() string { return e.err.Error() }

func (e *unknownOutcome) Unwrap() error { return e.err }

func (e *unknownOutcome) Rejected() bool { return false }
