// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package rpc

import (
	stderrors "errors"
	"fmt"
	"testing"

	codiserrors "github.com/CodisLabs/codis/pkg/utils/errors"
)

func TestNewRemoteErrorPreservesCauseAndStack(t *testing.T) {
	if err := NewRemoteError(nil); err != nil {
		t.Fatalf("NewRemoteError(nil) = %v, want nil", err)
	}

	local := codiserrors.Trace(stderrors.New("boom"))
	remote := NewRemoteError(local)
	if remote.Cause != "boom" {
		t.Fatalf("Cause = %q, want %q", remote.Cause, "boom")
	}
	if remote.Stack == nil {
		t.Fatal("remote error lost the local stack")
	}

	traced := remote.TracedError()
	if got, want := traced.Error(), "[Remote Error] boom"; got != want {
		t.Fatalf("TracedError().Error() = %q, want %q", got, want)
	}
	if codiserrors.Stack(traced) == nil {
		t.Fatal("TracedError() lost the remote stack")
	}
	if stderrors.Is(traced, local) {
		t.Fatal("remote errors must not expose the local error chain")
	}
}

func TestRemoteErrorClassificationSurvivesWrapping(t *testing.T) {
	remote := &RemoteError{Cause: "remote boom"}
	wrapped := fmt.Errorf("rpc call: %w", remote)

	if !stderrors.Is(wrapped, remote) {
		t.Fatal("errors.Is should match the wrapped *RemoteError")
	}
	var target *RemoteError
	if !stderrors.As(wrapped, &target) {
		t.Fatal("errors.As should find the wrapped *RemoteError")
	}
	if target != remote {
		t.Fatalf("errors.As target = %p, want %p", target, remote)
	}

	if got := NewRemoteError(remote); got != remote {
		t.Fatalf("NewRemoteError should return an existing *RemoteError, got %v", got)
	}
	converted := NewRemoteError(wrapped)
	if converted == remote {
		t.Fatal("NewRemoteError should flatten a non-RemoteError wrapper")
	}
	if converted.Cause != "rpc call: remote boom" {
		t.Fatalf("converted Cause = %q", converted.Cause)
	}
}
