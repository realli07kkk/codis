// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package etcdonline publishes a proxy's online record and reads the online
// records of one product (contract §4).
//
// A registering proxy owns an etcd lease per process, binds the record
// /codis3/{product}/online-proxies/{token} to it with a create-if-absent
// transaction and renews it in the background. The record therefore disappears
// when the process stops (explicit revoke) or dies (server-side TTL), and a
// crashing instance never blocks a restart for longer than that TTL.
//
// The driver is injected as a small interface so the registration loop is
// testable without etcd, and so pkg/proxy never imports the etcd client. Both
// a ready driver (Client) and a factory (NewClient) are accepted: the factory
// keeps the connection lazy, so a proxy that only enabled the online
// registration goes online without waiting for the coordinator and retries the
// connection in the background (contract R2).
package etcdonline

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/etcd"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

const (
	// defaultTTL is the lease TTL requested by the first version of the online
	// registration (contract §4). It is deliberately not a tuning knob.
	defaultTTL = 30 * time.Second

	// defaultStopWait bounds Close's wait for the registration loop: one etcd
	// request timeout plus slack, so an in-flight lease call can finish.
	defaultStopWait = 6 * time.Second

	// defaultRetry* configure the exponential retry of the registration loop:
	// 1s, 2s, 4s ... capped at 30s, reset after a successful registration.
	defaultRetryMin = 1
	defaultRetryMax = 30
)

// ErrNoClient is returned by List before the connection exists, so the caller
// keeps its last snapshot instead of dropping every peer.
var ErrNoClient = stderrors.New("online registration client is not connected yet")

// LeaseClient is the driver surface the registry needs. *etcd.Client
// implements it; tests use a fake.
type LeaseClient interface {
	GrantLease(ttlSeconds int64) (leaseID int64, err error)
	KeepAlive(ctx context.Context, leaseID int64) (<-chan *clientv3.LeaseKeepAliveResponse, error)
	RevokeLease(leaseID int64) error
	TimeToLive(leaseID int64) (ttlSeconds int64, err error)
	PutWithLeaseIfAbsent(key string, value []byte, leaseID int64) (succeeded bool, err error)
	LeaseOfKey(key string) (leaseID int64, exists bool, err error)
	RangeWithPrefix(ctx context.Context, prefix string) ([]etcd.KeyValue, error)
	Close() error
}

// Options configures a Registry. Product, Token and Addr identify the record;
// Addr is the address clients use to reach this proxy.
type Options struct {
	// Client is a ready driver. When it is nil, Register calls NewClient
	// before the first registration round and retries with a bounded backoff
	// while it fails, so the connection is established lazily and a broken
	// coordinator configuration never delays going online.
	Client LeaseClient
	// NewClient builds the driver on demand. It must be safe to call again
	// after a failure and may perform the connection preflight.
	NewClient func() (LeaseClient, error)

	Product string
	Token   string
	Addr    string

	// TTL overrides the fixed 30s lease TTL. It exists for integration tests
	// that must observe the server-side expiry within seconds; deployment
	// code leaves it zero.
	TTL time.Duration
}

// Registry publishes one proxy's online record. It is created before the
// proxy accepts traffic, Register runs in its own goroutine, and Close owns
// the shutdown order.
type Registry struct {
	product string
	token   string
	addr    string
	key     string
	value   []byte

	// ttl, stopWait and retry are fixed in production; package tests override
	// them to shorten waits.
	ttl      time.Duration
	stopWait time.Duration
	retry    backoff

	mu        sync.Mutex
	client    LeaseClient
	newClient func() (LeaseClient, error)
	leaseID   int64
	cancel    context.CancelFunc
	done      chan struct{}
	closed    bool

	closeOnce sync.Once
	closeErr  error

	// onCloseWait is a test hook: Close calls it once the loop is being
	// stopped and just before the bounded wait on done, so a test can release
	// an in-flight fake call and pin the grant-races-Close window. It is nil
	// in production and must not block.
	onCloseWait func()
}

// New returns a registry for one proxy. It performs no I/O: the driver is
// only built by Register (through NewClient) and the first lease is requested
// after that, so a proxy can go online before etcd answers.
func New(opts Options) *Registry {
	r := &Registry{
		client:    opts.Client,
		newClient: opts.NewClient,
		product:   opts.Product,
		token:     opts.Token,
		addr:      opts.Addr,
		key:       models.OnlineProxyPath(opts.Product, opts.Token),
		ttl:       defaultTTL,
		stopWait:  defaultStopWait,
		retry:     backoff{minStep: defaultRetryMin, maxStep: defaultRetryMax, unit: time.Second},
	}
	if opts.TTL > 0 {
		r.ttl = opts.TTL
	}
	// models.OnlineRecord holds only strings, so marshalling cannot fail.
	r.value, _ = json.Marshal(models.OnlineRecord{Token: opts.Token, Addr: opts.Addr})
	return r
}

// Register publishes the online record and keeps it alive until ctx is done
// or Close is called. It blocks, so the caller runs it in a goroutine: going
// online never waits for the registration to succeed, and a failure is retried
// with a bounded backoff.
func (r *Registry) Register(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	if r.closed || r.done != nil {
		r.mu.Unlock()
		cancel()
		log.Warnf("online registration of %s: already running or closed", r.key)
		return
	}
	r.cancel = cancel
	r.done = make(chan struct{})
	done := r.done
	r.mu.Unlock()

	defer func() {
		cancel()
		close(done)
	}()

	// A successful registration resets the delay, so the first retry after any
	// interruption always waits at least the minimum: a keepalive stream that
	// closes immediately cannot turn into a hot loop.
	for {
		if ctx.Err() != nil {
			return
		}
		if err := r.connect(); err != nil {
			if ctx.Err() != nil {
				return
			}
			delay := r.retry.next()
			log.Warnf("online registration %s unavailable: %v; retrying in %s", r.key, err, delay)
			if !waitCtx(ctx, delay) {
				return
			}
			continue
		}
		leaseID, ok := r.ensureOwnership(ctx)
		if ok {
			r.retry.reset()
			r.keepAlive(ctx, leaseID)
		}
		delay := r.retry.next()
		if !ok {
			log.Warnf("online registration %s: not registered yet; retrying in %s", r.key, delay)
		}
		if !waitCtx(ctx, delay) {
			return
		}
	}
}

// connect makes sure a driver exists. The factory is called without holding
// r.mu, because the connection preflight may block for a dial timeout; a
// driver that arrives after Close is closed right away instead of leaking.
func (r *Registry) connect() error {
	r.mu.Lock()
	if r.client != nil {
		r.mu.Unlock()
		return nil
	}
	factory, closed := r.newClient, r.closed
	r.mu.Unlock()

	switch {
	case closed:
		return context.Canceled
	case factory == nil:
		return stderrors.New("no etcd client was injected")
	}
	client, err := factory()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Close may have run, or another round may have won the race.
	if r.closed || r.client != nil {
		_ = client.Close()
		if r.closed {
			return context.Canceled
		}
		return nil
	}
	r.client = client
	log.Infof("online registration %s: connected to the coordinator", r.key)
	return nil
}

// driver returns the current client under r.mu; it is nil before the
// connection exists and again after Close, which clears it so a late List
// reports ErrNoClient instead of using a client that is being torn down.
func (r *Registry) driver() LeaseClient {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

// keepAlive renews leaseID until the stream closes or ctx is done. A stream
// that closes (or a direct error at call time) means the lease can no longer
// be trusted: ownership must be re-confirmed before the record counts as
// published again.
func (r *Registry) keepAlive(ctx context.Context, leaseID int64) {
	client := r.driver()
	if client == nil {
		return
	}
	ch, err := client.KeepAlive(ctx, leaseID)
	if err != nil {
		log.WarnErrorf(err, "online registration %s: keepalive of lease %d failed", r.key, leaseID)
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				log.Warnf("online registration %s: keepalive stream of lease %d closed; re-confirming ownership", r.key, leaseID)
				return
			}
		}
	}
}

// ensureOwnership makes sure the key is bound to a lease this process owns.
// It returns that lease, or ok=false when the round must be retried. It never
// overwrites or claims a key held by another lease: such a record is left
// alone until its owner exits or the server expires it.
func (r *Registry) ensureOwnership(ctx context.Context) (leaseID int64, ok bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	client := r.driver()
	if client == nil {
		return 0, false
	}
	if leaseID := r.lease(); leaseID != 0 {
		ttl, err := client.TimeToLive(leaseID)
		switch {
		case err != nil:
			if etcd.Classify(err) == etcd.KindRejected {
				// The server refuses lease reads: a new lease would be refused
				// too, so report loudly and start over with a fresh one.
				log.Errorf("online registration %s: cannot read the ttl of lease %d: %v", r.key, leaseID, err)
				r.abandonLease(client, leaseID)
			} else {
				// Connectivity: keep the lease and retry, so a short partition
				// does not make us race our own still-live record.
				log.WarnErrorf(err, "online registration %s: cannot read the ttl of lease %d", r.key, leaseID)
			}
			return 0, false
		case ttl < 0:
			log.Warnf("online registration %s: lease %d has expired", r.key, leaseID)
			r.clearLease(leaseID)
		default:
			keyLease, exists, err := client.LeaseOfKey(r.key)
			if err != nil {
				log.WarnErrorf(err, "online registration %s: cannot read the record to confirm lease %d", r.key, leaseID)
				r.abandonLease(client, leaseID)
				return 0, false
			}
			switch {
			case !exists:
				log.Warnf("online registration %s: record disappeared; rebinding it to lease %d", r.key, leaseID)
				return r.bindWith(client, leaseID)
			case keyLease == leaseID:
				return leaseID, true
			default:
				log.Warnf("online registration %s: record is held by lease %d, not ours (%d); waiting for its owner to exit or expire", r.key, keyLease, leaseID)
				r.abandonLease(client, leaseID)
				return 0, false
			}
		}
	}

	leaseID, err := client.GrantLease(ttlSeconds(r.ttl))
	if err != nil {
		if etcd.Classify(err) == etcd.KindRejected {
			log.Errorf("online registration %s: lease grant rejected: %v", r.key, err)
		} else {
			log.WarnErrorf(err, "online registration %s: lease grant failed", r.key)
		}
		return 0, false
	}
	r.setLease(leaseID)
	return r.bindWith(client, leaseID)
}

func (r *Registry) bindWith(client LeaseClient, leaseID int64) (int64, bool) {
	succeeded, err := client.PutWithLeaseIfAbsent(r.key, r.value, leaseID)
	if err == nil {
		if succeeded {
			log.Infof("online registration %s: registered addr %q with lease %d", r.key, r.addr, leaseID)
			return leaseID, true
		}
		// The compare failed, so the key already exists: only an existing
		// record bound to this same lease means the write is ours.
		return r.verifyOwnership(client, leaseID, false)
	}
	if etcd.Classify(err) == etcd.KindRejected {
		// The server refused the transaction before applying it, so there is
		// nothing to read back.
		log.Errorf("online registration %s: lease %d bind rejected: %v", r.key, leaseID, err)
		return 0, false
	}
	log.WarnErrorf(err, "online registration %s: lease %d bind has an unknown outcome; verifying ownership", r.key, leaseID)
	return r.verifyOwnership(client, leaseID, true)
}

// verifyOwnership reads the key's lease after an ambiguous bind. With
// retryMissing, a missing key means the write probably never reached the
// server, so the same lease is used for one more attempt instead of granting
// another one.
func (r *Registry) verifyOwnership(client LeaseClient, leaseID int64, retryMissing bool) (int64, bool) {
	if client == nil {
		return 0, false
	}
	keyLease, exists, err := client.LeaseOfKey(r.key)
	if err != nil {
		log.WarnErrorf(err, "online registration %s: cannot verify ownership of lease %d", r.key, leaseID)
		r.abandonLease(client, leaseID)
		return 0, false
	}
	switch {
	case exists && keyLease == leaseID:
		log.Infof("online registration %s: registered addr %q with lease %d (confirmed after an unknown outcome)", r.key, r.addr, leaseID)
		return leaseID, true
	case exists:
		log.Warnf("online registration %s: record is held by lease %d, not ours (%d); leaving it alone", r.key, keyLease, leaseID)
		r.abandonLease(client, leaseID)
		return 0, false
	case !retryMissing:
		log.Warnf("online registration %s: record vanished before lease %d could be confirmed", r.key, leaseID)
		r.abandonLease(client, leaseID)
		return 0, false
	}
	succeeded, err := client.PutWithLeaseIfAbsent(r.key, r.value, leaseID)
	if err == nil && succeeded {
		log.Infof("online registration %s: registered addr %q with lease %d on retry", r.key, r.addr, leaseID)
		return leaseID, true
	}
	if err != nil {
		log.WarnErrorf(err, "online registration %s: retry of the lease %d bind failed", r.key, leaseID)
	}
	r.abandonLease(client, leaseID)
	return 0, false
}

// Close stops the registration loop, revokes this instance's lease with a
// bound and closes the client (contract §4). The order matters: the loop must
// be gone before the revoke, so it cannot rebind the key; and the record must
// never be deleted by token, so a late Close of an old instance cannot remove
// the record a new instance now owns. A failed revoke is only logged — the
// server-side TTL converges — and a crashed instance relies on that TTL alone.
// Close is idempotent and safe when the connection was never established: it
// then only stops the loop that was never started and closes nothing.
func (r *Registry) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		cancel, done := r.cancel, r.done
		client, leaseBefore := r.client, r.leaseID
		// Forget the client and the lease now so no later round reuses them.
		// The lease read here is only a candidate: a round already inside
		// GrantLease/bind uses its own client and calls, which the cancel does
		// not abort, so it can set a new lease after this snapshot. The
		// authoritative lease is read back below, after the loop is gone.
		r.client = nil
		r.leaseID = 0
		r.mu.Unlock()

		var leaseAfter int64
		if cancel != nil {
			cancel()
			if r.onCloseWait != nil {
				r.onCloseWait()
			}
			if done != nil {
				select {
				case <-done:
				case <-time.After(r.stopWait):
					log.Warnf("online registration %s: loop did not stop within %s; closing anyway", r.key, r.stopWait)
				}
			}
			// Read back the lease the loop left set: a grant that raced the
			// cancel binds its lease here even though the snapshot above saw
			// leaseID 0, so revoking only the snapshot would leak it to the TTL.
			leaseAfter = r.lease()
		}

		if client == nil {
			// Never connected: there is no lease to revoke and nothing to close.
			return
		}
		// RevokeLease is bounded by the client's request timeout, so a
		// coordinator that stopped answering cannot stall shutdown. Both the
		// pre-close lease and any lease a racing round granted are revoked
		// once; a failed revoke is only logged and the TTL converges.
		revoke := func(leaseID int64) {
			if err := client.RevokeLease(leaseID); err != nil {
				log.Errorf("online registration %s: revoking lease %d on shutdown failed; the server ttl removes the record: %v", r.key, leaseID, err)
			} else {
				log.Infof("online registration %s: revoked lease %d on shutdown", r.key, leaseID)
			}
		}
		if leaseBefore != 0 {
			revoke(leaseBefore)
		}
		if leaseAfter != 0 && leaseAfter != leaseBefore {
			revoke(leaseAfter)
		}
		if err := client.Close(); err != nil {
			log.Errorf("online registration %s: closing the etcd client failed: %v", r.key, err)
			r.closeErr = err
		}
	})
	return r.closeErr
}

// stopped reports whether the registration loop has exited (or was never
// started). Package tests use it to assert the shutdown order.
func (r *Registry) stopped() bool {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func (r *Registry) lease() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.leaseID
}

func (r *Registry) setLease(leaseID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leaseID = leaseID
}

// clearLease forgets leaseID without revoking it.
func (r *Registry) clearLease(leaseID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leaseID == leaseID {
		r.leaseID = 0
	}
}

// abandonLease gives up leaseID: it revokes it while the server still answers,
// so keys bound to it do not linger until the TTL, then forgets it. A failed
// revoke is only logged. The client is passed in because Close may already have
// cleared r.client for this instance while a round is still finishing.
func (r *Registry) abandonLease(client LeaseClient, leaseID int64) {
	r.clearLease(leaseID)
	if client != nil {
		if err := client.RevokeLease(leaseID); err != nil {
			log.WarnErrorf(err, "online registration %s: cannot revoke lease %d", r.key, leaseID)
		}
	}
}

// ttlSeconds is the granted TTL in whole seconds; etcd rejects 0.
func ttlSeconds(ttl time.Duration) int64 {
	if secs := int64(ttl / time.Second); secs > 0 {
		return secs
	}
	return 1
}

// backoff is the exponential retry delay of the registration loop (the
// jodis watcher used the same 1..30 second cadence). A successful
// registration resets it.
type backoff struct {
	minStep, maxStep int
	unit             time.Duration
	value            int
}

func (b *backoff) reset() { b.value = 0 }

// next returns the following delay and advances the backoff.
func (b *backoff) next() time.Duration {
	v := b.value * 2
	if v < b.minStep {
		v = b.minStep
	}
	if v > b.maxStep {
		v = b.maxStep
	}
	b.value = v
	return b.unit * time.Duration(v)
}

// waitCtx waits for d and reports false when ctx ends first, so a shutting
// down registry never stalls on a retry.
func waitCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
