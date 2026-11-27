// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
)

func newIntegrationLockTopom(product, adminAddr string) *models.Topom {
	return &models.Topom{
		Token:       "topom-token",
		StartTime:   time.Now().String(),
		AdminAddr:   adminAddr,
		ProductName: product,
		Pid:         42,
		Pwd:         "/tmp",
		Sys:         "integration",
	}
}

// TestLockIntegrationOwnership covers the persistent topom lock against a real
// etcd: exclusive acquisition, create_revision ownership, operator recovery
// and the stale-owner release that must never remove a successor's lock.
func TestLockIntegrationOwnership(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	cfg := &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	}
	clientA := newTestClient(t, cfg)
	clientB := newTestClient(t, cfg)

	const product = "lock_integration"
	storeA := models.NewStore(clientA, product)
	storeB := models.NewStore(clientB, product)
	topomA := newIntegrationLockTopom(product, "127.0.0.1:1001")
	topomB := newIntegrationLockTopom(product, "127.0.0.1:1002")

	ownA, err := storeA.AcquireLock(topomA)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if ownA.OwnerID == "" || ownA.CreateRevision <= 0 {
		t.Fatalf("ownership = %+v, want an owner id and a positive revision", ownA)
	}

	info, err := storeA.ShowLock()
	if err != nil || info == nil {
		t.Fatalf("show lock = %+v, %v", info, err)
	}
	if info.CreateRevision != ownA.CreateRevision {
		t.Fatalf("show lock create revision = %d, want %d", info.CreateRevision, ownA.CreateRevision)
	}
	if info.Topom.OwnerID != topomA.OwnerID || info.Topom.AdminAddr != topomA.AdminAddr {
		t.Fatalf("show lock topom = %+v, want owner %q and admin_addr %q",
			info.Topom, topomA.OwnerID, topomA.AdminAddr)
	}
	if info.Lease != 0 {
		t.Fatalf("show lock lease = %d, want 0 (the lock is persistent)", info.Lease)
	}

	ownB, err := storeB.AcquireLock(topomB)
	if ownB != nil {
		t.Fatalf("conflicting acquire returned ownership %+v", ownB)
	}
	if !stderrors.Is(err, models.ErrLockHeld) {
		t.Fatalf("conflicting acquire = %v, want ErrLockHeld", err)
	}
	if stderrors.Is(err, models.ErrResultUnknown) {
		t.Fatalf("lock conflict reported as an unknown result: %v", err)
	}

	// Operator recovery removes the observed revision; a new owner follows.
	if err := storeA.RemoveLock(ownA.CreateRevision); err != nil {
		t.Fatalf("remove lock: %v", err)
	}
	if info, err := storeA.ShowLock(); err != nil || info != nil {
		t.Fatalf("show lock after removal = %+v, %v; want none", info, err)
	}

	ownB, err = storeB.AcquireLock(topomB)
	if err != nil {
		t.Fatalf("acquire after recovery: %v", err)
	}
	if ownB.CreateRevision <= ownA.CreateRevision {
		t.Fatalf("successor create revision = %d, want > %d", ownB.CreateRevision, ownA.CreateRevision)
	}
	if ownB.OwnerID == ownA.OwnerID {
		t.Fatalf("successor reused the owner id %q", ownB.OwnerID)
	}

	// The stale owner's late release and remove must both fail without
	// touching the successor's lock.
	if err := storeA.ReleaseLock(ownA); !stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("stale release = %v, want ErrLockOwnershipChanged", err)
	}
	if err := storeA.RemoveLock(ownA.CreateRevision); !stderrors.Is(err, models.ErrLockOwnershipChanged) {
		t.Fatalf("stale remove = %v, want ErrLockOwnershipChanged", err)
	}
	info, err = storeB.ShowLock()
	if err != nil || info == nil {
		t.Fatalf("show successor lock = %+v, %v", info, err)
	}
	if info.CreateRevision != ownB.CreateRevision || info.Topom.OwnerID != ownB.OwnerID {
		t.Fatalf("successor lock = %+v, want create revision %d and owner %q",
			info, ownB.CreateRevision, ownB.OwnerID)
	}

	if err := storeB.ReleaseLock(ownB); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	if info, err := storeB.ShowLock(); err != nil || info != nil {
		t.Fatalf("show lock after release = %+v, %v; want none", info, err)
	}
	if err := storeB.ReleaseLock(ownB); !stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("release of a missing lock = %v, want ErrLockGone", err)
	}
	if err := storeB.RemoveLock(ownB.CreateRevision); !stderrors.Is(err, models.ErrLockGone) {
		t.Fatalf("remove of a missing lock = %v, want ErrLockGone", err)
	}
}

// TestLockIntegrationUnknownAfterStop checks that an unreachable etcd produces
// an unconfirmed result, never a lock conflict and never an assumed success.
func TestLockIntegrationUnknownAfterStop(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})
	store := models.NewStore(c, "lock_stopped")
	s.stop()

	start := time.Now()
	ownership, err := store.AcquireLock(newIntegrationLockTopom("lock_stopped", "127.0.0.1:1003"))
	elapsed := time.Since(start)

	if ownership != nil {
		t.Fatalf("acquire returned ownership %+v with etcd stopped", ownership)
	}
	if err == nil {
		t.Fatal("acquire succeeded with etcd stopped")
	}
	if stderrors.Is(err, models.ErrLockHeld) {
		t.Fatalf("stopped coordinator reported as a lock conflict: %v", err)
	}
	if !stderrors.Is(err, models.ErrResultUnknown) {
		t.Fatalf("acquire error = %v, want ErrResultUnknown", err)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("acquire took %s to report an unknown result", elapsed)
	}
}
