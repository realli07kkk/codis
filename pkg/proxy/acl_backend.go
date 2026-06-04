// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"github.com/CodisLabs/codis/pkg/proxy/redis"
	redisutils "github.com/CodisLabs/codis/pkg/utils/redis"
)

type RedisAuthIdentity = redisutils.RedisAuthIdentity

func (s *Router) aclDryRunLocal(r *Request) error {
	if r == nil || r.ACLIdentity == nil {
		return nil
	}
	return s.aclDryRunAny(r)
}

func (s *Router) aclDryRunKey(r *Request, key []byte) error {
	if r == nil || r.ACLIdentity == nil {
		return nil
	}
	slot := int(Hash(key) % MaxSlotNum)
	if slot < 0 || slot >= MaxSlotNum {
		return ErrInvalidSlotId
	}
	return s.aclDryRunSlot(r, &s.slots[slot])
}

func (s *Router) aclDryRunAny(r *Request) error {
	for i := range s.slots {
		slot := &s.slots[i]
		slot.lock.RLock()
		ok := slot.backend.bc != nil
		slot.lock.RUnlock()
		if ok {
			return s.aclDryRunSlot(r, slot)
		}
	}
	return ErrSlotIsNotReady
}

func (s *Router) aclDryRunSlot(r *Request, slot *Slot) error {
	slot.lock.RLock()
	resp, err := aclDryRunOnBackend(slot.backend.bc, r.Database, r.Seed16(), r.ACLIdentity.Username, r.Multi)
	slot.lock.RUnlock()
	if err != nil {
		return err
	}
	if resp != nil {
		r.Resp = resp
	}
	return nil
}

func aclDryRunOnBackend(bc *sharedBackendConn, database int32, seed uint, username string, multi []*redis.Resp) (*redis.Resp, error) {
	return (&forwardHelper{}).aclDryRunOnBackend(bc, database, seed, username, multi)
}
