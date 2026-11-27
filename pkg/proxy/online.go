// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"context"

	"github.com/CodisLabs/codis/pkg/models"
)

// OnlineLister reads the online proxy records of the product with one
// consistent range read. Injected by the entrypoint; pkg/proxy never
// imports the etcd client (architecture rule D2).
type OnlineLister interface {
	List(ctx context.Context) ([]models.OnlineRecord, error)
}

// OnlineRegistry is the registration side injected when etcd_register_online
// is on.
type OnlineRegistry interface {
	OnlineLister
	// Register blocks until ctx is done; the caller runs it in a goroutine so
	// going online never waits for the registry.
	Register(ctx context.Context)
	// Close stops the registration loop, revokes the own lease with a bound
	// and closes the client (contract R3). It is called first by Proxy.Close.
	Close() error
}

// OnlineRegistryFactory builds the registration implementation once the
// product, the proxy token and the client-facing address are known. The
// token only becomes known after the dashboard issues the proxy model, so
// the factory keeps the coordinator connection lazy: it is called with the
// proxy lock held, must not perform I/O or block, and the returned registry
// connects on its own when Register runs.
type OnlineRegistryFactory func(product, token, addr string) OnlineRegistry

// registryLister exposes the lazily created registry to discovery. It is
// installed whenever a factory exists, because the registry itself does not
// exist yet; before the token is known there is simply nothing to discover,
// which is a successful empty read rather than a refresh failure.
type registryLister struct {
	proxy *Proxy
}

func (l *registryLister) List(ctx context.Context) ([]models.OnlineRecord, error) {
	if registry := l.proxy.registry(); registry != nil {
		return registry.List(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
