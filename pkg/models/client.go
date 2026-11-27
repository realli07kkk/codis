// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package models holds the Codis metadata models and the client interface the
// coordinator is accessed through.
//
// The only backend on the etcd line is etcd (pkg/models/etcd). This package
// never imports a concrete coordinator: each component entry builds the
// client (cmd/internal/coordinator) and injects it into the store, the topom
// and the proxy. Tests use the in-memory pkg/models/modelstest client.
package models

type Client interface {
	Create(path string, data []byte) error
	Update(path string, data []byte) error
	Delete(path string) error

	Read(path string, must bool) ([]byte, error)
	List(path string, must bool) ([]string, error)

	Close() error
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
