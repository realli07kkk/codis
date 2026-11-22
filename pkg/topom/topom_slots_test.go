// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func getSlotMapping(x *testing.T, t *Topom, sid int) *models.SlotMapping {
	ctx, err := t.newContext()
	assert.MustNoError(x, err)
	m, err := ctx.getSlotMapping(sid)
	assert.MustNoError(x, err)
	assert.Must(x, m.Id == sid)
	return m
}

func checkSlots(x *testing.T, t *Topom, c *proxy.ApiClient) {
	ctx, err := t.newContext()
	assert.MustNoError(x, err)

	slots1 := ctx.toSlotSlice(ctx.slots, nil)
	assert.Must(x, len(slots1) == MaxSlotNum)

	slots2, err := c.Slots()
	assert.MustNoError(x, err)
	assert.Must(x, len(slots2) == MaxSlotNum)

	for i := 0; i < len(slots1); i++ {
		a := slots1[i]
		b := slots2[i]
		assert.Must(x, a.Id == b.Id)
		assert.Must(x, a.Locked == b.Locked)
		assert.Must(x, a.BackendAddr == b.BackendAddr)
		assert.Must(x, a.MigrateFrom == b.MigrateFrom)
	}
}

func TestSlotCreateAction(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100
	const gid = 200

	assert.Must(x, t.SlotCreateAction(sid, gid) != nil)

	g := &models.Group{Id: gid}
	contextUpdateGroup(x, t, g)
	assert.Must(x, t.SlotCreateAction(sid, gid) != nil)

	g.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: "server"},
	}
	contextUpdateGroup(x, t, g)
	assert.MustNoError(x, t.SlotCreateAction(sid, gid))

	assert.Must(x, t.SlotCreateAction(sid, gid) != nil)

	m := getSlotMapping(x, t, sid)
	assert.Must(x, m.GroupId == 0)
	assert.Must(x, m.Action.State == models.ActionPending)
	assert.Must(x, m.Action.Index > 0 && m.Action.TargetId == gid)

	m = &models.SlotMapping{Id: sid, GroupId: gid}
	contextUpdateSlotMapping(x, t, m)
	assert.Must(x, t.SlotCreateAction(sid, gid) != nil)
}

func TestSlotRemoveAction(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100
	assert.Must(x, t.SlotRemoveAction(sid) != nil)

	sstates := []string{
		models.ActionNothing,
		models.ActionPending,
		models.ActionPreparing,
		models.ActionPrepared,
		models.ActionMigrating,
		models.ActionFinished,
	}

	m := &models.SlotMapping{Id: sid}
	for _, m.Action.State = range sstates {
		contextUpdateSlotMapping(x, t, m)
		if m.Action.State == models.ActionPending {
			assert.MustNoError(x, t.SlotRemoveAction(sid))
		} else {
			assert.Must(x, t.SlotRemoveAction(sid) != nil)
		}
	}
}

func prepareSlotAction(x *testing.T, t *Topom, sid int, must bool) *models.SlotMapping {
	i, ok, err := t.SlotActionPrepare()
	if must {
		assert.MustNoError(x, err)
		assert.Must(x, ok && sid == i)
	} else {
		assert.Must(x, ok == false)
	}
	return getSlotMapping(x, t, sid)
}

func completeSlotAction(x *testing.T, t *Topom, sid int, must bool) *models.SlotMapping {
	err := t.SlotActionComplete(sid)
	if must {
		assert.MustNoError(x, err)
	}
	return getSlotMapping(x, t, sid)
}

func TestSlotActionSimple(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100
	const gid = 200

	m := &models.SlotMapping{Id: sid}
	m.Action.State = models.ActionPending
	m.Action.TargetId = gid
	contextUpdateSlotMapping(x, t, m)

	m1 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m1.GroupId == 0)
	assert.Must(x, m1.Action.State == models.ActionMigrating)

	m2 := completeSlotAction(x, t, sid, true)
	assert.Must(x, m2.GroupId == gid)
	assert.Must(x, m2.Action.State == models.ActionNothing)
}

func TestSlotActionPending(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100
	const gid = 200

	reset := func() {
		m := &models.SlotMapping{Id: sid}
		m.Action.State = models.ActionPending
		m.Action.TargetId = gid
		contextUpdateSlotMapping(x, t, m)
	}

	reset()

	m1 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m1.Action.State == models.ActionMigrating)

	reset()

	p, c := openProxy(x)
	defer c.Shutdown()

	contextCreateProxy(x, t, p)

	m2 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m2.Action.State == models.ActionMigrating)
	checkSlots(x, t, c)

	assert.MustNoError(x, c.Shutdown())

	reset()

	m3 := prepareSlotAction(x, t, sid, false)
	assert.Must(x, m3.Action.State == models.ActionPreparing)
}

func TestSlotActionPreparing(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100
	const gid = 200

	reset := func() {
		m := &models.SlotMapping{Id: sid}
		m.Action.State = models.ActionPreparing
		m.Action.TargetId = gid
		contextUpdateSlotMapping(x, t, m)
	}

	reset()

	m1 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m1.Action.State == models.ActionMigrating)

	reset()

	p1, c1 := openProxy(x)
	defer c1.Shutdown()

	p2, c2 := openProxy(x)
	defer c2.Shutdown()

	contextCreateProxy(x, t, p1)
	contextCreateProxy(x, t, p2)

	m2 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m2.Action.State == models.ActionMigrating)
	checkSlots(x, t, c1)
	checkSlots(x, t, c2)

	assert.MustNoError(x, c1.Shutdown())

	reset()

	m3 := prepareSlotAction(x, t, sid, false)
	assert.Must(x, m3.Action.State == models.ActionPreparing)
	checkSlots(x, t, c2)
}

func TestSlotActionPrepared(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const gid1, gid2 = 200, 300
	const server1 = "server1:port"
	const server2 = "server2:port"

	g1 := &models.Group{Id: gid1}
	g1.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server1},
	}
	contextCreateGroup(x, t, g1)
	g2 := &models.Group{Id: gid2}
	g2.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server2},
	}
	contextCreateGroup(x, t, g2)

	const sid = 100

	reset := func() {
		m := &models.SlotMapping{Id: sid, GroupId: gid1}
		m.Action.State = models.ActionPrepared
		m.Action.TargetId = gid2
		contextUpdateSlotMapping(x, t, m)
	}

	reset()

	m1 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m1.Action.State == models.ActionMigrating)

	reset()

	p1, c1 := openProxy(x)
	defer c1.Shutdown()

	p2, c2 := openProxy(x)
	defer c2.Shutdown()

	contextCreateProxy(x, t, p1)
	contextCreateProxy(x, t, p2)

	m2 := prepareSlotAction(x, t, sid, true)
	assert.Must(x, m2.Action.State == models.ActionMigrating)
	checkSlots(x, t, c1)
	checkSlots(x, t, c2)

	assert.MustNoError(x, c1.Shutdown())

	reset()

	m3 := prepareSlotAction(x, t, sid, false)
	assert.Must(x, m3.Action.State == models.ActionPrepared)

	slots, err := c2.Slots()
	assert.MustNoError(x, err)
	assert.Must(x, len(slots) == MaxSlotNum)

	s := slots[sid]
	assert.Must(x, s.Locked == false)
	assert.Must(x, s.BackendAddr == server2 && s.MigrateFrom == server1)
}

func TestSlotActionMigrating(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const gid1, gid2 = 200, 300
	const server1 = "server1:port"
	const server2 = "server2:port"

	g1 := &models.Group{Id: gid1}
	g1.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server1},
	}
	contextCreateGroup(x, t, g1)
	g2 := &models.Group{Id: gid2}
	g2.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server2},
	}
	contextCreateGroup(x, t, g2)

	const sid = 100

	reset := func() {
		m := &models.SlotMapping{Id: sid, GroupId: gid1}
		m.Action.State = models.ActionMigrating
		m.Action.TargetId = gid2
		contextUpdateSlotMapping(x, t, m)
	}

	reset()

	m1 := completeSlotAction(x, t, sid, true)
	assert.Must(x, m1.GroupId == gid2)
	assert.Must(x, m1.Action.State == models.ActionNothing)

	reset()

	p1, c1 := openProxy(x)
	defer c1.Shutdown()

	p2, c2 := openProxy(x)
	defer c2.Shutdown()

	contextCreateProxy(x, t, p1)
	contextCreateProxy(x, t, p2)

	m2 := completeSlotAction(x, t, sid, true)
	assert.Must(x, m2.GroupId == gid2)
	assert.Must(x, m2.Action.State == models.ActionNothing)
	checkSlots(x, t, c1)
	checkSlots(x, t, c2)

	assert.MustNoError(x, c1.Shutdown())

	reset()

	m3 := completeSlotAction(x, t, sid, false)
	assert.Must(x, m3.GroupId == gid1)
	assert.Must(x, m3.Action.TargetId == gid2)
	assert.Must(x, m3.Action.State == models.ActionFinished)

	slots, err := c2.Slots()
	assert.MustNoError(x, err)
	assert.Must(x, len(slots) == MaxSlotNum)

	s := slots[sid]
	assert.Must(x, s.Locked == false)
	assert.Must(x, s.BackendAddr == server2 && s.MigrateFrom == "")
}

func TestSlotActionFinished(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const gid1, gid2 = 200, 300
	const server1 = "server1:port"
	const server2 = "server2:port"

	g1 := &models.Group{Id: gid1}
	g1.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server1},
	}
	contextCreateGroup(x, t, g1)
	g2 := &models.Group{Id: gid2}
	g2.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: server2},
	}
	contextCreateGroup(x, t, g2)

	const sid = 100

	reset := func() {
		m := &models.SlotMapping{Id: sid, GroupId: gid1}
		m.Action.State = models.ActionFinished
		m.Action.TargetId = gid2
		contextUpdateSlotMapping(x, t, m)
	}

	reset()

	m1 := completeSlotAction(x, t, sid, true)
	assert.Must(x, m1.GroupId == gid2)
	assert.Must(x, m1.Action.State == models.ActionNothing)

	reset()

	p1, c1 := openProxy(x)
	defer c1.Shutdown()

	p2, c2 := openProxy(x)
	defer c2.Shutdown()

	contextCreateProxy(x, t, p1)
	contextCreateProxy(x, t, p2)

	m2 := completeSlotAction(x, t, sid, true)
	assert.Must(x, m2.GroupId == gid2)
	assert.Must(x, m2.Action.State == models.ActionNothing)
	checkSlots(x, t, c1)
	checkSlots(x, t, c2)

	assert.MustNoError(x, c1.Shutdown())

	reset()

	m3 := completeSlotAction(x, t, sid, false)
	assert.Must(x, m3.GroupId == gid1)
	assert.Must(x, m3.Action.TargetId == gid2)
	assert.Must(x, m3.Action.State == models.ActionFinished)

	slots, err := c2.Slots()
	assert.MustNoError(x, err)
	assert.Must(x, len(slots) == MaxSlotNum)

	s := slots[sid]
	assert.Must(x, s.Locked == false)
	assert.Must(x, s.BackendAddr == server2 && s.MigrateFrom == "")
}

func TestSlotsAssignGroup(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	m := &models.SlotMapping{Id: 100, GroupId: 200}
	m.Action.State = models.ActionPending

	assert.Must(x, t.SlotsAssignGroup([]*models.SlotMapping{m}) != nil)

	g := &models.Group{Id: 200, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: "server"},
	}}
	contextCreateGroup(x, t, g)
	assert.Must(x, t.SlotsAssignGroup([]*models.SlotMapping{m}) != nil)

	m.Action.State = models.ActionNothing
	assert.MustNoError(x, t.SlotsAssignGroup([]*models.SlotMapping{m}))
}

func TestSlotsRebalance(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	groupBy := func(plans map[int]int) map[int]int {
		d := make(map[int]int)
		for sid, gid := range plans {
			assert.Must(x, sid >= 0 && sid < MaxSlotNum)
			m := getSlotMapping(x, t, sid)
			assert.Must(x, m.Action.State == models.ActionNothing)
			assert.Must(x, m.GroupId != gid)
			d[gid]++
		}
		return d
	}

	plans1, err := t.SlotsRebalance(false)
	assert.Must(x, plans1 == nil && err != nil)

	g1 := &models.Group{Id: 100, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: "server1"},
	}}
	contextCreateGroup(x, t, g1)

	plans2, err := t.SlotsRebalance(false)
	assert.MustNoError(x, err)
	assert.Must(x, len(plans2) == MaxSlotNum)
	d2 := groupBy(plans2)
	assert.Must(x, len(d2) == 1 && d2[g1.Id] == MaxSlotNum)

	g2 := &models.Group{Id: 200, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: "server2"},
	}}
	contextCreateGroup(x, t, g2)

	plans3, err := t.SlotsRebalance(false)
	assert.MustNoError(x, err)
	assert.Must(x, len(plans3) == MaxSlotNum)
	d3 := groupBy(plans3)
	assert.Must(x, len(d3) == 2 && d3[g1.Id] == d3[g2.Id])

	for i := 0; i < MaxSlotNum; i++ {
		m := &models.SlotMapping{Id: i, GroupId: g1.Id}
		contextUpdateSlotMapping(x, t, m)
	}
	plans4, err := t.SlotsRebalance(false)
	assert.MustNoError(x, err)
	assert.Must(x, len(plans4) == MaxSlotNum/2)
	d4 := groupBy(plans4)
	assert.Must(x, len(d4) == 1 && d4[g2.Id] == len(plans4))

	for i := 0; i < MaxSlotNum; i++ {
		m := &models.SlotMapping{Id: i}
		if i >= MaxSlotNum/4 {
			m.Action.State = models.ActionPending
			m.Action.TargetId = g1.Id
		}
		contextUpdateSlotMapping(x, t, m)
	}
	plans5, err := t.SlotsRebalance(false)
	assert.MustNoError(x, err)
	assert.Must(x, len(plans5) == MaxSlotNum/4)
	d5 := groupBy(plans5)
	assert.Must(x, len(d5) == 1 && d5[g2.Id] == len(plans5))
}
