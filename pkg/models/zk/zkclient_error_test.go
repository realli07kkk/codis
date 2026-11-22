// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package zkclient

import (
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/go-zookeeper/zk"

	codiserrors "github.com/CodisLabs/codis/pkg/utils/errors"
)

func TestKnownZookeeperErrorClassification(t *testing.T) {
	for _, sentinel := range []error{zk.ErrNoNode, zk.ErrNodeExists, zk.ErrNotEmpty} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			wrapped := codiserrors.Trace(fmt.Errorf("zk: %w", sentinel))
			if !isKnownZKError(wrapped) {
				t.Fatalf("isKnownZKError(%v) = false", wrapped)
			}
			multi := codiserrors.Trace(fmt.Errorf("shell: %w", fmt.Errorf("op: %w", sentinel)))
			if !isKnownZKError(multi) {
				t.Fatalf("isKnownZKError(%v) = false", multi)
			}
			if !stderrors.Is(multi, sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", multi, sentinel)
			}
		})
	}

	if isKnownZKError(nil) {
		t.Fatal("nil should not be a known zookeeper error")
	}
	if isKnownZKError(stderrors.New("unrelated")) {
		t.Fatal("unrelated error classified as zookeeper sentinel")
	}
}
