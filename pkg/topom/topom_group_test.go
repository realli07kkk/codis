// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func getGroup(x *testing.T, t *Topom, gid int) *models.Group {
	ctx, err := t.newContext()
	assert.MustNoError(x, err)
	g, err := ctx.getGroup(gid)
	assert.MustNoError(x, err)
	return g
}

func TestGroupCreate(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	assert.Must(x, t.CreateGroup(0) != nil)
	assert.Must(x, t.CreateGroup(-1) != nil)
	assert.Must(x, t.CreateGroup(10000) != nil)

	assert.MustNoError(x, t.CreateGroup(1))
	assert.Must(x, t.CreateGroup(1) != nil)

	g := getGroup(x, t, 1)
	assert.Must(x, g.Id == 1)
	assert.Must(x, g.Promoting.State == models.ActionNothing)
	assert.Must(x, len(g.Servers) == 0)
}

func TestGroupRemove(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const gid = 200

	assert.Must(x, t.RemoveGroup(gid) != nil)

	g := &models.Group{Id: gid}
	contextCreateGroup(x, t, g)
	assert.MustNoError(x, t.RemoveGroup(gid))

	g.Servers = []*models.GroupServer{
		&models.GroupServer{Addr: "server"},
	}
	contextUpdateGroup(x, t, g)
	assert.Must(x, t.RemoveGroup(gid) != nil)

	g.Servers = nil
	contextUpdateGroup(x, t, g)
	assert.MustNoError(x, t.RemoveGroup(gid))
}

func TestGroupAddServer(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const gid1 = 100
	const gid2 = 200
	const server1 = "server1:port"
	const server2 = "server2:port"

	assert.MustNoError(x, t.CreateGroup(gid1))
	assert.MustNoError(x, t.GroupAddServer(gid1, "", server1))
	assert.Must(x, t.GroupAddServer(gid1, "", server1) != nil)

	assert.MustNoError(x, t.CreateGroup(gid2))
	assert.MustNoError(x, t.GroupAddServer(gid2, "", server2))
	assert.Must(x, t.GroupAddServer(gid2, "", server1) != nil)

	g1 := getGroup(x, t, gid1)
	g1.Servers = nil
	contextUpdateGroup(x, t, g1)

	assert.MustNoError(x, t.GroupAddServer(gid2, "", server1))

	g2 := getGroup(x, t, gid2)
	assert.Must(x, len(g2.Servers) == 2)
	assert.Must(x, g2.Servers[0].Addr == server2)
	assert.Must(x, g2.Servers[1].Addr == server1)
}

func TestGroupDelServer(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100
	const gid = 200
	const server1 = "server1:port"
	const server2 = "server2:port"

	reset := func() {
		g := &models.Group{Id: gid}
		g.Servers = []*models.GroupServer{
			&models.GroupServer{Addr: server1},
			&models.GroupServer{Addr: server2},
		}
		contextUpdateGroup(x, t, g)
	}

	reset()

	assert.Must(x, t.RemoveGroup(gid) != nil)
	assert.MustNoError(x, t.GroupDelServer(gid, server2))
	assert.MustNoError(x, t.GroupDelServer(gid, server1))
	assert.MustNoError(x, t.RemoveGroup(gid))

	reset()

	assert.Must(x, t.GroupDelServer(gid, server1) != nil)
	assert.MustNoError(x, t.GroupDelServer(gid, server2))
	assert.MustNoError(x, t.GroupDelServer(gid, server1))
	assert.Must(x, t.GroupDelServer(gid, server1) != nil)

	reset()

	m := &models.SlotMapping{Id: sid, GroupId: gid}
	contextUpdateSlotMapping(x, t, m)

	assert.MustNoError(x, t.GroupDelServer(gid, server2))
	assert.Must(x, t.GroupDelServer(gid, server1) != nil)

	m.GroupId = 0
	contextUpdateSlotMapping(x, t, m)

	assert.MustNoError(x, t.GroupDelServer(gid, server1))
	assert.MustNoError(x, t.RemoveGroup(gid))
}

func TestGroupPromote(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	s := newFakeServer(x)
	defer s.Close()

	const sid = 100
	const gid = 200
	const server1 = "server1:port"
	server2 := s.Addr

	reset := func() {
		g := &models.Group{Id: gid}
		g.Servers = []*models.GroupServer{
			&models.GroupServer{Addr: server1},
			&models.GroupServer{Addr: server2},
		}
		contextUpdateGroup(x, t, g)
	}

	reset()

	assert.Must(x, t.GroupPromoteServer(gid, server1) != nil)
	g1 := getGroup(x, t, gid)
	assert.Must(x, g1.Promoting.State == models.ActionNothing)
	assert.Must(x, len(g1.Servers) == 2)
	assert.Must(x, g1.Servers[0].Addr == server1)
	assert.Must(x, g1.Servers[1].Addr == server2)

	reset()

	assert.MustNoError(x, t.GroupPromoteServer(gid, server2))
	g2 := getGroup(x, t, gid)
	assert.Must(x, g2.Promoting.State == models.ActionNothing)
	assert.Must(x, len(g2.Servers) == 2)
	assert.Must(x, g2.Servers[0].Addr == server2)
	assert.Must(x, g2.Servers[1].Addr == server1)

	reset()

	m := &models.SlotMapping{Id: sid}
	m.Action.State = models.ActionMigrating
	m.Action.TargetId = gid
	contextUpdateSlotMapping(x, t, m)

	p, c := openProxy(x)
	defer c.Shutdown()

	contextCreateProxy(x, t, p)
	assert.MustNoError(x, c.Shutdown())

	assert.Must(x, t.GroupPromoteServer(gid, server2) != nil)

	g3 := getGroup(x, t, gid)
	assert.Must(x, g3.Promoting.State == models.ActionPreparing)
	contextRemoveProxy(x, t, p)
	assert.MustNoError(x, t.GroupPromoteServer(gid, server2))
	g4 := getGroup(x, t, gid)
	assert.Must(x, g4.Promoting.State == models.ActionNothing)
	assert.Must(x, len(g4.Servers) == 2)
	assert.Must(x, g4.Servers[0].Addr == server2)
	assert.Must(x, g4.Servers[1].Addr == server1)
}
