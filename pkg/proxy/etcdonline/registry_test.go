// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcdonline

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/etcd"
)

// fakeStore holds the etcd state shared by every fake client of a test, so
// two registries see the same keys and leases, as two proxies of one product
// do against one cluster.
type fakeStore struct {
	mu        sync.Mutex
	kvs       map[string]fakeEntry
	leases    map[int64]int64 // lease ID -> granted ttl in seconds
	nextLease int64
}

type fakeEntry struct {
	value []byte
	lease int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{kvs: map[string]fakeEntry{}, leases: map[int64]int64{}, nextLease: 1000}
}

func (s *fakeStore) put(key string, value []byte, lease int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kvs[key] = fakeEntry{value: append([]byte(nil), value...), lease: lease}
}

func (s *fakeStore) get(key string) (fakeEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.kvs[key]
	return e, ok
}

// expireLease removes a lease and its keys, as the server does when the TTL
// runs out.
func (s *fakeStore) expireLease(leaseID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases, leaseID)
	for key, e := range s.kvs {
		if e.lease == leaseID {
			delete(s.kvs, key)
		}
	}
}

// fakeClient is one process's view of the store. Failures are injected per
// client so one instance in a test can misbehave while another does not.
type fakeClient struct {
	store *fakeStore

	mu     sync.Mutex
	calls  []string
	closed bool
	stream []chan *clientv3.LeaseKeepAliveResponse

	grantCalls     int
	keepAliveCalls int
	bindCalls      int
	grantFail      int
	bindApplyFail  int // applies the write, then reports an unknown outcome
	bindNoApply    int // reports an unknown outcome without writing
	keepAliveFail  int
	ttlFail        int
	leaseOfFail    int
	revokeFail     int
	onRevoke       func()

	// granted records the lease IDs this fake handed out, in order, so a test
	// can assert which lease Close revoked.
	granted []int64
	// grantStarted is signaled (non-blocking, buffered) when GrantLease
	// begins; grantGate, when set, blocks GrantLease until it is closed. Tests
	// use them to hold a grant in flight across Close.
	grantStarted chan struct{}
	grantGate    chan struct{}
}

func newFakeClient(store *fakeStore) *fakeClient {
	return &fakeClient{store: store}
}

func fakeUnknown(op, key string) error {
	return &etcd.OpError{Op: op, Key: key, Err: fmt.Errorf("error reading from server: EOF"), Kind: etcd.KindUnknown}
}

func fakeRejected(op, key string) error {
	return &etcd.OpError{Op: op, Key: key, Err: fmt.Errorf("etcdserver: permission denied"), Kind: etcd.KindRejected}
}

func (f *fakeClient) log(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeClient) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeClient) lastStream() chan *clientv3.LeaseKeepAliveResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.stream) == 0 {
		return nil
	}
	return f.stream[len(f.stream)-1]
}

func (f *fakeClient) counts() (grant, keepAlive, bind int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.grantCalls, f.keepAliveCalls, f.bindCalls
}

func (f *fakeClient) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeClient) GrantLease(ttlSeconds int64) (int64, error) {
	f.log("grant")
	f.mu.Lock()
	f.grantCalls++
	fail := f.grantFail > 0
	if fail {
		f.grantFail--
	}
	started, gate := f.grantStarted, f.grantGate
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if gate != nil {
		// Block here until the test releases the grant, pinning the window
		// where Close zeroes leaseID before this grant completes.
		<-gate
	}
	if fail {
		return 0, fakeUnknown("lease-grant", "")
	}
	s := f.store
	s.mu.Lock()
	s.nextLease++
	id := s.nextLease
	s.leases[id] = ttlSeconds
	s.mu.Unlock()

	f.mu.Lock()
	f.granted = append(f.granted, id)
	f.mu.Unlock()
	return id, nil
}

func (f *fakeClient) KeepAlive(ctx context.Context, leaseID int64) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	f.log(fmt.Sprintf("keepalive:%d", leaseID))
	f.mu.Lock()
	f.keepAliveCalls++
	if f.keepAliveFail > 0 {
		f.keepAliveFail--
		f.mu.Unlock()
		return nil, fakeUnknown("lease-keepalive", "")
	}
	ch := make(chan *clientv3.LeaseKeepAliveResponse, 1)
	f.stream = append(f.stream, ch)
	f.mu.Unlock()
	// The stream is only closed by the test: a real SDK stream closes when the
	// server stops answering, and this fake must not close it on ctx end, or
	// the registry could not be observed reacting to a closed stream.
	return ch, nil
}

func (f *fakeClient) RevokeLease(leaseID int64) error {
	f.log(fmt.Sprintf("revoke:%d", leaseID))
	f.mu.Lock()
	fail := f.revokeFail > 0
	if fail {
		f.revokeFail--
	}
	hook := f.onRevoke
	f.mu.Unlock()

	s := f.store
	s.mu.Lock()
	_, ok := s.leases[leaseID]
	if ok {
		delete(s.leases, leaseID)
		for key, e := range s.kvs {
			if e.lease == leaseID {
				delete(s.kvs, key)
			}
		}
	}
	s.mu.Unlock()

	if hook != nil {
		hook()
	}
	if fail || !ok {
		// The server no longer knows this lease: a late or duplicate revoke
		// reports an error and touches nothing.
		return &etcd.OpError{Op: "lease-revoke", Key: fmt.Sprintf("lease(%d)", leaseID), Err: fmt.Errorf("etcdserver: requested lease not found"), Kind: etcd.KindNotFound}
	}
	return nil
}

func (f *fakeClient) TimeToLive(leaseID int64) (int64, error) {
	f.log(fmt.Sprintf("ttl:%d", leaseID))
	f.mu.Lock()
	fail := f.ttlFail > 0
	if fail {
		f.ttlFail--
	}
	f.mu.Unlock()
	if fail {
		return 0, fakeUnknown("lease-ttl", "")
	}
	s := f.store
	s.mu.Lock()
	defer s.mu.Unlock()
	ttl, ok := s.leases[leaseID]
	if !ok {
		return -1, nil
	}
	return ttl, nil
}

func (f *fakeClient) PutWithLeaseIfAbsent(key string, value []byte, leaseID int64) (bool, error) {
	f.log("bind:" + key)
	f.mu.Lock()
	f.bindCalls++
	applyFail := f.bindApplyFail > 0
	if applyFail {
		f.bindApplyFail--
	}
	noApply := !applyFail && f.bindNoApply > 0
	if noApply {
		f.bindNoApply--
	}
	f.mu.Unlock()

	switch {
	case noApply:
		return false, fakeUnknown("put-lease", key)
	case applyFail:
		f.applyBind(key, value, leaseID)
		return false, fakeUnknown("put-lease", key)
	}
	succeeded := f.applyBind(key, value, leaseID)
	return succeeded, nil
}

// applyBind mimics the version==0 transaction: it writes only when the key is
// absent and reports whether it did.
func (f *fakeClient) applyBind(key string, value []byte, leaseID int64) bool {
	s := f.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.kvs[key]; ok {
		return false
	}
	s.kvs[key] = fakeEntry{value: append([]byte(nil), value...), lease: leaseID}
	return true
}

func (f *fakeClient) LeaseOfKey(key string) (int64, bool, error) {
	f.log("read:" + key)
	f.mu.Lock()
	fail := f.leaseOfFail > 0
	if fail {
		f.leaseOfFail--
	}
	f.mu.Unlock()
	if fail {
		return 0, false, fakeUnknown("read", key)
	}
	e, ok := f.store.get(key)
	if !ok {
		return 0, false, nil
	}
	return e.lease, true, nil
}

func (f *fakeClient) RangeWithPrefix(ctx context.Context, prefix string) ([]etcd.KeyValue, error) {
	f.log("range:" + prefix)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s := f.store
	s.mu.Lock()
	kvs := make([]etcd.KeyValue, 0, len(s.kvs))
	for key, e := range s.kvs {
		if strings.HasPrefix(key, prefix+"/") {
			kvs = append(kvs, etcd.KeyValue{Key: key, Value: append([]byte(nil), e.value...)})
		}
	}
	s.mu.Unlock()
	slices.SortFunc(kvs, func(a, b etcd.KeyValue) int { return strings.Compare(a.Key, b.Key) })
	return kvs, nil
}

func (f *fakeClient) Close() error {
	f.log("close")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

const testProduct = "test-product"

func newTestRegistry(f *fakeClient, token, addr string) *Registry {
	r := New(Options{Client: f, Product: testProduct, Token: token, Addr: addr})
	// Shorten every wait so the tests stay fast; production keeps 30s/1..30s.
	r.ttl = 2 * time.Second
	r.stopWait = 500 * time.Millisecond
	r.retry = backoff{minStep: 1, maxStep: 2, unit: 5 * time.Millisecond}
	return r
}

func startRegister(t *testing.T, r *Registry) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go r.Register(ctx)
	return cancel
}

func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

func recordOf(t *testing.T, store *fakeStore, token string) (models.OnlineRecord, int64, bool) {
	t.Helper()
	e, ok := store.get(models.OnlineProxyPath(testProduct, token))
	if !ok {
		return models.OnlineRecord{}, 0, false
	}
	var rec models.OnlineRecord
	if err := json.Unmarshal(e.value, &rec); err != nil {
		t.Fatalf("stored record of %s: %v", token, err)
	}
	return rec, e.lease, true
}

func TestRegisterTwoTokensAndList(t *testing.T) {
	store := newFakeStore()
	fa := newFakeClient(store)
	ra := newTestRegistry(fa, "token-a", "127.0.0.1:11080")
	faCancel := startRegister(t, ra)
	defer faCancel()

	fb := newFakeClient(store)
	rb := newTestRegistry(fb, "token-b", "127.0.0.1:11081")
	fbCancel := startRegister(t, rb)
	defer fbCancel()

	eventually(t, time.Second, "both records published", func() bool {
		_, la, oka := recordOf(t, store, "token-a")
		_, lb, okb := recordOf(t, store, "token-b")
		return oka && okb && la != 0 && lb != 0 && la != lb
	})

	records, err := ra.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []models.OnlineRecord{
		{Token: "token-a", Addr: "127.0.0.1:11080"},
		{Token: "token-b", Addr: "127.0.0.1:11081"},
	}
	if !slices.Equal(records, want) {
		t.Fatalf("list = %+v, want %+v", records, want)
	}

	for _, r := range []*Registry{ra, rb} {
		if err := r.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
}

func TestSameTokenConflictDoesNotOverwriteThenTakeover(t *testing.T) {
	store := newFakeStore()
	fa := newFakeClient(store)
	ra := newTestRegistry(fa, "dup", "127.0.0.1:11080")
	faCancel := startRegister(t, ra)
	defer faCancel()

	eventually(t, time.Second, "first instance published", func() bool {
		rec, lease, ok := recordOf(t, store, "dup")
		return ok && lease != 0 && rec.Addr == "127.0.0.1:11080"
	})
	_, la, _ := recordOf(t, store, "dup")

	fb := newFakeClient(store)
	rb := newTestRegistry(fb, "dup", "127.0.0.1:11081")
	fbCancel := startRegister(t, rb)
	defer fbCancel()

	// The second instance must not claim or overwrite the record; it keeps
	// retrying in the background instead.
	eventually(t, time.Second, "second instance attempted a bind", func() bool {
		grant, _, bind := fb.counts()
		return grant >= 1 && bind >= 1
	})
	rec, lease, ok := recordOf(t, store, "dup")
	if !ok || lease != la || rec.Addr != "127.0.0.1:11080" {
		t.Fatalf("record after conflict = %+v lease %d exists %v, want the first instance left alone", rec, lease, ok)
	}
	if _, keepAlive, _ := fb.counts(); keepAlive != 0 {
		t.Fatalf("conflicting instance started keepalive %d times", keepAlive)
	}

	// A normal exit of the owner removes the record; then the second instance
	// wins the race and publishes its own address.
	if err := ra.Close(); err != nil {
		t.Fatalf("close first instance: %v", err)
	}
	eventually(t, 2*time.Second, "second instance takeover", func() bool {
		rec, lease, ok := recordOf(t, store, "dup")
		return ok && lease != 0 && lease != la && rec.Addr == "127.0.0.1:11081"
	})
	// keepAlive follows the successful bind in the same loop iteration; wait
	// for it instead of sampling the counter the instant the record appears.
	eventually(t, time.Second, "second instance started keepalive after takeover", func() bool {
		_, keepAlive, _ := fb.counts()
		return keepAlive >= 1
	})

	if err := rb.Close(); err != nil {
		t.Fatalf("close second instance: %v", err)
	}
}

func TestBindUnknownButAppliedConfirmsWithoutSecondGrant(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	f.bindApplyFail = 1
	r := newTestRegistry(f, "t", "addr")
	cancel := startRegister(t, r)
	defer cancel()

	// The applied write becomes visible in the store before the confirming
	// read is logged, so wait for both instead of reading the call log too
	// early.
	read := "read:" + models.OnlineProxyPath(testProduct, "t")
	eventually(t, time.Second, "record confirmed by reading its lease", func() bool {
		_, lease, ok := recordOf(t, store, "t")
		return ok && lease != 0 && slices.Contains(f.callLog(), read)
	})
	if grant, _, bind := f.counts(); grant != 1 || bind != 1 {
		t.Fatalf("grant=%d bind=%d, want exactly one grant and one bind", grant, bind)
	}
	if calls := f.callLog(); !slices.Contains(calls, read) {
		t.Fatalf("ownership was not verified by reading the lease: %v", calls)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBindUnknownNotAppliedRetriesOnce(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	f.bindNoApply = 1
	r := newTestRegistry(f, "t", "addr")
	cancel := startRegister(t, r)
	defer cancel()

	eventually(t, time.Second, "record published on retry", func() bool {
		_, lease, ok := recordOf(t, store, "t")
		return ok && lease != 0
	})
	if grant, _, bind := f.counts(); grant != 1 || bind != 2 {
		t.Fatalf("grant=%d bind=%d, want one grant and exactly one retried bind", grant, bind)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestForeignLeaseConflictAbandonsOwnLease(t *testing.T) {
	store := newFakeStore()
	store.put(models.OnlineProxyPath(testProduct, "t"), []byte(`{"token":"t","addr":"foreign"}`), 999)
	f := newFakeClient(store)
	r := newTestRegistry(f, "t", "mine")
	cancel := startRegister(t, r)
	defer cancel()

	eventually(t, time.Second, "own lease revoked after the conflict", func() bool {
		grant, _, _ := f.counts()
		return grant >= 1 && len(f.revokedBy()) >= 1
	})
	if _, keepAlive, _ := f.counts(); keepAlive != 0 {
		t.Fatalf("conflicting instance started keepalive %d times", keepAlive)
	}
	rec, lease, ok := recordOf(t, store, "t")
	if !ok || lease != 999 || rec.Addr != "foreign" {
		t.Fatalf("record = %+v lease %d exists %v, want the foreign record untouched", rec, lease, ok)
	}
	for _, id := range f.revokedBy() {
		if id == 999 {
			t.Fatal("conflicting instance revoked the foreign lease")
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestKeepAliveDirectErrorIsRetried(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	f.keepAliveFail = 1
	r := newTestRegistry(f, "t", "addr")
	cancel := startRegister(t, r)
	defer cancel()

	eventually(t, time.Second, "keepalive restarted after a direct error", func() bool {
		_, keepAlive, _ := f.counts()
		return keepAlive >= 2
	})
	if _, _, bind := f.counts(); bind != 1 {
		t.Fatalf("bind=%d, want ownership reused instead of granted again", bind)
	}
	if stream := f.lastStream(); stream == nil {
		t.Fatal("no keepalive stream after the retry")
	}
	if _, lease, ok := recordOf(t, store, "t"); !ok || lease == 0 {
		t.Fatal("record disappeared while retrying the keepalive")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestKeepAliveStreamCloseReconfirmsOwnership(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	r := newTestRegistry(f, "t", "addr")
	cancel := startRegister(t, r)
	defer cancel()

	eventually(t, time.Second, "first keepalive stream", func() bool {
		_, keepAlive, _ := f.counts()
		return keepAlive >= 1 && f.lastStream() != nil
	})
	close(f.lastStream())

	eventually(t, time.Second, "keepalive restarted after the stream closed", func() bool {
		_, keepAlive, _ := f.counts()
		return keepAlive >= 2 && f.lastStream() != nil
	})
	calls := f.callLog()
	read := "read:" + models.OnlineProxyPath(testProduct, "t")
	if !slices.Contains(calls, read) {
		t.Fatalf("a closed stream did not trigger an ownership check: %v", calls)
	}
	if grant, _, _ := f.counts(); grant != 1 {
		t.Fatalf("grant=%d, want the same lease to be reused after a closed stream", grant)
	}
	if _, lease, ok := recordOf(t, store, "t"); !ok || lease == 0 {
		t.Fatal("record disappeared after the stream closed")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestCloseIsIdempotentAndOrdered(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	r := newTestRegistry(f, "t", "addr")
	r.stopWait = 50 * time.Millisecond
	cancel := startRegister(t, r)
	defer cancel()

	eventually(t, time.Second, "record published", func() bool {
		_, lease, ok := recordOf(t, store, "t")
		return ok && lease != 0
	})
	f.mu.Lock()
	f.onRevoke = func() {
		if !r.stopped() {
			t.Errorf("lease revoked before the registration loop stopped")
		}
	}
	f.mu.Unlock()

	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	calls := f.callLog()
	revokeAt := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, "revoke:") })
	closeAt := slices.IndexFunc(calls, func(c string) bool { return c == "close" })
	if revokeAt < 0 || closeAt < 0 || revokeAt > closeAt {
		t.Fatalf("calls = %v, want revoke before close", calls)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if n := len(f.revokedBy()); n != 1 {
		t.Fatalf("revoked %d leases, want exactly 1", n)
	}
	if n := slices.IndexFunc(calls, func(c string) bool { return c == "close" }); n != closeAt {
		t.Fatal("second close reached the client again")
	}
	if _, _, ok := recordOf(t, store, "t"); ok {
		t.Fatal("record survived close")
	}
}

func TestLateOldInstanceCloseKeepsNewRecord(t *testing.T) {
	store := newFakeStore()
	fo := newFakeClient(store)
	old := newTestRegistry(fo, "t", "old-addr")
	cancelOld := startRegister(t, old)
	defer cancelOld()

	eventually(t, time.Second, "old instance published", func() bool {
		rec, lease, ok := recordOf(t, store, "t")
		return ok && lease != 0 && rec.Addr == "old-addr"
	})
	_, leaseOld, _ := recordOf(t, store, "t")

	// The server drops the old lease and its record (TTL ran out during a
	// partition) while the old process still believes it owns it.
	store.expireLease(leaseOld)

	fn := newFakeClient(store)
	fresh := newTestRegistry(fn, "t", "new-addr")
	cancelFresh := startRegister(t, fresh)
	defer cancelFresh()
	eventually(t, time.Second, "new instance published", func() bool {
		rec, lease, ok := recordOf(t, store, "t")
		return ok && lease != 0 && lease != leaseOld && rec.Addr == "new-addr"
	})
	_, leaseNew, _ := recordOf(t, store, "t")

	// The old instance closes late: it may only revoke its own, already
	// unknown lease, never the record of the new instance.
	fo.mu.Lock()
	fo.revokeFail = 1
	fo.mu.Unlock()
	if err := old.Close(); err != nil {
		t.Fatalf("late close: %v", err)
	}
	rec, lease, ok := recordOf(t, store, "t")
	if !ok || lease != leaseNew || rec.Addr != "new-addr" {
		t.Fatalf("record after the late close = %+v lease %d exists %v, want the new instance's record", rec, lease, ok)
	}
	for _, id := range fo.revokedBy() {
		if id == leaseNew {
			t.Fatal("late old instance revoked the new instance's lease")
		}
	}
	if _, keepAlive, _ := fn.counts(); keepAlive == 0 {
		t.Fatal("new instance lost its keepalive")
	}
	if err := fresh.Close(); err != nil {
		t.Fatalf("close new instance: %v", err)
	}
	if _, _, ok := recordOf(t, store, "t"); ok {
		t.Fatal("new instance's record survived its close")
	}
}

func TestListSkipsBadRecords(t *testing.T) {
	store := newFakeStore()
	dir := models.OnlineProxyDir(testProduct)
	store.put(dir+"/good", []byte(`{"token":"good","addr":"127.0.0.1:11080"}`), 1)
	store.put(dir+"/malformed", []byte(`{"token":`), 1)
	store.put(dir+"/mismatch", []byte(`{"token":"other","addr":"127.0.0.1:1"}`), 1)
	store.put(dir+"/no-addr", []byte(`{"token":"no-addr"}`), 1)
	store.put(dir+"/deep/extra", []byte(`{"token":"extra","addr":"127.0.0.1:2"}`), 1)
	other := models.OnlineProxyDir("other-product")
	store.put(other+"/foreign", []byte(`{"token":"foreign","addr":"127.0.0.1:3"}`), 1)

	r := New(Options{Client: newFakeClient(store), Product: testProduct, Token: "good", Addr: "127.0.0.1:11080"})
	records, err := r.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 1 || records[0].Token != "good" || records[0].Addr != "127.0.0.1:11080" {
		t.Fatalf("list = %+v, want only the good record", records)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.List(canceled); err == nil {
		t.Fatal("list with a canceled context returned no error")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// grantedIDs lists the leases this fake client handed out, in order.
func (f *fakeClient) grantedIDs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.granted...)
}

// revokedBy lists the leases this fake client revoked, in order.
func (f *fakeClient) revokedBy() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []int64
	for _, c := range f.calls {
		if id, ok := cutPrefixInt(c, "revoke:"); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func cutPrefixInt(call, prefix string) (int64, bool) {
	rest, ok := strings.CutPrefix(call, prefix)
	if !ok {
		return 0, false
	}
	var id int64
	if _, err := fmt.Sscanf(rest, "%d", &id); err != nil {
		return 0, false
	}
	return id, true
}

// newLazyRegistry returns a registry that only gets a driver through the
// factory, with the short waits of newTestRegistry.
func newLazyRegistry(token, addr string, factory func() (LeaseClient, error)) *Registry {
	r := New(Options{NewClient: factory, Product: testProduct, Token: token, Addr: addr})
	r.ttl = 2 * time.Second
	r.stopWait = 500 * time.Millisecond
	r.retry = backoff{minStep: 1, maxStep: 2, unit: 5 * time.Millisecond}
	return r
}

// TestRegisterWaitsForLazyClient pins the lazy connection: while NewClient
// fails nothing is granted, written or joined (no keepalive), and the
// successful third attempt registers normally.
func TestRegisterWaitsForLazyClient(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	var attempts atomic.Int32
	r := newLazyRegistry("t", "addr", func() (LeaseClient, error) {
		if n := attempts.Add(1); n < 3 {
			return nil, fmt.Errorf("connect attempt %d: dial unavailable", n)
		}
		return f, nil
	})
	cancel := startRegister(t, r)
	defer cancel()

	eventually(t, time.Second, "two failed connection attempts", func() bool {
		return attempts.Load() >= 2
	})
	if grant, keepAlive, bind := f.counts(); grant != 0 || keepAlive != 0 || bind != 0 {
		t.Fatalf("grant=%d keepalive=%d bind=%d before the client exists, want none", grant, keepAlive, bind)
	}
	if _, _, ok := recordOf(t, store, "t"); ok {
		t.Fatal("a record was published before the client exists")
	}
	if _, err := r.List(context.Background()); !stderrors.Is(err, ErrNoClient) {
		t.Fatalf("list without a client = %v, want ErrNoClient", err)
	}

	eventually(t, 2*time.Second, "record published after the third attempt", func() bool {
		_, lease, ok := recordOf(t, store, "t")
		return ok && lease != 0
	})
	if n := attempts.Load(); n < 3 {
		t.Fatalf("client factory called %d times, want at least 3", n)
	}
	// The keepalive starts after the bind returns, so wait for it instead of
	// reading the counters in the same instant.
	eventually(t, time.Second, "keepalive of the new lease", func() bool {
		_, keepAlive, _ := f.counts()
		return keepAlive == 1
	})
	if grant, _, bind := f.counts(); grant != 1 || bind != 1 {
		t.Fatalf("grant=%d bind=%d, want exactly one of each", grant, bind)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !f.isClosed() {
		t.Fatal("the lazily created client was not closed")
	}
}

// TestCloseBeforeAnyConnectionIsSafe pins that Close neither panics nor waits
// when no driver was ever created, both before Register ran and while the loop
// is still retrying the connection.
func TestCloseBeforeAnyConnectionIsSafe(t *testing.T) {
	neverStarted := New(Options{Product: testProduct, Token: "t", Addr: "addr"})
	if _, err := neverStarted.List(context.Background()); !stderrors.Is(err, ErrNoClient) {
		t.Fatalf("list without a client = %v, want ErrNoClient", err)
	}
	if err := neverStarted.Close(); err != nil {
		t.Fatalf("close before Register: %v", err)
	}

	var attempts atomic.Int32
	retrying := newLazyRegistry("u", "addr", func() (LeaseClient, error) {
		attempts.Add(1)
		return nil, fmt.Errorf("coordinator unreachable")
	})
	cancel := startRegister(t, retrying)
	defer cancel()
	eventually(t, time.Second, "a failed connection attempt", func() bool {
		return attempts.Load() >= 1
	})
	if err := retrying.Close(); err != nil {
		t.Fatalf("close while retrying: %v", err)
	}
	if !retrying.stopped() {
		t.Fatal("the registration loop kept running after Close")
	}
	if err := retrying.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// TestCloseRevokesLeaseGrantedWhileClosing pins the W-1 window: a round that
// is already inside GrantLease when Close runs grants and binds a lease after
// Close has zeroed leaseID. Close must therefore read back the lease the loop
// finally set, not only the pre-close snapshot, and revoke the newly granted
// one so the record does not linger until the TTL (contract §4).
func TestCloseRevokesLeaseGrantedWhileClosing(t *testing.T) {
	store := newFakeStore()
	f := newFakeClient(store)
	gate := make(chan struct{})
	f.grantStarted = make(chan struct{}, 1)
	f.grantGate = gate

	r := newTestRegistry(f, "t", "addr")
	// Wide enough that the blocked grant is always released before the bounded
	// wait expires; the grant is released by the close hook, not by a timeout.
	r.stopWait = 5 * time.Second
	r.onCloseWait = func() { close(gate) }

	cancel := startRegister(t, r)
	defer cancel()

	// Hold the loop inside GrantLease before Close runs.
	select {
	case <-f.grantStarted:
	case <-time.After(time.Second):
		t.Fatal("registration never entered GrantLease")
	}
	if _, _, ok := recordOf(t, store, "t"); ok {
		t.Fatal("record was published before the blocked grant returned")
	}

	// Close zeroes leaseID and cancels, then releases the gate; the grant lands
	// after the snapshot, exactly as in the reported race.
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	granted := f.grantedIDs()
	if len(granted) != 1 {
		t.Fatalf("granted %v, want exactly one lease", granted)
	}
	revokes := f.revokedBy()
	if len(revokes) != 1 || revokes[0] != granted[0] {
		t.Fatalf("revoked %v, want the lease granted while closing (%v)", revokes, granted)
	}
	// The bind ran (the create-if-absent write landed), then the revoke removed
	// the record, so nothing survives to the TTL.
	if grant, _, bind := f.counts(); grant != 1 || bind != 1 {
		t.Fatalf("grant=%d bind=%d, want the racing round to have bound its lease once", grant, bind)
	}
	if _, _, ok := recordOf(t, store, "t"); ok {
		t.Fatal("record granted while closing survived")
	}
}
