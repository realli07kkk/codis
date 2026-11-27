// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcdonline

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// List reads every online record of the product with ONE consistent range
// read over the online prefix (contract §4). Malformed records are skipped
// with a warning, so a single bad key cannot break discovery; the result is
// sorted by token and contains no duplicates.
func (r *Registry) List(ctx context.Context) ([]models.OnlineRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Without a connection there is no readable list. Reporting an error (not
	// an empty result) keeps the caller's last successful snapshot, so a
	// coordinator outage never silently drops every peer.
	client := r.driver()
	if client == nil {
		return nil, ErrNoClient
	}
	dir := models.OnlineProxyDir(r.product)
	kvs, err := client.RangeWithPrefix(ctx, dir)
	if err != nil {
		return nil, err
	}
	prefix := dir + "/"
	records := make([]models.OnlineRecord, 0, len(kvs))
	for _, kv := range kvs {
		token, ok := strings.CutPrefix(kv.Key, prefix)
		if !ok || token == "" || strings.Contains(token, "/") {
			log.Warnf("online registration: skip key %s, not the record of exactly one token", kv.Key)
			continue
		}
		var rec models.OnlineRecord
		if err := json.Unmarshal(kv.Value, &rec); err != nil {
			log.Warnf("online registration: skip record %s, unreadable value: %v", kv.Key, err)
			continue
		}
		// The key is the authority for which proxy the record describes: a
		// record that disagrees with it, or that carries no reachable address,
		// cannot be used and is treated as bad.
		if rec.Token != token || rec.Addr == "" {
			log.Warnf("online registration: skip record %s, token %q addr %q does not match the key", kv.Key, rec.Token, rec.Addr)
			continue
		}
		records = append(records, rec)
	}
	slices.SortFunc(records, func(a, b models.OnlineRecord) int {
		return strings.Compare(a.Token, b.Token)
	})
	return records, nil
}
