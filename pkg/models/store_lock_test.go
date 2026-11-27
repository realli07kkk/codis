// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models_test

import (
	"encoding/json"
	stderrors "errors"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/modelstest"
)

const lockProduct = "lock_test"

// newLockStore opens a store over a fresh client of state, which is how tests
// emulate one Dashboard process.
func newLockStore(t *testing.T, state *modelstest.State, product string) *models.Store {
	t.Helper()
	client := state.NewClient()
	t.Cleanup(func() { _ = client.Close() })
	return models.NewStore(client, product)
}

func newLockTopom(product, adminAddr string) *models.Topom {
	return &models.Topom{
		Token:       "topom-token",
		StartTime:   "2026-11-27T00:00:00Z",
		AdminAddr:   adminAddr,
		ProductName: product,
		Pid:         42,
		Pwd:         "/tmp",
		Sys:         "test",
	}
}

func mustAcquireLock(t *testing.T, store *models.Store, topom *models.Topom) *models.LockOwnership {
	t.Helper()
	ownership, err := store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if ownership == nil {
		t.Fatal("acquire lock returned nil ownership")
	}
	return ownership
}

func mustShowLock(t *testing.T, store *models.Store) *models.LockInfo {
	t.Helper()
	info, err := store.ShowLock()
	if err != nil {
		t.Fatalf("show lock: %v", err)
	}
	if info == nil {
		t.Fatal("show lock returned no lock")
	}
	return info
}

func TestAcquireLockSucceeds(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	topom := newLockTopom(lockProduct, "127.0.0.1:12345")

	ownership, err := store.AcquireLock(topom)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if ownership.OwnerID == "" {
		t.Fatal("ownership owner id is empty")
	}
	if topom.OwnerID != ownership.OwnerID {
		t.Fatalf("owner id = %q, want the generated id written back to the topom %q",
			topom.OwnerID, ownership.OwnerID)
	}
	if ownership.CreateRevision <= 0 {
		t.Fatalf("create revision = %d, want a positive revision", ownership.CreateRevision)
	}

	info := mustShowLock(t, store)
	if info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("lock create revision = %d, want %d", info.CreateRevision, ownership.CreateRevision)
	}
	if info.ModRevision != ownership.CreateRevision {
		t.Fatalf("lock mod revision = %d, want %d", info.ModRevision, ownership.CreateRevision)
	}
	if info.Lease != 0 {
		t.Fatalf("lock lease = %d, want 0 (the lock is persistent)", info.Lease)
	}
	if info.Topom.OwnerID != ownership.OwnerID {
		t.Fatalf("lock owner id = %q, want %q", info.Topom.OwnerID, ownership.OwnerID)
	}
	if info.Topom.AdminAddr != topom.AdminAddr || info.Topom.ProductName != lockProduct {
		t.Fatalf("lock topom = %+v, want admin_addr %q and product %q",
			info.Topom, topom.AdminAddr, lockProduct)
	}

	// The stored value keeps the Topom JSON and only adds owner_id.
	raw, err := json.Marshal(info.Topom)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["owner_id"] != ownership.OwnerID {
		t.Fatalf("encoded owner_id = %v, want %q", decoded["owner_id"], ownership.OwnerID)
	}
	for _, field := range []string{"token", "start_time", "admin_addr", "product_name", "pid", "pwd", "sys"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("encoded lock is missing %q: %v", field, decoded)
		}
	}
}

func TestAcquireLockConflict(t *testing.T) {
	state := modelstest.NewState()
	storeA := newLockStore(t, state, lockProduct)
	storeB := newLockStore(t, state, lockProduct)
	topomA := newLockTopom(lockProduct, "127.0.0.1:1001")
	topomB := newLockTopom(lockProduct, "127.0.0.1:1002")

	ownA := mustAcquireLock(t, storeA, topomA)

	ownership, err := storeB.AcquireLock(topomB)
	if ownership != nil {
		t.Fatalf("second acquire returned ownership %+v", ownership)
	}
	if !stderrors.Is(err, models.ErrLockHeld) {
		t.Fatalf("second acquire error = %v, want ErrLockHeld", err)
	}
	if stderrors.Is(err, models.ErrResultUnknown) {
		t.Fatalf("lock conflict reported as an unknown result: %v", err)
	}

	info := mustShowLock(t, storeA)
	if info.CreateRevision != ownA.CreateRevision || info.Topom.OwnerID != ownA.OwnerID {
		t.Fatalf("lock changed after a conflicting acquire: %+v", info)
	}
	if info.Topom.AdminAddr != topomA.AdminAddr {
		t.Fatalf("lock admin_addr = %q, want %q", info.Topom.AdminAddr, topomA.AdminAddr)
	}
}

func TestAcquireLockSameOwnerRecoversRevision(t *testing.T) {
	state := modelstest.NewState()
	storeA := newLockStore(t, state, lockProduct)
	storeB := newLockStore(t, state, lockProduct)
	topom := newLockTopom(lockProduct, "127.0.0.1:1001")

	first := mustAcquireLock(t, storeA, topom)

	// A retry of the same process uses the same owner id, so an existing
	// record that carries it is the caller's own lock, not a conflict.
	again, err := storeB.AcquireLock(topom)
	if err != nil {
		t.Fatalf("re-acquire with the same owner id: %v", err)
	}
	if again.OwnerID != first.OwnerID {
		t.Fatalf("owner id = %q, want %q", again.OwnerID, first.OwnerID)
	}
	if again.CreateRevision != first.CreateRevision {
		t.Fatalf("create revision = %d, want the existing %d", again.CreateRevision, first.CreateRevision)
	}
}

func TestAcquireLockIsolatesProducts(t *testing.T) {
	state := modelstest.NewState()
	storeFoo := newLockStore(t, state, "lock_foo")
	storeFooBar := newLockStore(t, state, "lock_foo-bar")

	ownFoo := mustAcquireLock(t, storeFoo, newLockTopom("lock_foo", "127.0.0.1:2001"))
	ownFooBar := mustAcquireLock(t, storeFooBar, newLockTopom("lock_foo-bar", "127.0.0.1:2002"))

	if info := mustShowLock(t, storeFoo); info.Topom.OwnerID != ownFoo.OwnerID {
		t.Fatalf("lock_foo owner = %q, want %q", info.Topom.OwnerID, ownFoo.OwnerID)
	}
	if info := mustShowLock(t, storeFooBar); info.Topom.OwnerID != ownFooBar.OwnerID {
		t.Fatalf("lock_foo-bar owner = %q, want %q", info.Topom.OwnerID, ownFooBar.OwnerID)
	}
}

func TestReleaseLock(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:1001"))

	if err := store.ReleaseLock(ownership); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if info, err := store.ShowLock(); err != nil || info != nil {
		t.Fatalf("lock after release = %+v, %v; want none", info, err)
	}

	// The key is free again, and the next owner gets a new revision.
	storeB := newLockStore(t, state, lockProduct)
	next := mustAcquireLock(t, storeB, newLockTopom(lockProduct, "127.0.0.1:1002"))
	if next.CreateRevision <= ownership.CreateRevision {
		t.Fatalf("next create revision = %d, want > %d", next.CreateRevision, ownership.CreateRevision)
	}
}

func TestReleaseLockOwnershipChanged(t *testing.T) {
	state := modelstest.NewState()
	storeA := newLockStore(t, state, lockProduct)
	ownA := mustAcquireLock(t, storeA, newLockTopom(lockProduct, "127.0.0.1:1001"))

	// Another actor replaces the record: the key is recreated from scratch,
	// which is what an operator recovery followed by a new owner looks like.
	client := state.NewClient()
	defer client.Close()
	if err := client.Delete(models.LockPath(lockProduct)); err != nil {
		t.Fatal(err)
	}
	topomB := newLockTopom(lockProduct, "127.0.0.1:1002")
	ownB := mustAcquireLock(t, models.NewStore(client, lockProduct), topomB)

	err := storeA.ReleaseLock(ownA)
	if !stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("release after replacement = %v, want ErrLockOwnershipChanged", err)
	}
	if stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("release after replacement also reported ErrLockGone: %v", err)
	}

	info := mustShowLock(t, storeA)
	if info.CreateRevision != ownB.CreateRevision || info.Topom.OwnerID != ownB.OwnerID {
		t.Fatalf("successor lock = %+v, want create revision %d and owner %q",
			info, ownB.CreateRevision, ownB.OwnerID)
	}
	if info.Topom.AdminAddr != topomB.AdminAddr {
		t.Fatalf("successor admin_addr = %q, want %q", info.Topom.AdminAddr, topomB.AdminAddr)
	}
}

func TestReleaseLockGone(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:1001"))

	client := state.NewClient()
	defer client.Close()
	if err := client.Delete(models.LockPath(lockProduct)); err != nil {
		t.Fatal(err)
	}

	err := store.ReleaseLock(ownership)
	if !stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("release of a missing lock = %v, want ErrLockGone", err)
	}
	if stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("missing lock reported as changed ownership: %v", err)
	}
}

func TestReleaseLockRejectsInvalidOwnership(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	for _, ownership := range []*models.LockOwnership{
		nil,
		{OwnerID: "owner", CreateRevision: 0},
		{OwnerID: "owner", CreateRevision: -1},
	} {
		if err := store.ReleaseLock(ownership); err == nil {
			t.Fatalf("release with %+v succeeded, want a validation error", ownership)
		}
	}
}

func TestShowLockEmpty(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	info, err := store.ShowLock()
	if err != nil || info != nil {
		t.Fatalf("show lock of an empty product = %+v, %v; want nil, nil", info, err)
	}
}

func TestRemoveLock(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:1001"))
	info := mustShowLock(t, store)

	if err := store.RemoveLock(info.CreateRevision); err != nil {
		t.Fatalf("remove lock: %v", err)
	}
	if info, err := store.ShowLock(); err != nil || info != nil {
		t.Fatalf("lock after removal = %+v, %v; want none", info, err)
	}

	// A second call finds no lock at all.
	if err := store.RemoveLock(ownership.CreateRevision); !stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("remove missing lock = %v, want ErrLockGone", err)
	}
}

func TestRemoveLockMismatch(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:1001"))

	err := store.RemoveLock(ownership.CreateRevision + 1)
	if !stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("remove with a wrong revision = %v, want ErrLockOwnershipChanged", err)
	}
	if stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("wrong revision reported as a missing lock: %v", err)
	}

	info := mustShowLock(t, store)
	if info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("lock create revision = %d, want the untouched %d",
			info.CreateRevision, ownership.CreateRevision)
	}
}

func TestRemoveLockRejectsNonPositiveRevision(t *testing.T) {
	state := modelstest.NewState()
	store := newLockStore(t, state, lockProduct)
	ownership := mustAcquireLock(t, store, newLockTopom(lockProduct, "127.0.0.1:1001"))

	for _, revision := range []int64{0, -1} {
		err := store.RemoveLock(revision)
		if err == nil {
			t.Fatalf("remove with revision %d succeeded, want a validation error", revision)
		}
		if stderrors.Is(err, models.ErrLockGone) || stderrors.Is(err, models.ErrLockOwnershipChanged) ||
			stderrors.Is(err, models.ErrResultUnknown) {
			t.Fatalf("remove with revision %d classified as %v, want an argument error", revision, err)
		}
	}
	if info := mustShowLock(t, store); info.CreateRevision != ownership.CreateRevision {
		t.Fatalf("lock create revision = %d, want %d", info.CreateRevision, ownership.CreateRevision)
	}
}

// legacyClient hides the optional LockClient methods, like a coordinator
// driver that only implements models.Client.
type legacyClient struct {
	models.Client
}

func TestLockUnsupported(t *testing.T) {
	client := modelstest.NewState().NewClient()
	defer client.Close()
	store := models.NewStore(legacyClient{client}, lockProduct)

	if _, err := store.AcquireLock(newLockTopom(lockProduct, "127.0.0.1:1001")); !stderrors.Is(err, models.ErrLockUnsupported) {
		t.Fatalf("acquire lock = %v, want ErrLockUnsupported", err)
	}
	if err := store.ReleaseLock(&models.LockOwnership{OwnerID: "owner", CreateRevision: 1}); !stderrors.Is(err, models.ErrLockUnsupported) {
		t.Fatalf("release lock = %v, want ErrLockUnsupported", err)
	}
	if _, err := store.ShowLock(); !stderrors.Is(err, models.ErrLockUnsupported) {
		t.Fatalf("show lock = %v, want ErrLockUnsupported", err)
	}
	if err := store.RemoveLock(1); !stderrors.Is(err, models.ErrLockUnsupported) {
		t.Fatalf("remove lock = %v, want ErrLockUnsupported", err)
	}
}
