// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"

	rpctypes "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// leaseOpKey names a lease in an OpError: lease operations have no key of
// their own, but the kind and the original error must stay reachable.
func leaseOpKey(leaseID int64) string {
	return fmt.Sprintf("lease(%d)", leaseID)
}

// GrantLease asks the server for a fresh lease of ttlSeconds and returns its
// ID. Every bounded call uses the client's request timeout.
func (c *Client) GrantLease(ttlSeconds int64) (leaseID int64, err error) {
	if err := c.checkOpen(); err != nil {
		return 0, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, wrapOp("lease-grant", fmt.Sprintf("lease(ttl=%d)", ttlSeconds), err)
	}
	return int64(resp.ID), nil
}

// KeepAlive renews leaseID until ctx ends. The returned channel carries the
// server's acknowledgements and is CLOSED when the lease can no longer be
// trusted (server unreachable for about one TTL, permission change, halted
// stream); a closed channel is the only failure signal the SDK reports after
// the call returned, so callers must treat it as "re-confirm ownership".
// KeepAlive itself is NOT bounded by the request timeout: it lives as long as
// ctx and the lease do.
func (c *Client) KeepAlive(ctx context.Context, leaseID int64) (<-chan *clientv3.LeaseKeepAliveResponse, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	ch, err := c.client.KeepAlive(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		return nil, wrapOp("lease-keepalive", leaseOpKey(leaseID), err)
	}
	return ch, nil
}

// RevokeLease revokes leaseID immediately; the server deletes every key bound
// to it. Revoking an already expired lease is an error (the server no longer
// knows it).
func (c *Client) RevokeLease(leaseID int64) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	if _, err := c.client.Revoke(ctx, clientv3.LeaseID(leaseID)); err != nil {
		return wrapOp("lease-revoke", leaseOpKey(leaseID), err)
	}
	return nil
}

// TimeToLive returns the remaining seconds of leaseID. A lease the server no
// longer knows (expired, revoked, never granted) reports -1.
func (c *Client) TimeToLive(leaseID int64) (ttlSeconds int64, err error) {
	if err := c.checkOpen(); err != nil {
		return 0, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.TimeToLive(ctx, clientv3.LeaseID(leaseID))
	if err != nil {
		if stderrors.Is(err, rpctypes.ErrLeaseNotFound) {
			return -1, nil
		}
		return 0, wrapOp("lease-ttl", leaseOpKey(leaseID), err)
	}
	return resp.TTL, nil
}

// PutWithLeaseIfAbsent binds value to leaseID under key with a transaction
// that only writes when the key does not exist (version == 0), so it never
// overwrites another owner's record. succeeded is false when the key already
// exists; a transport failure returns an error whose Kind tells whether the
// write may have been applied (KindUnknown) or was refused (KindRejected).
func (c *Client) PutWithLeaseIfAbsent(key string, value []byte, leaseID int64) (succeeded bool, err error) {
	if err := c.checkOpen(); err != nil {
		return false, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Version(key), "=", 0)).
		Then(clientv3.OpPut(key, string(value), clientv3.WithLease(clientv3.LeaseID(leaseID)))).
		Commit()
	if err != nil {
		return false, wrapOp("put-lease", key, err)
	}
	return resp.Succeeded, nil
}

// LeaseOfKey reports the lease bound to key and whether the key exists. A
// missing key returns exists=false with no error, so a caller that cannot
// confirm ownership can tell "gone" from "unreadable".
func (c *Client) LeaseOfKey(key string) (leaseID int64, exists bool, err error) {
	if err := c.checkOpen(); err != nil {
		return 0, false, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Get(ctx, key)
	if err != nil {
		return 0, false, wrapOp("read", key, err)
	}
	for _, kv := range resp.Kvs {
		if string(kv.Key) == key {
			return kv.Lease, true, nil
		}
	}
	return 0, false, nil
}

// KeyValue is one record of a prefix read.
type KeyValue struct {
	Key   string
	Value []byte
}

// RangeWithPrefix reads every key under prefix in ONE linearizable range
// read, bounded by the client's request timeout on top of ctx. Prefix "dir"
// matches "dir/..." but not "dir" itself, so it cannot pick up a sibling with
// a shared name prefix.
func (c *Client) RangeWithPrefix(ctx context.Context, prefix string) ([]KeyValue, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	dir := strings.TrimSuffix(prefix, "/")
	resp, err := c.client.Get(ctx, dir+"/", clientv3.WithPrefix())
	if err != nil {
		return nil, wrapOp("range", prefix, err)
	}
	kvs := make([]KeyValue, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		kvs = append(kvs, KeyValue{Key: string(kv.Key), Value: kv.Value})
	}
	return kvs, nil
}
