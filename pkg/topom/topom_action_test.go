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
	t := openTopom(x)
	defer t.Close()

	s := newFakeServer(x)
	defer s.Close()

	const sid = 100
	const gid = 200

	reset := func() {
		m := &models.SlotMapping{Id: sid}
		m.Action.State = models.ActionPending
		m.Action.TargetId = gid
		contextUpdateSlotMapping(x, t, m)
		g := &models.Group{Id: gid}
		g.Servers = []*models.GroupServer{
			&models.GroupServer{Addr: s.Addr},
		}
		contextUpdateGroup(x, t, g)
	}

	reset()

	prepareSlotAction(x, t, sid, true)
	exec1, err := t.newSlotActionExecutor(sid)
	assert.MustNoError(x, err)
	assert.Must(x, t.action.executor.Int64() != 0)
	assert.Must(x, exec1 != nil)
	exec1(0)
	assert.Must(x, t.action.executor.Int64() == 0)

	reset()

	prepareSlotAction(x, t, sid, true)
	g2 := getGroup(x, t, gid)
	g2.Promoting.State = models.ActionPrepared
	contextUpdateGroup(x, t, g2)
	exec2, err := t.newSlotActionExecutor(sid)
	assert.MustNoError(x, err)
	assert.Must(x, exec2 == nil)
	assert.Must(x, t.action.executor.Int64() == 0)

	reset()

	assert.MustNoError(x, t.ProcessSlotAction())
	m := getSlotMapping(x, t, sid)
	assert.Must(x, m.Action.State == models.ActionNothing)
	assert.Must(x, m.GroupId == gid)
}

func TestSlotActionExecutorSemiAsyncRedis8Response(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const migrationRemaining = 5
	source := newFakeServerWithHandler(x, func(args []string) *redis.Resp {
		if args[0] != "SLOTSMGRTTAGSLOT-ASYNC" {
			return nil
		}
		return redis.NewArray([]*redis.Resp{
			redis.NewInt([]byte("1")),
			redis.NewInt([]byte(strconv.Itoa(migrationRemaining))),
		})
	})
	defer source.Close()

	target := newFakeServer(x)
	defer target.Close()

	const sid = 100
	const gid1 = 200
	const gid2 = 300

	contextUpdateGroup(x, t, &models.Group{Id: gid1, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: source.Addr},
	}})
	contextUpdateGroup(x, t, &models.Group{Id: gid2, Servers: []*models.GroupServer{
		&models.GroupServer{Addr: target.Addr},
	}})
	m := &models.SlotMapping{Id: sid, GroupId: gid1}
	m.Action.State = models.ActionMigrating
	m.Action.TargetId = gid2
	contextUpdateSlotMapping(x, t, m)

	exec, err := t.newSlotActionExecutor(sid)
	assert.MustNoError(x, err)
	assert.Must(x, exec != nil)
	remains, nextdb, err := exec(0)
	assert.MustNoError(x, err)
	assert.Must(x, remains == migrationRemaining)
	assert.Must(x, nextdb == 0)
}

func TestSyncAction(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	s := newFakeServer(x)
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
		contextUpdateGroup(x, t, g)
	}

	reset()

	assert.MustNoError(x, t.SyncCreateAction(server2))
	assert.Must(x, t.SyncCreateAction(server2) != nil)
	g1 := getGroup(x, t, gid)
	assert.Must(x, len(g1.Servers) == 2)
	assert.Must(x, g1.Servers[1].Action.State == models.ActionPending)
	assert.MustNoError(x, t.SyncRemoveAction(server2))
	assert.Must(x, t.SyncRemoveAction(server1) != nil)

	reset()

	assert.MustNoError(x, t.SyncCreateAction(server2))
	addr2, err := t.SyncActionPrepare()
	assert.MustNoError(x, err)
	assert.Must(x, addr2 == server2)
	g2 := getGroup(x, t, gid)
	assert.Must(x, len(g2.Servers) == 2)
	assert.Must(x, g2.Servers[1].Action.State == models.ActionSyncing)

	reset()

	assert.MustNoError(x, t.SyncCreateAction(server2))
	exec3, err := t.newSyncActionExecutor(server2)
	assert.MustNoError(x, err)
	assert.Must(x, exec3 == nil)

	addr3, err := t.SyncActionPrepare()
	assert.MustNoError(x, err)
	assert.Must(x, addr3 == server2)

	exec4, err := t.newSyncActionExecutor(addr3)
	assert.MustNoError(x, err)
	assert.Must(x, exec4 != nil)
	assert.MustNoError(x, t.SyncActionComplete(server2, true))
	assert.MustNoError(x, t.SyncRemoveAction(server2))

	g3 := getGroup(x, t, gid)
	assert.Must(x, len(g3.Servers) == 2)
	assert.Must(x, g3.Servers[1].Action.State == models.ActionNothing)
}
