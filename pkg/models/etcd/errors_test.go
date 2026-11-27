// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"strings"
	"testing"

	rpctypes "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Kind
	}{
		{name: "nil", err: nil, want: KindOK},
		{name: "not exist", err: ErrNotExist, want: KindNotFound},
		{name: "not exist wrapped", err: fmt.Errorf("wrapped: %w", ErrNotExist), want: KindNotFound},
		{name: "exist", err: ErrExist, want: KindExist},
		{name: "exist in OpError", err: &OpError{Op: "create", Key: "/k", Err: ErrExist, Kind: KindExist}, want: KindExist},
		{name: "opError rejected", err: &OpError{Op: "put", Key: "/k", Err: errors.New("boom"), Kind: KindRejected}, want: KindRejected},
		{name: "closed client", err: errors.Trace(ErrClosedClient), want: KindOther},
		{name: "deadline", err: context.DeadlineExceeded, want: KindUnknown},
		{name: "deadline wrapped", err: fmt.Errorf("read: %w", context.DeadlineExceeded), want: KindUnknown},
		{name: "canceled", err: context.Canceled, want: KindUnknown},
		{name: "eof", err: io.EOF, want: KindUnknown},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, want: KindUnknown},
		{name: "grpc unavailable eof", err: status.Error(codes.Unavailable, "error reading from server: EOF"), want: KindUnknown},
		{name: "grpc deadline", err: status.Error(codes.DeadlineExceeded, "context deadline exceeded"), want: KindUnknown},
		{name: "grpc permission denied", err: status.Error(codes.PermissionDenied, "etcdserver: permission denied"), want: KindRejected},
		{name: "grpc unauthenticated", err: status.Error(codes.Unauthenticated, "auth"), want: KindRejected},
		{name: "grpc invalid argument", err: status.Error(codes.InvalidArgument, "etcdserver: permission not given"), want: KindRejected},
		{name: "grpc already exists", err: status.Error(codes.AlreadyExists, "exists"), want: KindExist},
		{name: "grpc not found", err: status.Error(codes.NotFound, "missing"), want: KindNotFound},
		{name: "grpc internal", err: status.Error(codes.Internal, "internal"), want: KindOther},
		{name: "rpctypes auth failed", err: rpctypes.ErrAuthFailed, want: KindRejected},
		{name: "rpctypes permission denied", err: rpctypes.ErrPermissionDenied, want: KindRejected},
		{name: "plain error", err: errors.New("boom"), want: KindOther},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.err); got != c.want {
				t.Fatalf("Classify(%v) = %s, want %s", c.err, got, c.want)
			}
		})
	}
	if !errors.Equal(ErrExist, &OpError{Err: ErrExist}) {
		t.Fatal("errors.Is should see ErrExist through OpError")
	}
}

func TestOpErrorRejected(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "rejected", err: &OpError{Op: "put", Key: "/k", Kind: KindRejected}, want: true},
		{name: "rejected wrapped", err: fmt.Errorf("wrapped: %w", &OpError{Op: "put", Key: "/k", Kind: KindRejected}), want: true},
		{name: "unknown", err: &OpError{Op: "put", Key: "/k", Kind: KindUnknown}, want: false},
		{name: "exist", err: &OpError{Op: "create", Key: "/k", Kind: KindExist}, want: false},
		{name: "not found", err: &OpError{Op: "read", Key: "/k", Kind: KindNotFound}, want: false},
		{name: "other", err: &OpError{Op: "read", Key: "/k", Kind: KindOther}, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var rejected interface{ Rejected() bool }
			ok := stderrors.As(c.err, &rejected) && rejected.Rejected()
			if ok != c.want {
				t.Fatalf("Rejected() of %v = %t, want %t", c.err, ok, c.want)
			}
		})
	}
	var nilErr *OpError
	if nilErr.Rejected() {
		t.Fatal("nil *OpError reported as rejected")
	}
}

func TestPreflightErrorKeepsCause(t *testing.T) {
	cause := fmt.Errorf("https://bad:1: %w", context.DeadlineExceeded)
	err := errors.Errorf("no endpoint passed the tls preflight within 2s: %w",
		joinProbeCauses([]error{cause}))
	if !strings.Contains(err.Error(), "no endpoint passed the tls preflight") {
		t.Fatalf("preflight error = %v, want it to name the preflight", err)
	}
	if !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preflight error = %v, want the cause reachable through errors.Is", err)
	}

	var many []error
	for i := 0; i < 5; i++ {
		many = append(many, fmt.Errorf("https://e%d: %w", i, context.DeadlineExceeded))
	}
	merged := joinProbeCauses(many)
	if !stderrors.Is(merged, context.DeadlineExceeded) {
		t.Fatalf("merged causes = %v, want the causes reachable through errors.Is", merged)
	}
	for _, want := range []string{"e0", "e1", "e2", "2 more endpoint(s) failed"} {
		if !strings.Contains(merged.Error(), want) {
			t.Fatalf("merged causes = %q, want %q", merged.Error(), want)
		}
	}
	if strings.Contains(merged.Error(), "e3") {
		t.Fatalf("merged causes = %q, want only the first three endpoints", merged.Error())
	}
}

func TestDirectChildren(t *testing.T) {
	cases := []struct {
		name string
		path string
		keys []string
		want []string
	}{
		{
			name: "product isolation",
			path: "/codis3",
			keys: []string{"/codis3/foo/a", "/codis3/foo/b", "/codis3/foo-bar/c"},
			want: []string{"/codis3/foo", "/codis3/foo-bar"},
		},
		{
			name: "dedupe",
			path: "/codis3",
			keys: []string{"/codis3/foo/a", "/codis3/foo/b"},
			want: []string{"/codis3/foo"},
		},
		{
			name: "sorted",
			path: "/codis3",
			keys: []string{"/codis3/b/x", "/codis3/a/x", "/codis3/c/x"},
			want: []string{"/codis3/a", "/codis3/b", "/codis3/c"},
		},
		{
			name: "empty",
			path: "/codis3",
			keys: nil,
			want: nil,
		},
		{
			name: "nested path",
			path: "/codis3/foo",
			keys: []string{"/codis3/foo/a", "/codis3/foo-bar/b"},
			want: []string{"/codis3/foo/a"},
		},
		{
			name: "prefix itself skipped",
			path: "/codis3",
			keys: []string{"/codis3/"},
			want: nil,
		},
		{
			name: "foreign keys ignored",
			path: "/codis3",
			keys: []string{"/other/x"},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := directChildren(c.path, c.keys)
			if !equalStrings(got, c.want) {
				t.Fatalf("directChildren(%q, %v) = %v, want %v", c.path, c.keys, got, c.want)
			}
		})
	}
}
