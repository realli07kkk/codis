// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/modelstest"
)

// newFaultyLockStore opens a store whose lock calls go through a fault
// injector, which is how tests reproduce "failed before commit" and
// "committed but the response was lost".
func newFaultyLockStore(t *testing.T, state *modelstest.State, faults *modelstest.Faults) (*models.Store, models.LockClient) {
	t.Helper()
	inner := state.NewClient()
	t.Cleanup(func() { _ = inner.Close() })
	client := modelstest.Faulty(inner, faults)
	return models.NewStore(client, lockProduct), client
}

func TestAcquireLockFailsBeforeCommit(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{FailCreateBeforeCommit: true}
	store, _ := newFaultyLockStore(t, state, faults)
	topom := newLockTopom(lockProduct, "127.0.0.1:3001")

	ownership, err := store.AcquireLock(topom)
	if ownership != nil {
		t.Fatalf("acquire returned ownership %+v for a failed commit", ownership)
	}
	if !stderrors.Is(err, models.ErrResultUnknown) {
		t.Fatalf("acquire error = %v, want ErrResultUnknown", err)
	}
	if stderrors.Is(err, models.ErrLockHeld) {
		t.Fatalf("failed commit reported as a lock conflict: %v", err)
	}
	if info, showErr := store.ShowLock(); showErr != nil || info != nil {
		t.Fatalf("lock after a failed commit = %+v, %v; want none", info, showErr)
	}

	// Once the coordinator answers again, the same process (same owner id)
	// takes the lock.
	faults.Clear()
	recovered, err := store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire after recovery: %v", err)
	}
	if recovered.CreateRevision <= 0 {
		t.Fatalf("create revision = %d, want a positive revision", recovered.CreateRevision)
	}
}

// failFirstCreateClient fails the first conditional create with an unknown
// outcome without writing, so the caller has to read back, find the key
// missing and retry once with the same owner id.
type failFirstCreateClient struct {
	models.LockClient
	mu     sync.Mutex
	failed bool
}

func (c *failFirstCreateClient) CreateWithRevision(path string, data []byte) (bool, int64, error) {
	c.mu.Lock()
	fail := !c.failed
	c.failed = true
	c.mu.Unlock()
	if fail {
		return false, 0, fmt.Errorf("injected create failure before commit: %w", context.DeadlineExceeded)
	}
	return c.LockClient.CreateWithRevision(path, data)
}

func TestAcquireLockRetriesOnceAfterMissingKey(t *testing.T) {
	state := modelstest.NewState()
	inner := state.NewClient()
	defer inner.Close()
	store := models.NewStore(&failFirstCreateClient{LockClient: inner}, lockProduct)
	topom := newLockTopom(lockProduct, "127.0.0.1:3002")

	ownership, err := store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire with one retry: %v", err)
	}
	if ownership.CreateRevision <= 0 {
		t.Fatalf("create revision = %d, want a positive revision", ownership.CreateRevision)
	}
	info := mustShowLock(t, store)
	if info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("lock create revision = %d, want %d", info.CreateRevision, ownership.CreateRevision)
	}
	if info.Topom.OwnerID != topom.OwnerID {
		t.Fatalf("retried create used owner %q, want the original %q", info.Topom.OwnerID, topom.OwnerID)
	}
}

func TestAcquireLockLostCreateResponse(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{LoseCreateResponse: true}
	store, _ := newFaultyLockStore(t, state, faults)
	topom := newLockTopom(lockProduct, "127.0.0.1:3003")

	ownership, err := store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire with a lost create response: %v", err)
	}
	if ownership.CreateRevision <= 0 {
		t.Fatalf("create revision = %d, want a positive revision", ownership.CreateRevision)
	}

	info := mustShowLock(t, store)
	if info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("read-back create revision = %d, want %d", info.CreateRevision, ownership.CreateRevision)
	}
	if info.Topom.OwnerID != topom.OwnerID {
		t.Fatalf("lock owner = %q, want %q", info.Topom.OwnerID, topom.OwnerID)
	}

	faults.Clear()
	if err := store.ReleaseLock(ownership); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if info, err := store.ShowLock(); err != nil || info != nil {
		t.Fatalf("lock after release = %+v, %v; want none", info, err)
	}
}

func TestAcquireLockUnknownWhenReadBackFails(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{LoseCreateResponse: true, FailReadBack: true}
	store, _ := newFaultyLockStore(t, state, faults)
	topom := newLockTopom(lockProduct, "127.0.0.1:3004")

	ownership, err := store.AcquireLock(topom)
	if ownership != nil {
		t.Fatalf("acquire returned ownership %+v without a successful read-back", ownership)
	}
	if !stderrors.Is(err, models.ErrResultUnknown) {
		t.Fatalf("acquire error = %v, want ErrResultUnknown", err)
	}
	if stderrors.Is(err, models.ErrLockHeld) {
		t.Fatalf("unconfirmed acquire reported as a lock conflict: %v", err)
	}

	// The unconfirmed create did commit. The process must not own it yet, and
	// a retry of the same process recovers it by owner id.
	faults.Clear()
	info := mustShowLock(t, store)
	if info.Topom.OwnerID != topom.OwnerID {
		t.Fatalf("unconfirmed lock owner = %q, want %q", info.Topom.OwnerID, topom.OwnerID)
	}
	recovered, err := store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire after read-back recovery: %v", err)
	}
	if recovered.CreateRevision != info.CreateRevision {
		t.Fatalf("recovered create revision = %d, want %d", recovered.CreateRevision, info.CreateRevision)
	}
}

func TestReleaseLockLostDeleteResponse(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{}
	store, _ := newFaultyLockStore(t, state, faults)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:3005"))

	faults.Set(false, false, false, true)
	err := store.ReleaseLock(ownership)
	if !stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("release with a lost delete response = %v, want ErrLockGone", err)
	}
	if stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("deleted lock reported as changed ownership: %v", err)
	}
	if info, showErr := store.ShowLock(); showErr != nil || info != nil {
		t.Fatalf("lock after the lost response = %+v, %v; want none", info, showErr)
	}
}

func TestReleaseLockStaleOwnerKeepsSuccessorLock(t *testing.T) {
	state := modelstest.NewState()
	storeA := newLockStore(t, state, lockProduct)
	storeB := newLockStore(t, state, lockProduct)
	topomA := newLockTopom(lockProduct, "127.0.0.1:3006")
	topomB := newLockTopom(lockProduct, "127.0.0.1:3007")

	ownA := mustAcquireLock(t, storeA, topomA)
	// Operator recovery removes A's lock; a new owner then takes it over.
	if err := storeA.RemoveLock(ownA.CreateRevision); err != nil {
		t.Fatalf("remove lock of the stale owner: %v", err)
	}
	ownB := mustAcquireLock(t, storeB, topomB)

	err := storeA.ReleaseLock(ownA)
	if !stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("late release of a stale owner = %v, want ErrLockOwnershipChanged", err)
	}
	info := mustShowLock(t, storeB)
	if info.CreateRevision != ownB.CreateRevision || info.Topom.OwnerID != ownB.OwnerID {
		t.Fatalf("successor lock = %+v, want create revision %d and owner %q",
			info, ownB.CreateRevision, ownB.OwnerID)
	}
	if info.Topom.AdminAddr != topomB.AdminAddr {
		t.Fatalf("successor admin_addr = %q, want %q", info.Topom.AdminAddr, topomB.AdminAddr)
	}
}

func TestRemoveLockLostDeleteResponse(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{}
	store, _ := newFaultyLockStore(t, state, faults)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:3008"))

	faults.Set(false, false, false, true)
	err := store.RemoveLock(ownership.CreateRevision)
	if !stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("remove with a lost delete response = %v, want ErrLockGone", err)
	}
	if info, showErr := store.ShowLock(); showErr != nil || info != nil {
		t.Fatalf("lock after the lost response = %+v, %v; want none", info, showErr)
	}
}

func TestRemoveLockStaleRevisionKeepsSuccessorLock(t *testing.T) {
	state := modelstest.NewState()
	storeA := newLockStore(t, state, lockProduct)
	storeB := newLockStore(t, state, lockProduct)
	topomB := newLockTopom(lockProduct, "127.0.0.1:3009")

	ownA := mustAcquireLock(t, storeA, newLockTopom(lockProduct, "127.0.0.1:3010"))
	if err := storeA.RemoveLock(ownA.CreateRevision); err != nil {
		t.Fatalf("remove lock of the previous owner: %v", err)
	}
	ownB := mustAcquireLock(t, storeB, topomB)

	err := storeA.RemoveLock(ownA.CreateRevision)
	if !stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("remove with a stale revision = %v, want ErrLockOwnershipChanged", err)
	}
	info := mustShowLock(t, storeB)
	if info.CreateRevision != ownB.CreateRevision {
		t.Fatalf("successor create revision = %d, want the untouched %d",
			info.CreateRevision, ownB.CreateRevision)
	}
}

// mustBeRejected fails unless err is a definite rejection that names its
// reason, and asserts it was not degraded into an unknown outcome.
func mustBeRejected(t *testing.T, err error, forbidden ...error) {
	t.Helper()
	if err == nil {
		t.Fatal("write was refused but no error was returned")
	}
	if !models.IsRejected(err) {
		t.Fatalf("error = %v, want a definite rejection (models.IsRejected)", err)
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("error = %v, want it to label the refusal", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v, want the refusal reason", err)
	}
	for _, forbidden := range forbidden {
		if stderrors.Is(err, forbidden) {
			t.Fatalf("rejection reported as %v: %v", forbidden, err)
		}
	}
}

func TestAcquireLockRejectedCreateIsNotReadBack(t *testing.T) {
	state := modelstest.NewState()
	// FailReadBack proves the point: a read-back would turn the result into
	// ErrResultUnknown, so a rejection without it means nothing was re-read.
	faults := &modelstest.Faults{RejectCreate: true, FailReadBack: true}
	store, _ := newFaultyLockStore(t, state, faults)
	topom := newLockTopom(lockProduct, "127.0.0.1:3011")

	ownership, err := store.AcquireLock(topom)
	if ownership != nil {
		t.Fatalf("acquire returned ownership %+v for a refused create", ownership)
	}
	mustBeRejected(t, err, models.ErrLockHeld, models.ErrResultUnknown)

	// The refusal wrote nothing, and the process does not own the lock.
	faults.Clear()
	if info, showErr := store.ShowLock(); showErr != nil || info != nil {
		t.Fatalf("lock after a refused acquire = %+v, %v; want none", info, showErr)
	}
	ownership, err = store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire after the refusal cleared: %v", err)
	}
	if ownership.CreateRevision <= 0 {
		t.Fatalf("create revision = %d, want a positive revision", ownership.CreateRevision)
	}
}

func TestReleaseLockRejectedDeleteIsNotReadBack(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{}
	store, _ := newFaultyLockStore(t, state, faults)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:3012"))

	faults.SetRejections(false, true)
	faults.Set(false, false, true, false) // a read-back would be unconfirmable
	err := store.ReleaseLock(ownership)
	mustBeRejected(t, err, models.ErrLockGone, models.ErrLockOwnershipChanged, models.ErrResultUnknown)

	// The refused delete removed nothing: the lock still belongs to the owner.
	faults.Clear()
	info := mustShowLock(t, store)
	if info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("lock create revision after a refused release = %d, want %d",
			info.CreateRevision, ownership.CreateRevision)
	}
	if err := store.ReleaseLock(ownership); err != nil {
		t.Fatalf("release after the refusal cleared: %v", err)
	}
}

func TestRemoveLockRejectedDeleteIsNotReadBack(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{}
	store, _ := newFaultyLockStore(t, state, faults)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:3013"))

	faults.SetRejections(false, true)
	faults.Set(false, false, true, false) // a read-back would be unconfirmable
	err := store.RemoveLock(ownership.CreateRevision)
	mustBeRejected(t, err, models.ErrLockGone, models.ErrLockOwnershipChanged, models.ErrResultUnknown)

	faults.Clear()
	info := mustShowLock(t, store)
	if info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("lock create revision after a refused remove = %d, want %d",
			info.CreateRevision, ownership.CreateRevision)
	}
	if err := store.RemoveLock(ownership.CreateRevision); err != nil {
		t.Fatalf("remove after the refusal cleared: %v", err)
	}
}

// TestRejectedReadBackStaysUnknown keeps the read-back rule: a refused read
// cannot confirm anything, so the outcome stays unknown and must not be
// reported as a definite refusal.
func TestRejectedReadBackStaysUnknown(t *testing.T) {
	state := modelstest.NewState()
	faults := &modelstest.Faults{LoseCreateResponse: true}
	inner := state.NewClient()
	t.Cleanup(func() { _ = inner.Close() })
	client := modelstest.Faulty(&rejectedReadClient{LockClient: inner}, faults)
	store := models.NewStore(client, lockProduct)

	_, err := store.AcquireLock(newLockTopom(lockProduct, "127.0.0.1:3014"))
	if !stderrors.Is(err, models.ErrResultUnknown) {
		t.Fatalf("acquire with a refused read-back = %v, want ErrResultUnknown", err)
	}
	if models.IsRejected(err) {
		t.Fatalf("unknown result reported as a definite rejection: %v", err)
	}
}

// rejectedReadClient refuses every read with a models.Rejected error.
type rejectedReadClient struct {
	models.LockClient
}

func (c *rejectedReadClient) ReadWithRevision(path string) ([]byte, int64, int64, int64, error) {
	return nil, 0, 0, 0, &rejectedReadError{}
}

type rejectedReadError struct{}

func (e *rejectedReadError) Error() string {
	return "modelstest: injected read refusal: permission denied"
}

func (e *rejectedReadError) Rejected() bool { return true }
