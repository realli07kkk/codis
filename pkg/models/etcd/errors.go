// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"

	rpctypes "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

var (
	ErrClosedClient = errors.New("use of closed etcd client")

	// ErrExist means the key already existed when Create was called.
	ErrExist = errors.New("key already exists")

	// ErrNotExist means the key (or the path prefix) was missing while
	// must=true required it.
	ErrNotExist = errors.New("key does not exist")
)

// Kind is the outcome class of an error, designed for callers that must tell
// lock conflicts apart from connectivity failures.
type Kind int

const (
	KindOK Kind = iota
	// KindNotFound means the key or path does not exist.
	KindNotFound
	// KindExist means the key already exists.
	KindExist
	// KindRejected means the server (or the request) refused the operation:
	// permission, authentication, invalid arguments. The server did not
	// apply the change.
	KindRejected
	// KindUnknown means the outcome cannot be confirmed: deadline, cancel,
	// unavailable, EOF, dropped connection. The server may or may not have
	// applied the change.
	KindUnknown
	// KindOther is anything not covered above.
	KindOther
)

func (k Kind) String() string {
	switch k {
	case KindOK:
		return "OK"
	case KindNotFound:
		return "NotFound"
	case KindExist:
		return "Exist"
	case KindRejected:
		return "Rejected"
	case KindUnknown:
		return "Unknown"
	case KindOther:
		return "Other"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// OpError wraps a failed etcd operation with its key and its Kind. It keeps
// the original error reachable through errors.Is/As via Unwrap.
type OpError struct {
	Op   string
	Key  string
	Err  error
	Kind Kind
}

func (e *OpError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("etcd %s %s: %v", e.Op, e.Key, e.Err)
}

func (e *OpError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Rejected reports whether the server definitively refused the operation
// before applying any change. Callers that must tell a refusal (permission,
// authentication, invalid arguments) apart from an unknown outcome can use
// this through the models.Rejected interface instead of importing this
// package.
func (e *OpError) Rejected() bool {
	return e != nil && e.Kind == KindRejected
}

// Classify reports which Kind an error belongs to. Callers that only have an
// error (for example a failed Create during lock acquisition) can use it to
// decide whether a retry is safe.
func Classify(err error) Kind {
	if err == nil {
		return KindOK
	}
	var opErr *OpError
	if stderrors.As(err, &opErr) {
		return opErr.Kind
	}
	switch {
	case stderrors.Is(err, ErrNotExist):
		return KindNotFound
	case stderrors.Is(err, ErrExist):
		return KindExist
	case stderrors.Is(err, ErrClosedClient):
		return KindOther
	case stderrors.Is(err, context.DeadlineExceeded), stderrors.Is(err, context.Canceled):
		return KindUnknown
	case stderrors.Is(err, io.EOF), stderrors.Is(err, io.ErrUnexpectedEOF):
		return KindUnknown
	}
	var etcdErr rpctypes.EtcdError
	if stderrors.As(err, &etcdErr) {
		return kindOfCode(etcdErr.Code())
	}
	if s, ok := status.FromError(err); ok {
		return kindOfCode(s.Code())
	}
	return KindOther
}

func kindOfCode(code codes.Code) Kind {
	switch code {
	case codes.NotFound:
		return KindNotFound
	case codes.AlreadyExists:
		return KindExist
	case codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
		return KindRejected
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Aborted:
		return KindUnknown
	}
	return KindOther
}

func wrapOp(op, key string, err error) error {
	return &OpError{Op: op, Key: key, Err: err, Kind: Classify(err)}
}
