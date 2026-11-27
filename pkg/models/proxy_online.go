// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"path/filepath"
)

// OnlineRecord is the value stored under OnlineProxyPath by a proxy that
// enabled etcd_register_online (contract §4). Addr is the address clients use
// to reach the proxy; the key is bound to a lease owned by the registering
// process, so the record disappears when that process stops renewing.
type OnlineRecord struct {
	Token string `json:"token"`
	Addr  string `json:"addr"`
}

// OnlineProxyDir is the prefix holding the online records of one product.
func OnlineProxyDir(product string) string {
	return filepath.Join(ProductDir(product), "online-proxies")
}

// OnlineProxyPath is the key of the online record of one proxy token.
func OnlineProxyPath(product string, token string) string {
	return filepath.Join(OnlineProxyDir(product), token)
}
