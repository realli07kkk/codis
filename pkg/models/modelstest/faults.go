// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package modelstest

import (
	"context"
	"sync"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
)

// Faults injects coordinator failures into a LockClient so tests can cover
// "failed before commit", "refused before commit" and "committed but the
// response was lost".
//
// Change the flags through Set, SetRejections or Clear: the wrapped client may
// be used from several goroutines, and the wrapper reads a snapshot under the
// same lock.
type Faults struct {
	mu sync.Mutex

	// FailCreateBeforeCommit makes CreateWithRevision fail without writing.
	FailCreateBeforeCommit bool
	// RejectCreate makes CreateWithRevision refuse the write without writing,
	// as a server that denies the permission would.
	RejectCreate bool
	// LoseCreateResponse writes the key, then reports an unknown outcome.
	LoseCreateResponse bool
	// FailReadBack makes ReadWithRevision fail with an unknown outcome.
	FailReadBack bool
	// RejectDelete makes DeleteByCreateRevision refuse the delete without
	// removing the key.
	RejectDelete bool
	// LoseDeleteResponse deletes the key, then reports an unknown outcome.
	LoseDeleteResponse bool
}

// Set replaces the create/read-back/delete outcome flags atomically. The
// definite-rejection flags are set through SetRejections, so callers that only
// exercise unknown outcomes stay unchanged.
func (f *Faults) Set(failCreateBeforeCommit, loseCreateResponse, failReadBack, loseDeleteResponse bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FailCreateBeforeCommit = failCreateBeforeCommit
	f.LoseCreateResponse = loseCreateResponse
	f.FailReadBack = failReadBack
	f.LoseDeleteResponse = loseDeleteResponse
}

// SetRejections replaces the definite-rejection flags atomically: RejectCreate
// refuses CreateWithRevision, RejectDelete refuses DeleteByCreateRevision.
// The returned errors implement models.Rejected, so the lock API must report
// them as-is instead of reading back.
func (f *Faults) SetRejections(rejectCreate, rejectDelete bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RejectCreate = rejectCreate
	f.RejectDelete = rejectDelete
}

// Clear disables every injected fault, restoring a healthy client.
func (f *Faults) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FailCreateBeforeCommit = false
	f.RejectCreate = false
	f.LoseCreateResponse = false
	f.FailReadBack = false
	f.RejectDelete = false
	f.LoseDeleteResponse = false
}

// faultState is a lock-free snapshot of Faults.
type faultState struct {
	failCreateBeforeCommit bool
	rejectCreate           bool
	loseCreateResponse     bool
	failReadBack           bool
	rejectDelete           bool
	loseDeleteResponse     bool
}

func (f *Faults) state() faultState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return faultState{
		failCreateBeforeCommit: f.FailCreateBeforeCommit,
		rejectCreate:           f.RejectCreate,
		loseCreateResponse:     f.LoseCreateResponse,
		failReadBack:           f.FailReadBack,
		rejectDelete:           f.RejectDelete,
		loseDeleteResponse:     f.LoseDeleteResponse,
	}
}

var _ models.LockClient = (*faultyClient)(nil)

// Faulty wraps inner so the enabled faults fail the calls made through the
// returned client. Every other method is delegated unchanged.
func Faulty(inner models.LockClient, faults *Faults) models.LockClient {
	if faults == nil {
		faults = &Faults{}
	}
	return &faultyClient{LockClient: inner, faults: faults}
}

type faultyClient struct {
	models.LockClient
	faults *Faults
}

// injectedUnknown mimics a request whose outcome cannot be observed: the
// deadline may have expired before or after the server applied it, so
// pkg/models must treat it as unknown and read the state back.
func injectedUnknown(op, detail string) error {
	return errors.Errorf("modelstest: injected %s %s: %w", op, detail, context.DeadlineExceeded)
}

// injectedRejected mimics a request the server refused before applying any
// change, for example for a missing permission. Reloading the state cannot
// tell the caller anything new, so pkg/models must report the reason instead
// of reading back.
type injectedRejected struct {
	op     string
	detail string
}

func (e *injectedRejected) Error() string {
	return "modelstest: injected " + e.op + " " + e.detail + ": permission denied"
}

// Rejected implements models.Rejected.
func (e *injectedRejected) Rejected() bool { return true }

func (c *faultyClient) CreateWithRevision(path string, data []byte) (bool, int64, error) {
	st := c.faults.state()
	if st.rejectCreate {
		return false, 0, &injectedRejected{op: "create", detail: "refusal"}
	}
	if st.failCreateBeforeCommit {
		return false, 0, injectedUnknown("create", "failure before commit")
	}
	succeeded, createRevision, err := c.LockClient.CreateWithRevision(path, data)
	if err != nil || !succeeded || !st.loseCreateResponse {
		return succeeded, createRevision, err
	}
	return false, 0, injectedUnknown("create", "response loss")
}

func (c *faultyClient) ReadWithRevision(path string) ([]byte, int64, int64, int64, error) {
	if c.faults.state().failReadBack {
		return nil, 0, 0, 0, injectedUnknown("read-back", "failure")
	}
	return c.LockClient.ReadWithRevision(path)
}

func (c *faultyClient) DeleteByCreateRevision(path string, createRevision int64) (bool, error) {
	st := c.faults.state()
	if st.rejectDelete {
		return false, &injectedRejected{op: "delete", detail: "refusal"}
	}
	succeeded, err := c.LockClient.DeleteByCreateRevision(path, createRevision)
	if err != nil || !succeeded || !st.loseDeleteResponse {
		return succeeded, err
	}
	return false, injectedUnknown("delete", "response loss")
}
