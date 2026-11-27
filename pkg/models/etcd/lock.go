// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
)

var _ models.LockClient = (*Client)(nil)

// CreateWithRevision implements models.LockClient: it creates the key only
// when it does not exist and reports the key's create_revision. A compare that
// returns false reports succeeded=false instead of an error, so the caller can
// tell a lock conflict from a connectivity failure.
func (c *Client) CreateWithRevision(path string, data []byte) (bool, int64, error) {
	if err := c.checkOpen(); err != nil {
		return false, 0, err
	}
	succeeded, header, err := c.txnCreate(path, data)
	if err != nil {
		return false, 0, err
	}
	if !succeeded {
		return false, 0, nil
	}
	if header == nil {
		return false, 0, &OpError{
			Op: "create", Key: path, Err: errors.New("missing response header"), Kind: KindUnknown,
		}
	}
	// The transaction writes exactly one key, so the transaction revision is
	// that key's create_revision.
	return true, header.Revision, nil
}

// ReadWithRevision implements models.LockClient. The read is linearizable and
// bounded by the client's request timeout; a missing key returns zero values.
func (c *Client) ReadWithRevision(path string) ([]byte, int64, int64, int64, error) {
	if err := c.checkOpen(); err != nil {
		return nil, 0, 0, 0, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Get(ctx, path)
	if err != nil {
		return nil, 0, 0, 0, wrapOp("read", path, err)
	}
	for _, kv := range resp.Kvs {
		if string(kv.Key) == path {
			return kv.Value, kv.CreateRevision, kv.ModRevision, kv.Lease, nil
		}
	}
	return nil, 0, 0, 0, nil
}

// DeleteByCreateRevision implements models.LockClient: it deletes the key only
// when its create_revision still equals createRevision. A compare that returns
// false reports succeeded=false so the caller can distinguish a missing lock
// from a lock that changed owner.
func (c *Client) DeleteByCreateRevision(path string, createRevision int64) (bool, error) {
	if err := c.checkOpen(); err != nil {
		return false, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(path), "=", createRevision)).
		Then(clientv3.OpDelete(path)).
		Commit()
	if err != nil {
		return false, wrapOp("delete", path, err)
	}
	return resp.Succeeded, nil
}
