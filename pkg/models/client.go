// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"time"

	"github.com/CodisLabs/codis/pkg/models/consul"
	"github.com/CodisLabs/codis/pkg/models/fs"
	"github.com/CodisLabs/codis/pkg/models/zk"
	"github.com/CodisLabs/codis/pkg/utils/errors"
)

type Client interface {
	Create(path string, data []byte) error
	Update(path string, data []byte) error
	Delete(path string) error

	Read(path string, must bool) ([]byte, error)
	List(path string, must bool) ([]string, error)

	Close() error

	WatchInOrder(path string) (<-chan struct{}, []string, error)

	CreateEphemeral(path string, data []byte) (<-chan struct{}, error)
	CreateEphemeralInOrder(path string, data []byte) (<-chan struct{}, string, error)
}

// LockClient is implemented by coordinators that support the revision-conditioned
// operations required by the persistent topom lock (contract §2).
type LockClient interface {
	Client
	// CreateWithRevision: Txn(Version(key)==0) → Put. succeeded=false when the key exists.
	// createRevision is the key's create_revision on success.
	CreateWithRevision(path string, data []byte) (succeeded bool, createRevision int64, err error)
	// ReadWithRevision: missing key → (nil, 0, 0, 0, nil).
	ReadWithRevision(path string) (data []byte, createRevision, modRevision, lease int64, err error)
	// DeleteByCreateRevision: Txn(CreateRevision(key)==createRevision) → Delete.
	// succeeded=false when the key is missing or has another create_revision.
	DeleteByCreateRevision(path string, createRevision int64) (succeeded bool, err error)
}

func NewClient(coordinator string, addrlist string, auth string, timeout time.Duration) (Client, error) {
	switch coordinator {
	case "zk", "zookeeper":
		return zkclient.New(addrlist, auth, timeout)
	case "fs", "filesystem":
		return fsclient.New(addrlist)
	case "consul":
		return consulclient.New(addrlist, auth, timeout)
	}
	return nil, errors.Errorf("invalid coordinator name = %s", coordinator)
}
