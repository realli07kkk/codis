// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestSlotsCache(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const sid = 100

	check := func(gid int) {
		m := getSlotMapping(x, t, sid)
		assert.Must(x, m.GroupId == gid)
	}

	m := &models.SlotMapping{Id: sid}
	check(0)

	t.dirtySlotsCache(sid)
	m.GroupId = 100
	assert.MustNoError(x, t.storeUpdateSlotMapping(m))
	check(100)

	t.dirtySlotsCache(sid)
	m.GroupId = 200
	check(100)

	t.dirtyCacheAll()
	m.GroupId = 200
	check(100)

	t.dirtyCacheAll()
	m.GroupId = 300
	assert.MustNoError(x, t.storeUpdateSlotMapping(m))
	check(300)
}

func TestGroupCache(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const gid = 100

	check := func(exists bool, state string) {
		ctx, err := t.newContext()
		assert.MustNoError(x, err)
		if !exists {
			assert.Must(x, ctx.group[gid] == nil)
		} else {
			g, err := ctx.getGroup(gid)
			assert.MustNoError(x, err)
			assert.Must(x, g.Id == gid && g.Promoting.State == state)
		}
	}

	g := &models.Group{Id: gid}
	check(false, "")

	t.dirtyGroupCache(gid)
	check(false, "")

	t.dirtyGroupCache(gid)
	assert.MustNoError(x, t.storeCreateGroup(g))
	check(true, models.ActionNothing)

	t.dirtyGroupCache(gid)
	g.Promoting.State = models.ActionPreparing
	check(true, models.ActionNothing)

	t.dirtyGroupCache(gid)
	g.Promoting.State = models.ActionPreparing
	assert.MustNoError(x, t.storeUpdateGroup(g))
	check(true, models.ActionPreparing)

	t.dirtyCacheAll()
	g.Promoting.State = models.ActionPrepared
	assert.MustNoError(x, t.storeUpdateGroup(g))
	check(true, models.ActionPrepared)

	t.dirtyGroupCache(gid)
	assert.MustNoError(x, t.storeRemoveGroup(g))
	check(false, "")
}

func TestProxyCache(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	const token = "fake_proxy_token"

	check := func(exists bool) {
		ctx, err := t.newContext()
		assert.MustNoError(x, err)
		if !exists {
			assert.Must(x, ctx.proxy[token] == nil)
		} else {
			p, err := ctx.getProxy(token)
			assert.MustNoError(x, err)
			assert.Must(x, p.Token == token)
		}
	}

	p := &models.Proxy{Token: token}
	check(false)

	t.dirtyProxyCache(p.Token)
	assert.MustNoError(x, t.storeCreateProxy(p))
	check(true)

	t.dirtyProxyCache(p.Token)
	assert.MustNoError(x, t.storeRemoveProxy(p))
	check(false)
}

func contextUpdateSlotMapping(x *testing.T, t *Topom, m *models.SlotMapping) {
	t.dirtySlotsCache(m.Id)
	assert.MustNoError(x, t.storeUpdateSlotMapping(m))
}

func contextCreateGroup(x *testing.T, t *Topom, g *models.Group) {
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(x, t.storeCreateGroup(g))
}

func contextRemoveGroup(x *testing.T, t *Topom, g *models.Group) {
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(x, t.storeRemoveGroup(g))
}

func contextUpdateGroup(x *testing.T, t *Topom, g *models.Group) {
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(x, t.storeUpdateGroup(g))
}

func contextCreateProxy(x *testing.T, t *Topom, p *models.Proxy) {
	t.dirtyProxyCache(p.Token)
	assert.MustNoError(x, t.storeCreateProxy(p))
}

func contextRemoveProxy(x *testing.T, t *Topom, p *models.Proxy) {
	t.dirtyProxyCache(p.Token)
	assert.MustNoError(x, t.storeRemoveProxy(p))
}
