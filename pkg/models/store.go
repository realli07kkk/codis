// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/google/uuid"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

func init() {
	if filepath.Separator != '/' {
		log.Panicf("bad Separator = '%c', must be '/'", filepath.Separator)
	}
}

const CodisDir = "/codis3"

func ProductDir(product string) string {
	return filepath.Join(CodisDir, product)
}

func LockPath(product string) string {
	return filepath.Join(CodisDir, product, "topom")
}

func SlotPath(product string, sid int) string {
	return filepath.Join(CodisDir, product, "slots", fmt.Sprintf("slot-%04d", sid))
}

func GroupDir(product string) string {
	return filepath.Join(CodisDir, product, "group")
}

func ProxyDir(product string) string {
	return filepath.Join(CodisDir, product, "proxy")
}

func GroupPath(product string, gid int) string {
	return filepath.Join(CodisDir, product, "group", fmt.Sprintf("group-%04d", gid))
}

func ProxyPath(product string, token string) string {
	return filepath.Join(CodisDir, product, "proxy", fmt.Sprintf("proxy-%s", token))
}

func SentinelPath(product string) string {
	return filepath.Join(CodisDir, product, "sentinel")
}

func ACLPath(product string) string {
	return filepath.Join(CodisDir, product, "acl")
}

func ProxyQPSLimitPath(product string) string {
	return filepath.Join(CodisDir, product, "proxy_qps_limit")
}

func LoadTopom(client Client, product string, must bool) (*Topom, error) {
	b, err := client.Read(LockPath(product), must)
	if err != nil || b == nil {
		return nil, err
	}
	t := &Topom{}
	if err := jsonDecode(t, b); err != nil {
		return nil, err
	}
	return t, nil
}

type Store struct {
	client  Client
	product string
}

func NewStore(client Client, product string) *Store {
	return &Store{client, product}
}

func (s *Store) Close() error {
	return s.client.Close()
}

func (s *Store) Client() Client {
	return s.client
}

func (s *Store) LockPath() string {
	return LockPath(s.product)
}

func (s *Store) SlotPath(sid int) string {
	return SlotPath(s.product, sid)
}

func (s *Store) GroupDir() string {
	return GroupDir(s.product)
}

func (s *Store) ProxyDir() string {
	return ProxyDir(s.product)
}

func (s *Store) GroupPath(gid int) string {
	return GroupPath(s.product, gid)
}

func (s *Store) ProxyPath(token string) string {
	return ProxyPath(s.product, token)
}

func (s *Store) SentinelPath() string {
	return SentinelPath(s.product)
}

func (s *Store) ACLPath() string {
	return ACLPath(s.product)
}

func (s *Store) ProxyQPSLimitPath() string {
	return ProxyQPSLimitPath(s.product)
}

// LockOwnership is what one process holds on the /topom key: the owner id
// stored in the record and the create_revision of the record it created.
// ReleaseLock conditions its delete on CreateRevision, so a stale owner can
// never remove a successor's lock.
type LockOwnership struct {
	OwnerID        string
	CreateRevision int64
}

// LockInfo is a snapshot of the persistent topom lock, used by operator
// tooling to inspect and explicitly remove it.
type LockInfo struct {
	Topom          *Topom
	CreateRevision int64
	ModRevision    int64
	Lease          int64
}

var (
	// ErrLockHeld means another instance owns the lock. It is a definite
	// rejection and must be distinguishable from a connection failure.
	ErrLockHeld = errors.New("topom lock is held by another instance")
	// ErrLockOwnershipChanged means the lock exists, but not with the
	// create_revision the caller expected.
	ErrLockOwnershipChanged = errors.New("topom lock ownership has changed")
	// ErrLockGone means the lock key does not exist.
	ErrLockGone = errors.New("topom lock does not exist")
	// ErrResultUnknown means the outcome of a lock write cannot be confirmed.
	// The lock must be treated as possibly held.
	ErrResultUnknown = errors.New("topom lock operation result is unknown")
	// ErrLockUnsupported means the coordinator client lacks the
	// revision-conditioned operations the persistent lock requires.
	ErrLockUnsupported = errors.New("coordinator does not support the persistent topom lock")
)

// lockRecord is the decoded content of the lock key plus its revisions.
type lockRecord struct {
	topom          *Topom
	createRevision int64
	modRevision    int64
	lease          int64
}

func (s *Store) lockClient() (LockClient, error) {
	c, ok := s.client.(LockClient)
	if !ok {
		return nil, errors.Trace(ErrLockUnsupported)
	}
	return c, nil
}

// readLock performs one linearizable read of the lock key, bounded by the
// client's request timeout. A missing key returns (nil, nil); callers must
// never read a failure as "the lock is free".
func (s *Store) readLock(c LockClient, path string) (*lockRecord, error) {
	data, createRevision, modRevision, lease, err := c.ReadWithRevision(path)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	t := &Topom{}
	if err := jsonDecode(t, data); err != nil {
		return nil, err
	}
	return &lockRecord{
		topom: t, createRevision: createRevision, modRevision: modRevision, lease: lease,
	}, nil
}

// lockResultUnknown builds the ErrResultUnknown error while keeping the write
// and read-back causes reachable through errors.Is/As. The result is wrapped
// as an unknownOutcome, so a refusal hidden in a cause (a refused read-back)
// never turns the unconfirmed outcome into a definite rejection.
func lockResultUnknown(op, path string, writeErr, readErr error) error {
	switch {
	case writeErr != nil && readErr != nil:
		return &unknownOutcome{errors.Errorf("%w: models: %s %s failed: %w; read-back failed: %w",
			ErrResultUnknown, op, path, writeErr, readErr)}
	case writeErr != nil:
		return &unknownOutcome{errors.Errorf("%w: models: %s %s failed: %w", ErrResultUnknown, op, path, writeErr)}
	case readErr != nil:
		return &unknownOutcome{errors.Errorf("%w: models: %s %s: read-back failed: %w", ErrResultUnknown, op, path, readErr)}
	default:
		return &unknownOutcome{errors.Errorf("%w: models: %s %s", ErrResultUnknown, op, path)}
	}
}

// lockRejected reports a lock write the server definitively refused before
// applying it (contract §2, "明确拒绝"). Its outcome is known, so it is
// neither read back nor reported as ErrResultUnknown.
func lockRejected(op, path string, err error) error {
	return errors.Errorf("models: %s %s rejected: %w", op, path, err)
}

// AcquireLock takes the persistent topom lock (contract §2.1). Ownership is
// published as the Topom value plus the key's create_revision. A write the
// server refuses outright is reported as-is. When the write outcome is
// unknown the lock is read back: only a record carrying this process's owner
// id proves ownership, and a failed read is never taken as "the lock is
// free".
func (s *Store) AcquireLock(t *Topom) (*LockOwnership, error) {
	if t == nil {
		return nil, errors.New("models: nil topom for acquire lock")
	}
	c, err := s.lockClient()
	if err != nil {
		return nil, err
	}
	if t.OwnerID == "" {
		// The dashboard retries Start on the same Topom, so one id per
		// process start lets those retries recover the same lock.
		t.OwnerID = uuid.NewString()
	}
	path, data := s.LockPath(), t.Encode()

	succeeded, createRevision, err := c.CreateWithRevision(path, data)
	if err == nil {
		if succeeded {
			return &LockOwnership{OwnerID: t.OwnerID, CreateRevision: createRevision}, nil
		}
		// The conditional create was rejected, so the key exists. Read it
		// once: an earlier attempt with a lost response may have created it.
		return s.confirmOwner(c, path, t.OwnerID)
	}
	if IsRejected(err) {
		// A definite refusal leaves the key untouched, so a read-back would
		// prove nothing and the outcome is not unknown.
		return nil, lockRejected("acquire lock of", path, err)
	}

	// The submit outcome is unknown. Read the lock back before anything else.
	record, readErr := s.readLock(c, path)
	if readErr != nil {
		return nil, lockResultUnknown("acquire lock of", path, err, readErr)
	}
	if record != nil {
		if record.topom.OwnerID == t.OwnerID {
			return &LockOwnership{OwnerID: t.OwnerID, CreateRevision: record.createRevision}, nil
		}
		return nil, errors.Errorf("%w: models: acquire lock of %s: %v", ErrLockHeld, path, err)
	}
	// The key is missing, so the create cannot have been applied: retry the
	// conditional create exactly once with the same owner id.
	succeeded, createRevision, err = c.CreateWithRevision(path, data)
	if err == nil && succeeded {
		return &LockOwnership{OwnerID: t.OwnerID, CreateRevision: createRevision}, nil
	}
	if err == nil {
		return s.confirmOwner(c, path, t.OwnerID)
	}
	if IsRejected(err) {
		return nil, lockRejected("acquire lock of", path, err)
	}
	// A second retry is not allowed; a later Start re-reads the lock instead.
	return nil, lockResultUnknown("acquire lock of", path, err, nil)
}

// confirmOwner reads the lock after the conditional create was rejected. It
// succeeds only when the record carries ownerID, which is how a process
// recovers a create that actually committed but lost its response.
func (s *Store) confirmOwner(c LockClient, path, ownerID string) (*LockOwnership, error) {
	record, err := s.readLock(c, path)
	switch {
	case err != nil:
		return nil, lockResultUnknown("confirm lock ownership of", path, nil, err)
	case record == nil:
		// The key vanished after the rejected create; treat it as unknown
		// rather than assuming the lock is free.
		return nil, lockResultUnknown("confirm lock ownership of", path, nil, nil)
	case record.topom.OwnerID == ownerID:
		return &LockOwnership{OwnerID: ownerID, CreateRevision: record.createRevision}, nil
	default:
		return nil, errors.Errorf("%w: models: acquire lock of %s is owned by %q",
			ErrLockHeld, path, record.topom.OwnerID)
	}
}

// ReleaseLock releases the lock this process holds (contract §2.2). The delete
// is conditioned on the create_revision captured at acquire time, so a stale
// owner can never remove a successor's lock. A delete the server refuses
// outright is reported as-is, without a read-back.
func (s *Store) ReleaseLock(o *LockOwnership) error {
	if o == nil || o.CreateRevision <= 0 {
		return errors.New("models: invalid lock ownership for release")
	}
	c, err := s.lockClient()
	if err != nil {
		return err
	}
	path := s.LockPath()

	succeeded, err := c.DeleteByCreateRevision(path, o.CreateRevision)
	if err == nil {
		if succeeded {
			return nil
		}
		return s.conditionFailed(c, "release lock of", path, o.CreateRevision)
	}
	if IsRejected(err) {
		return lockRejected("release lock of", path, err)
	}
	// The delete outcome is unknown. Read back: a missing key means the lock
	// is gone (this request cannot claim it deleted it), while a surviving key
	// leaves the outcome unconfirmed. Never replay the delete.
	record, readErr := s.readLock(c, path)
	switch {
	case readErr != nil:
		return lockResultUnknown("release lock of", path, err, readErr)
	case record == nil:
		return errors.Errorf("%w: models: release lock of %s: %v", ErrLockGone, path, err)
	default:
		return lockResultUnknown("release lock of", path, err, nil)
	}
}

// conditionFailed classifies a rejected revision-conditioned delete by reading
// the lock: a missing key is ErrLockGone, anything else changed ownership.
func (s *Store) conditionFailed(c LockClient, op, path string, expected int64) error {
	record, err := s.readLock(c, path)
	switch {
	case err != nil:
		return lockResultUnknown(op, path, nil, err)
	case record == nil:
		return errors.Errorf("%w: models: %s %s", ErrLockGone, op, path)
	default:
		return errors.Errorf("%w: models: %s %s: expected create_revision %d, found %d",
			ErrLockOwnershipChanged, op, path, expected, record.createRevision)
	}
}

// ShowLock returns the current lock, or (nil, nil) when there is none.
func (s *Store) ShowLock() (*LockInfo, error) {
	c, err := s.lockClient()
	if err != nil {
		return nil, err
	}
	record, err := s.readLock(c, s.LockPath())
	if err != nil {
		return nil, errors.Trace(err)
	}
	if record == nil {
		return nil, nil
	}
	return &LockInfo{
		Topom:          record.topom,
		CreateRevision: record.createRevision,
		ModRevision:    record.modRevision,
		Lease:          record.lease,
	}, nil
}

// RemoveLock deletes the lock only when its create_revision equals
// expectedRevision (contract §2.3, operator recovery). A delete the server
// refuses outright is reported as-is, without a read-back. A delete whose
// response is lost is read back: a missing key reports ErrLockGone, and a
// replacement lock is always preserved.
func (s *Store) RemoveLock(expectedRevision int64) error {
	if expectedRevision <= 0 {
		return errors.Errorf("models: remove lock needs a positive revision, got %d", expectedRevision)
	}
	c, err := s.lockClient()
	if err != nil {
		return err
	}
	path := s.LockPath()

	succeeded, err := c.DeleteByCreateRevision(path, expectedRevision)
	if err == nil {
		if succeeded {
			return nil
		}
		return s.conditionFailed(c, "remove lock of", path, expectedRevision)
	}
	if IsRejected(err) {
		return lockRejected("remove lock of", path, err)
	}
	record, readErr := s.readLock(c, path)
	switch {
	case readErr != nil:
		return lockResultUnknown("remove lock of", path, err, readErr)
	case record == nil:
		return errors.Errorf("%w: models: remove lock of %s: %v", ErrLockGone, path, err)
	default:
		return lockResultUnknown("remove lock of", path, err, nil)
	}
}

func (s *Store) LoadTopom(must bool) (*Topom, error) {
	return LoadTopom(s.client, s.product, must)
}

func (s *Store) LoadACL(must bool) (*ACL, error) {
	b, err := s.client.Read(s.ACLPath(), must)
	if err != nil || b == nil {
		return nil, err
	}
	acl := &ACL{}
	if err := jsonDecode(acl, b); err != nil {
		return nil, err
	}
	return acl, nil
}

func (s *Store) UpdateACL(acl *ACL) error {
	return s.client.Update(s.ACLPath(), acl.Encode())
}

func (s *Store) LoadProxyQPSLimit(must bool) (*ProxyQPSLimit, error) {
	b, err := s.client.Read(s.ProxyQPSLimitPath(), must)
	if err != nil || b == nil {
		return nil, err
	}
	config := &ProxyQPSLimit{}
	if err := jsonDecode(config, b); err != nil {
		return nil, err
	}
	return config, nil
}

func (s *Store) UpdateProxyQPSLimit(config *ProxyQPSLimit) error {
	return s.client.Update(s.ProxyQPSLimitPath(), config.Encode())
}

func (s *Store) SlotMappings() ([]*SlotMapping, error) {
	slots := make([]*SlotMapping, MaxSlotNum)
	for i := range slots {
		m, err := s.LoadSlotMapping(i, false)
		if err != nil {
			return nil, err
		}
		if m != nil {
			slots[i] = m
		} else {
			slots[i] = &SlotMapping{Id: i}
		}
	}
	return slots, nil
}

func (s *Store) LoadSlotMapping(sid int, must bool) (*SlotMapping, error) {
	b, err := s.client.Read(s.SlotPath(sid), must)
	if err != nil || b == nil {
		return nil, err
	}
	m := &SlotMapping{}
	if err := jsonDecode(m, b); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Store) UpdateSlotMapping(m *SlotMapping) error {
	return s.client.Update(s.SlotPath(m.Id), m.Encode())
}

func (s *Store) ListGroup() (map[int]*Group, error) {
	paths, err := s.client.List(s.GroupDir(), false)
	if err != nil {
		return nil, err
	}
	group := make(map[int]*Group)
	for _, path := range paths {
		b, err := s.client.Read(path, true)
		if err != nil {
			return nil, err
		}
		g := &Group{}
		if err := jsonDecode(g, b); err != nil {
			return nil, err
		}
		group[g.Id] = g
	}
	return group, nil
}

func (s *Store) LoadGroup(gid int, must bool) (*Group, error) {
	b, err := s.client.Read(s.GroupPath(gid), must)
	if err != nil || b == nil {
		return nil, err
	}
	g := &Group{}
	if err := jsonDecode(g, b); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Store) UpdateGroup(g *Group) error {
	return s.client.Update(s.GroupPath(g.Id), g.Encode())
}

func (s *Store) DeleteGroup(gid int) error {
	return s.client.Delete(s.GroupPath(gid))
}

func (s *Store) ListProxy() (map[string]*Proxy, error) {
	paths, err := s.client.List(s.ProxyDir(), false)
	if err != nil {
		return nil, err
	}
	proxy := make(map[string]*Proxy)
	for _, path := range paths {
		b, err := s.client.Read(path, true)
		if err != nil {
			return nil, err
		}
		p := &Proxy{}
		if err := jsonDecode(p, b); err != nil {
			return nil, err
		}
		proxy[p.Token] = p
	}
	return proxy, nil
}

func (s *Store) LoadProxy(token string, must bool) (*Proxy, error) {
	b, err := s.client.Read(s.ProxyPath(token), must)
	if err != nil || b == nil {
		return nil, err
	}
	p := &Proxy{}
	if err := jsonDecode(p, b); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) UpdateProxy(p *Proxy) error {
	return s.client.Update(s.ProxyPath(p.Token), p.Encode())
}

func (s *Store) DeleteProxy(token string) error {
	return s.client.Delete(s.ProxyPath(token))
}

func (s *Store) LoadSentinel(must bool) (*Sentinel, error) {
	b, err := s.client.Read(s.SentinelPath(), must)
	if err != nil || b == nil {
		return nil, err
	}
	p := &Sentinel{}
	if err := jsonDecode(p, b); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) UpdateSentinel(p *Sentinel) error {
	return s.client.Update(s.SentinelPath(), p.Encode())
}

func ValidateProduct(name string) error {
	if regexp.MustCompile(`^\w[\w\.\-]*$`).MatchString(name) {
		return nil
	}
	return errors.Errorf("bad product name = %s", name)
}
