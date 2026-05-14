// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"strconv"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestSlotAction(x *testing.T) {
	t := openTopom()
	defer t.Close()

	s := newFakeServer()
	defer s.Close()

	const sid = 100
	const gid = 200

	reset := func() {
		m := &models.SlotMapping{Id: sid}
		m.Action.State = models.ActionPending
		m.Action.TargetId = gid
		contextUpdateSlotMapping(t, m)
		g := &models.Group{Id: gid}
		g.Servers = []*models.GroupServer{
			&models.GroupServer{Addr: s.Addr},
		}
		contextUpdateGroup(t, g)
	}

	reset()

	prepareSlotAction(t, sid, true)
	exec1, err := t.newSlotActionExecutor(sid)
	assert.MustNoError(err)
	assert.Must(t.action.executor.Int64() != 0)
	assert.Must(exec1 != nil)
	exec1(0)
	assert.Must(t.action.executor.Int64() == 0)

	reset()

	prepareSlotAction(t, sid, true)
	g2 := getGroup(t, gid)
	g2.Promoting.State = models.ActionPrepared
	contextUpdateGroup(t, g2)
	exec2, err := t.newSlotActionExecutor(sid)
	assert.MustNoError(err)
	assert.Must(exec2 == nil)
	assert.Must(t.action.executor.Int64() == 0)

	reset()

	assert.MustNoError(t.ProcessSlotAction())
	m := getSlotMapping(t, sid)
	assert.Must(m.Action.State == models.ActionNothing)
	assert.Must(m.GroupId == gid)
}

func TestSlotActionExecutorSemiAsyncRedis8Response(x *testing.T) {
	t := openTopom()
	defer t.Close()

	const migrationRemaining = 5
	source := newFakeServerWithHandler(func(args []string) *redis.Resp {
		if args[0] != "SLOTSMGRTTAGSLOT-ASYNC" {
			return nil
		}
		return redis.NewArray([]*redis.Resp{
			redis.NewInt([]byte("1")),
			redis.NewInt([]byte(strconv.Itoa(migrationRemaining))),
		})
	})
	defer source.Close()

	target := newFakeServer()
	defer target.Close()

	const sid = 100
	const gid1 = 200
	const gid2 = 300

	contextUpdateGroup(t, &models.Group{Id: gid1, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: source.Addr},
	}})
	contextUpdateGroup(t, &models.Group{Id: gid2, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: target.Addr},
	}})
	m := &models.SlotMapping{Id: sid, GroupId: gid1}
	m.Action.State = models.ActionMigrating
	m.Action.TargetId = gid2
	contextUpdateSlotMapping(t, m)

	exec, err := t.newSlotActionExecutor(sid)
	assert.MustNoError(err)
	assert.Must(exec != nil)
	remains, nextdb, err := exec(0)
	assert.MustNoError(err)
	assert.Must(remains == migrationRemaining)
	assert.Must(nextdb == 0)
}

func TestSyncAction(x *testing.T) {
	t := openTopom()
	defer t.Close()

	s := newFakeServer()
	defer s.Close()

	const gid = 200
	const server1 = "server1:port"
	server2 := s.Addr

	reset := func() {
		g := &models.Group{Id: gid}
		g.Servers = []*models.GroupServer{
			&models.GroupServer{Addr: server1},
			&models.GroupServer{Addr: server2},
		}
		contextUpdateGroup(t, g)
	}

	reset()

	assert.MustNoError(t.SyncCreateAction(server2))
	assert.Must(t.SyncCreateAction(server2) != nil)
	g1 := getGroup(t, gid)
	assert.Must(len(g1.Servers) == 2)
	assert.Must(g1.Servers[1].Action.State == models.ActionPending)
	assert.MustNoError(t.SyncRemoveAction(server2))
	assert.Must(t.SyncRemoveAction(server1) != nil)

	reset()

	assert.MustNoError(t.SyncCreateAction(server2))
	addr2, err := t.SyncActionPrepare()
	assert.MustNoError(err)
	assert.Must(addr2 == server2)
	g2 := getGroup(t, gid)
	assert.Must(len(g2.Servers) == 2)
	assert.Must(g2.Servers[1].Action.State == models.ActionSyncing)

	reset()

	assert.MustNoError(t.SyncCreateAction(server2))
	exec3, err := t.newSyncActionExecutor(server2)
	assert.MustNoError(err)
	assert.Must(exec3 == nil)

	addr3, err := t.SyncActionPrepare()
	assert.MustNoError(err)
	assert.Must(addr3 == server2)

	exec4, err := t.newSyncActionExecutor(addr3)
	assert.MustNoError(err)
	assert.Must(exec4 != nil)
	assert.MustNoError(t.SyncActionComplete(server2, true))
	assert.MustNoError(t.SyncRemoveAction(server2))

	g3 := getGroup(t, gid)
	assert.Must(len(g3.Servers) == 2)
	assert.Must(g3.Servers[1].Action.State == models.ActionNothing)
}
