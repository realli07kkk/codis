// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"

	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

type cmdAdmin struct {
	product     string
	coordinator coordarg.Args
}

func (t *cmdAdmin) Main(cfg *adminConfig) {
	t.product = cfg.Product.Value
	t.coordinator = cfg.CoordinatorArg

	switch cfg.Command {
	case "show-lock":
		t.handleShowLock(cfg)
	case "remove-lock":
		t.handleRemoveLock(cfg)
	case "config-dump":
		t.handleConfigDump()
	case "config-convert":
		t.handleConfigConvert(cfg)
	case "config-restore":
		t.handleConfigRestore(cfg)
	case "dashboard-list":
		t.handleDashboardList(cfg)
	}
}

func (t *cmdAdmin) newTopomClient() models.Client {
	c, err := t.coordinator.NewClient("")
	if err != nil {
		log.PanicErrorf(err, "create etcd client failed")
	}
	return c
}

func (t *cmdAdmin) newTopomStore() *models.Store {
	if err := models.ValidateProduct(t.product); err != nil {
		log.PanicErrorf(err, "invalid product name")
	}
	client := t.newTopomClient()
	return models.NewStore(client, t.product)
}

// lockInfoJSON is the machine readable view of the product lock. The field
// order matches the operator contract.
type lockInfoJSON struct {
	Product        string `json:"product"`
	AdminAddr      string `json:"admin_addr"`
	OwnerID        string `json:"owner_id"`
	CreateRevision int64  `json:"create_revision"`
	ModRevision    int64  `json:"mod_revision"`
	Lease          int64  `json:"lease"`
}

// handleShowLock prints the current lock, or null when the product has none.
func (t *cmdAdmin) handleShowLock(cfg *adminConfig) {
	store := t.newTopomStore()
	defer store.Close()

	info, err := store.ShowLock()
	if err != nil {
		log.PanicErrorf(err, "show lock of product %s failed", t.product)
	}
	if info == nil {
		fmt.Println("null")
		return
	}
	out := lockInfoJSON{
		Product:        t.product,
		CreateRevision: info.CreateRevision,
		ModRevision:    info.ModRevision,
		Lease:          info.Lease,
	}
	if info.Topom != nil {
		out.AdminAddr = info.Topom.AdminAddr
		out.OwnerID = info.Topom.OwnerID
	}
	b, err := json.MarshalIndent(out, "", "    ")
	if err != nil {
		log.PanicErrorf(err, "json marshal failed")
	}
	fmt.Println(string(b))
}

// handleRemoveLock deletes the lock only if it still has the create_revision
// the operator named. Every outcome is reported, and a lock this request
// cannot prove it deleted is always left in place.
func (t *cmdAdmin) handleRemoveLock(cfg *adminConfig) {
	expected := cfg.ExpectedRevision.Value

	store := t.newTopomStore()
	defer store.Close()

	err := store.RemoveLock(expected)
	switch {
	case err == nil:
		fmt.Printf("removed lock of product %s (create_revision %d)\n", t.product, expected)
	case stderrors.Is(err, models.ErrLockGone):
		log.Panicf("lock of product %s no longer exists (expected create_revision %d); nothing can be confirmed as deleted by this request",
			t.product, expected)
	case stderrors.Is(err, models.ErrLockOwnershipChanged):
		info, showErr := store.ShowLock()
		switch {
		case showErr != nil:
			log.PanicErrorf(err, "lock of product %s is not the expected one and the current holder cannot be read; left intact", t.product)
		case info == nil || info.Topom == nil:
			log.PanicErrorf(err, "lock of product %s is not the expected one and has since disappeared; left intact", t.product)
		default:
			log.PanicErrorf(err, "lock is now held by another instance (create_revision %d, owner_id %s, admin_addr %s); left intact",
				info.CreateRevision, info.Topom.OwnerID, info.Topom.AdminAddr)
		}
	case stderrors.Is(err, models.ErrResultUnknown):
		log.Panicf("result unknown: %v; re-run --show-lock for product %s", err, t.product)
	case models.IsRejected(err):
		log.Panicf("rejected: %v", err)
	default:
		log.PanicErrorf(err, "remove lock of product %s failed", t.product)
	}
}

func (t *cmdAdmin) handleConfigDump() {
	t.dumpConfigV3()
}

type ConfigV3 struct {
	Slots []*models.SlotMapping `json:"slots,omitempty"`
	Group []*models.Group       `json:"group,omitempty"`
	Proxy []*models.Proxy       `json:"proxy,omitempty"`
}

func (t *cmdAdmin) dumpConfigV3() {
	store := t.newTopomStore()
	defer store.Close()

	group, err := store.ListGroup()
	if err != nil {
		log.PanicErrorf(err, "list group failed")
	}
	proxy, err := store.ListProxy()
	if err != nil {
		log.PanicErrorf(err, "list proxy failed")
	}

	if len(group) == 0 && len(proxy) == 0 {
		log.Panicf("cann't find product = %s [v3]", t.product)
	}

	slots, err := store.SlotMappings()
	if err != nil {
		log.PanicErrorf(err, "list slots failed")
	}

	config := &ConfigV3{
		Slots: slots,
		Group: models.SortGroup(group),
		Proxy: models.SortProxy(proxy),
	}

	b, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		log.PanicErrorf(err, "json marshal failed")
	}
	fmt.Println(string(b))
}

func (t *cmdAdmin) loadJsonConfigV1(file string) map[string]any {
	b, err := os.ReadFile(file)
	if err != nil {
		log.PanicErrorf(err, "read file '%s' failed", file)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		log.PanicErrorf(err, "json unmarshal failed")
	}
	return v.(map[string]any)
}

func (t *cmdAdmin) convertSlotsV1(slots map[int]*models.SlotMapping, v any) {
	m := v.(map[string]any)

	var sid = int(m["id"].(float64))
	var gid = int(m["group_id"].(float64))
	var status = m["state"].(map[string]any)["status"].(string)

	log.Debugf("found slot-%04d status = %s", sid, status)

	switch status {
	case "online":
	case "offline":
		return
	default:
		log.Panicf("invalid slot status")
	}

	if slots[sid] != nil {
		log.Panicf("slot-%04d already exists", sid)
	}
	slots[sid] = &models.SlotMapping{
		Id: sid, GroupId: gid,
	}
}

func (t *cmdAdmin) convertGroupV1(group map[int]*models.Group, v any) {
	m := v.(map[string]any)

	var addr = m["addr"].(string)
	var gid = int(m["group_id"].(float64))
	var master = m["type"].(string) == "master"

	log.Debugf("found group-%04d %s is master = %t", gid, addr, master)

	if gid <= 0 || gid > models.MaxGroupId {
		log.Panicf("invalid group = %d", gid)
	}

	if group[gid] == nil {
		group[gid] = &models.Group{Id: gid}
	}
	g := group[gid]

	servers := []*models.GroupServer{}
	if master {
		servers = append(servers, &models.GroupServer{Addr: addr})
		servers = append(servers, g.Servers...)
	} else {
		servers = append(servers, g.Servers...)
		servers = append(servers, &models.GroupServer{Addr: addr})
	}
	g.Servers = servers
}

func (t *cmdAdmin) handleConfigConvert(cfg *adminConfig) {
	defer func() {
		if x := recover(); x != nil {
			log.Panicf("convert config failed: %+v", x)
		}
	}()

	cfg1 := t.loadJsonConfigV1(argumentMust(cfg.ConfigConvert, "--config-convert"))
	cfg2 := &ConfigV3{}

	if slots := cfg1["slots"]; slots != nil {
		temp := make(map[int]*models.SlotMapping)
		for _, v := range slots.(map[string]any) {
			t.convertSlotsV1(temp, v)
		}
		for i := 0; i < models.MaxSlotNum; i++ {
			if temp[i] == nil {
				continue
			}
			cfg2.Slots = append(cfg2.Slots, temp[i])
		}
	}

	if servers := cfg1["servers"]; servers != nil {
		group := make(map[int]*models.Group)
		for _, g := range servers.(map[string]any) {
			for _, v := range g.(map[string]any) {
				t.convertGroupV1(group, v)
			}
		}
		cfg2.Group = models.SortGroup(group)
	}

	b, err := json.MarshalIndent(cfg2, "", "    ")
	if err != nil {
		log.PanicErrorf(err, "json marshal failed")
	}
	fmt.Println(string(b))
}

func (t *cmdAdmin) loadJsonConfigV3(file string) *ConfigV3 {
	b, err := os.ReadFile(file)
	if err != nil {
		log.PanicErrorf(err, "read file '%s' failed", file)
	}

	config := &ConfigV3{}
	if err := json.Unmarshal(b, config); err != nil {
		log.PanicErrorf(err, "json unmarshal failed")
	}

	var proxy = make(map[string]*models.Proxy)
	for _, p := range config.Proxy {
		if proxy[p.Token] != nil {
			log.Panicf("proxy-%s already exists", p.Token)
		}
		proxy[p.Token] = p
	}

	var group = make(map[int]*models.Group)
	var maddr = make(map[string]bool)
	for _, g := range config.Group {
		if g.Id <= 0 || g.Id > models.MaxGroupId {
			log.Panicf("invalid group id = %d", g.Id)
		}
		if group[g.Id] != nil {
			log.Panicf("group-%04d already exists", g.Id)
		}
		if g.Promoting.State != models.ActionNothing {
			log.Panicf("gorup-%04d is promoting", g.Id)
		}
		for _, x := range g.Servers {
			addr := x.Addr
			if maddr[addr] {
				log.Panicf("server %s already exists", addr)
			}
			maddr[addr] = true
		}
		group[g.Id] = g
	}

	var slots = make(map[int]*models.SlotMapping)
	for _, s := range config.Slots {
		if s.Id < 0 || s.Id >= models.MaxSlotNum {
			log.Panicf("invalid slot id = %d", s.Id)
		}
		if slots[s.Id] != nil {
			log.Panicf("slot-%04d already exists", s.Id)
		}
		if s.Action.State != models.ActionNothing {
			log.Panicf("slot-%04d action is not empty", s.Id)
		}
		if g := group[s.GroupId]; g == nil || len(g.Servers) == 0 {
			log.Panicf("slot-%04d with group-%04d doesn't exist or empty", s.Id, s.GroupId)
		}
		slots[s.Id] = s
	}

	return config
}

func (t *cmdAdmin) handleConfigRestore(cfg *adminConfig) {
	store := t.newTopomStore()
	defer store.Close()

	config := t.loadJsonConfigV3(argumentMust(cfg.ConfigRestore, "--config-restore"))

	if !cfg.Confirm.Value {
		b, err := json.MarshalIndent(config, "", "    ")
		if err != nil {
			log.PanicErrorf(err, "json marshal failed")
		}
		fmt.Println(string(b))
		return
	}

	proxy, err := store.ListProxy()
	if err != nil {
		log.PanicErrorf(err, "list proxy failed")
	}
	group, err := store.ListGroup()
	if err != nil {
		log.PanicErrorf(err, "list group failed")
	}

	if len(group) != 0 || len(proxy) != 0 {
		log.Panicf("product %s is not empty", t.product)
	}

	for _, s := range config.Slots {
		if err := store.UpdateSlotMapping(s); err != nil {
			log.PanicErrorf(err, "restore slot-%04d failed", s.Id)
		}
	}

	for _, g := range config.Group {
		if err := store.UpdateGroup(g); err != nil {
			log.PanicErrorf(err, "restore group-%04d failed", g.Id)
		}
	}

	for _, p := range config.Proxy {
		if err := store.UpdateProxy(p); err != nil {
			log.PanicErrorf(err, "restore proxy-%s failed", p.Token)
		}
	}
}

func (t *cmdAdmin) handleDashboardList(cfg *adminConfig) {
	client := t.newTopomClient()
	defer client.Close()

	list, err := client.List(models.CodisDir, false)
	if err != nil {
		log.PanicErrorf(err, "list products failed")
	}

	nodes := []any{}

	for _, path := range list {
		var elem = &struct {
			Name      string `json:"name"`
			Dashboard string `json:"dashboard"`
		}{filepath.Base(path), ""}

		if b, err := client.Read(models.LockPath(elem.Name), false); err != nil {
			log.PanicErrorf(err, "read topom of product %s failed", elem.Name)
		} else if b != nil {
			var t = &models.Topom{}
			if err := json.Unmarshal(b, t); err != nil {
				log.PanicErrorf(err, "decode json failed")
			}
			elem.Dashboard = t.AdminAddr
		}

		nodes = append(nodes, elem)
	}

	if b, err := json.MarshalIndent(nodes, "", "    "); err != nil {
		log.PanicErrorf(err, "json encode failed")
	} else {
		fmt.Println(string(b))
	}
}
