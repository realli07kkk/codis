// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/assert"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

func newApiClient(t *Topom) *ApiClient {
	c := NewApiClient(t.model.AdminAddr)
	c.SetXAuth(t.config.ProductName)
	return c
}

func TestApiTopom(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	c := newApiClient(t)

	o, err := c.Model()
	assert.MustNoError(x, err)
	assert.Must(x, o != nil)

	assert.MustNoError(x, c.XPing())

	s, err := c.Stats()
	assert.MustNoError(x, err)
	assert.Must(x, s != nil)

	assert.MustNoError(x, c.LogLevel(log.LevelError))

	assert.MustNoError(x, c.Shutdown())
}

func TestApiSlots(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	s := newFakeServer(x)
	defer s.Close()

	const sid = 100
	const gid = 200

	c := newApiClient(t)

	assert.MustNoError(x, c.CreateGroup(gid))
	assert.MustNoError(x, c.GroupAddServer(gid, "", s.Addr))
	assert.MustNoError(x, c.SlotCreateAction(sid, gid))
	assert.MustNoError(x, c.SlotRemoveAction(sid))
	assert.MustNoError(x, c.SlotCreateActionRange(0, MaxSlotNum-1, gid))
	assert.MustNoError(x, c.SetSlotActionInterval(2000))
	assert.MustNoError(x, c.SetSlotActionDisabled(true))

	assert.MustNoError(x, c.SlotsAssignGroup([]*models.SlotMapping{
		&models.SlotMapping{Id: sid, GroupId: gid},
	}))

	slots, err := c.Slots()
	assert.MustNoError(x, err)
	assert.Must(x, len(slots) == MaxSlotNum)
	assert.Must(x, slots[sid].BackendAddr == s.Addr)
}

func TestApiGroup(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	s1 := newFakeServer(x)
	defer s1.Close()

	s2 := newFakeServer(x)
	defer s2.Close()

	const gid = 100

	c := newApiClient(t)

	assert.MustNoError(x, c.CreateGroup(gid))
	assert.MustNoError(x, c.GroupAddServer(gid, "", s1.Addr))
	assert.MustNoError(x, c.GroupAddServer(gid, "", s2.Addr))
	assert.MustNoError(x, c.SyncCreateAction(s1.Addr))
	assert.MustNoError(x, c.SyncRemoveAction(s1.Addr))
	assert.MustNoError(x, c.GroupPromoteServer(gid, s2.Addr))
	assert.MustNoError(x, c.GroupDelServer(gid, s1.Addr))
	assert.MustNoError(x, c.GroupDelServer(gid, s2.Addr))
	assert.MustNoError(x, c.RemoveGroup(gid))
}

func TestApiProxy(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	c := newApiClient(t)

	p, z := openProxy(x)
	defer z.Shutdown()

	assert.MustNoError(x, c.CreateProxy(p.AdminAddr))
	assert.MustNoError(x, c.ReinitProxy(p.Token))
	assert.MustNoError(x, c.RemoveProxy(p.Token, false))
}
