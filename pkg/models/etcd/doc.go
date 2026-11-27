// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package etcd implements the models.Client contract on top of the official
// etcd v3 client (go.etcd.io/etcd/client/v3).
//
// Architecture rule (D2): this package is the only place that may import
// go.etcd.io/etcd/client/v3. pkg/models, pkg/topom, pkg/proxy and
// pkg/migration must never import the etcd client nor this package. The
// dependency is strictly one-way: this package may import pkg/models (for the
// Client interface), but pkg/models must never import pkg/models/etcd.
//
// Key space: paths are used as etcd keys verbatim (Codis paths such as
// /codis3/{product}/... carry no trailing slash). List returns the direct
// children of a path, so /codis3/foo and /codis3/foo-bar stay isolated.
//
// Known limitation: a plaintext (http) endpoint that is actually served by a
// TLS listener cannot be detected at construction time, because no TLS
// configuration exists to probe with; the mismatch surfaces later as an RPC
// deadline exceeded. As soon as TLS is configured (explicit ca_file /
// cert_file / key_file, or https endpoints), New performs a bounded TLS
// handshake preflight and reports the underlying handshake error instead. The
// preflight probes every endpoint concurrently under one shared deadline
// (dial_timeout) and passes as soon as any endpoint completes a handshake.
package etcd
