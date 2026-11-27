// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	stderrors "errors"
	"strings"
	"testing"
	"time"
)

// TestIntegrationLeasePrimitives pins the lease primitives the online
// registration is built on: a granted lease reports its TTL, a version==0
// transaction binds a key to it, a second bind on the same key reports a
// compare failure, and a revoke removes the key immediately.
func TestIntegrationLeasePrimitives(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})

	const key = "/codis3/lease/online-token"

	leaseID, err := c.GrantLease(2)
	if err != nil {
		t.Fatalf("grant lease: %v", err)
	}
	if leaseID == 0 {
		t.Fatal("grant lease returned ID 0")
	}
	ttl, err := c.TimeToLive(leaseID)
	if err != nil {
		t.Fatalf("time to live: %v", err)
	}
	if ttl < 1 || ttl > 2 {
		t.Fatalf("ttl of a fresh 2s lease = %d, want 1..2", ttl)
	}
	t.Logf("granted lease %d with ttl %d", leaseID, ttl)

	succeeded, err := c.PutWithLeaseIfAbsent(key, []byte(`{"token":"online-token","addr":"127.0.0.1:11080"}`), leaseID)
	if err != nil || !succeeded {
		t.Fatalf("first bind = %v, %v; want true, nil", succeeded, err)
	}

	again, err := c.PutWithLeaseIfAbsent(key, []byte(`{"token":"other","addr":"127.0.0.1:1"}`), leaseID)
	if err != nil {
		t.Fatalf("second bind: %v", err)
	}
	if again {
		t.Fatal("bind of an existing key succeeded")
	}
	if b, err := c.Read(key, true); err != nil || string(b) != `{"token":"online-token","addr":"127.0.0.1:11080"}` {
		t.Fatalf("key after the second bind = %q, %v; the existing record must win", b, err)
	}

	keyLease, exists, err := c.LeaseOfKey(key)
	if err != nil || !exists || keyLease != leaseID {
		t.Fatalf("lease of key = %d, %v, %v; want %d, true", keyLease, exists, err, leaseID)
	}
	if _, exists, err := c.LeaseOfKey("/codis3/lease/missing"); err != nil || exists {
		t.Fatalf("lease of a missing key = exists %v, %v; want false, nil", exists, err)
	}

	if err := c.RevokeLease(leaseID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if b, err := c.Read(key, false); err != nil || b != nil {
		t.Fatalf("key after revoke = %q, %v; want deleted", b, err)
	}
	if ttl, err := c.TimeToLive(leaseID); err != nil || ttl != -1 {
		t.Fatalf("ttl of a revoked lease = %d, %v; want -1, nil", ttl, err)
	}
	err = c.RevokeLease(leaseID)
	if err == nil {
		t.Fatal("revoking an unknown lease returned no error")
	}
	if got := Classify(err); got != KindNotFound {
		t.Fatalf("revoking an unknown lease classified as %s, want NotFound: %v", got, err)
	}
	if _, err := c.PutWithLeaseIfAbsent("/codis3/lease/after-revoke", []byte("v"), leaseID); err == nil {
		t.Fatal("binding to a revoked lease succeeded")
	}
}

// TestIntegrationLeaseTTLExpiresKey pins the crash behaviour the registry
// relies on: without any revoke, the server removes the key about one granted
// TTL after the last renewal.
func TestIntegrationLeaseTTLExpiresKey(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})

	const key = "/codis3/lease/expiring"
	leaseID, err := c.GrantLease(1)
	if err != nil {
		t.Fatalf("grant lease: %v", err)
	}
	if _, err := c.PutWithLeaseIfAbsent(key, []byte("v"), leaseID); err != nil {
		t.Fatalf("bind: %v", err)
	}

	start := time.Now()
	deadline := start.Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if _, exists, err := c.LeaseOfKey(key); err != nil {
			t.Fatalf("lease of key: %v", err)
		} else if !exists {
			t.Logf("key disappeared %s after the grant (granted ttl 1s, no renewal)", time.Since(start).Round(time.Millisecond))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("key still present %s after a 1s lease was granted", time.Since(start))
}

// TestIntegrationRangeWithPrefix pins the single range read the `all` mode
// refresh uses: every child of the prefix, values included, and nothing of a
// sibling path.
func TestIntegrationRangeWithPrefix(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})

	for _, key := range []string{
		"/codis3/rng/online-proxies/a",
		"/codis3/rng/online-proxies/b",
	} {
		if err := c.Create(key, []byte(strings.TrimPrefix(key, "/codis3/rng/online-proxies/"))); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}
	if err := c.Create("/codis3/rng/online-proxies-x/c", []byte("c")); err != nil {
		t.Fatalf("create sibling: %v", err)
	}

	kvs, err := c.RangeWithPrefix(t.Context(), "/codis3/rng/online-proxies")
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	if len(kvs) != 2 {
		t.Fatalf("range returned %d records, want 2", len(kvs))
	}
	if kvs[0].Key != "/codis3/rng/online-proxies/a" || string(kvs[0].Value) != "a" {
		t.Fatalf("first record = %+v", kvs[0])
	}
	if kvs[1].Key != "/codis3/rng/online-proxies/b" || string(kvs[1].Value) != "b" {
		t.Fatalf("second record = %+v", kvs[1])
	}

	if _, err := c.RangeWithPrefix(t.Context(), "/codis3/rng/empty"); err != nil {
		t.Fatalf("range of a missing prefix: %v", err)
	}
}

// TestIntegrationLeaseUnknownWhenStopped pins the classification the
// registration loop depends on: a lease call against a stopped cluster is
// unknown, never a definite refusal.
func TestIntegrationLeaseUnknownWhenStopped(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: time.Second,
	})
	leaseID, err := c.GrantLease(30)
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	s.stop()

	err = c.RevokeLease(leaseID)
	if err == nil {
		t.Fatal("revoke succeeded after etcd stopped")
	}
	if got := Classify(err); got != KindUnknown {
		t.Fatalf("revoke after stop classified as %s, want Unknown: %v", got, err)
	}
	var opErr *OpError
	if !stderrors.As(err, &opErr) || opErr.Op != "lease-revoke" {
		t.Fatalf("revoke error is not a lease-revoke OpError: %v", err)
	}
	if opErr.Unwrap() == nil {
		t.Fatal("revoke OpError does not keep the original error")
	}
}
