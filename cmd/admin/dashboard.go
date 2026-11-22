// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

type cmdDashboard struct {
	addr string
}

func (t *cmdDashboard) Main(cfg *adminConfig) {
	t.addr = argumentMust(cfg.Dashboard, "--dashboard")

	switch cfg.Command {
	case "shutdown":
		t.handleShutdown(cfg)
	case "reload":
		t.handleReload(cfg)
	case "log-level":
		t.handleLogLevel(cfg)
	case "slots-assign", "slots-status":
		t.handleSlotsCommand(cfg)
	case "create-proxy", "online-proxy", "remove-proxy", "reinit-proxy", "proxy-status":
		t.handleProxyCommand(cfg)
	case "create-group", "remove-group", "resync-group", "group-add", "group-del",
		"group-status", "replica-groups", "promote-server":
		t.handleGroupCommand(cfg)
	case "sentinel-add", "sentinel-del", "sentinel-resync":
		t.handleSentinelCommand(cfg)
	case "sync-action":
		t.handleSyncActionCommand(cfg)
	case "slot-action":
		t.handleSlotActionCommand(cfg)
	case "rebalance":
		t.handleSlotRebalance(cfg)
	case "acl", "acl-sync", "acl-set":
		t.handleACLCommand(cfg)
	case "rdb-analysis-remote-fetch":
		t.handleRDBAnalysisRemoteFetch(cfg)
	case "pitr-create", "pitr-list", "pitr-get", "pitr-cancel", "pitr-remove":
		t.handlePitrCommand(cfg)
	default:
		t.handleOverview(cfg)
	}
}

func (t *cmdDashboard) newTopomClient() *topom.ApiClient {
	c := topom.NewApiClient(t.addr)

	log.Debugf("call rpc model to dashboard %s", t.addr)
	p, err := c.Model()
	if err != nil {
		log.PanicErrorf(err, "call rpc model to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc model OK")

	c.SetXAuth(p.ProductName)

	log.Debugf("call rpc xping to dashboard %s", t.addr)
	if err := c.XPing(); err != nil {
		log.PanicErrorf(err, "call rpc xping to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc xping OK")

	return c
}

func (t *cmdDashboard) handleOverview(cfg *adminConfig) {
	c := t.newTopomClient()

	log.Debugf("call rpc overview to dashboard %s", t.addr)
	o, err := c.Overview()
	if err != nil {
		log.PanicErrorf(err, "call rpc overview to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc overview OK")

	var cmd = cfg.Command
	switch cmd {
	case "", "config", "model", "slots", "stats", "group", "proxy", "list-group", "list-proxy":
	default:
		cmd = ""
	}

	var obj any
	switch cmd {
	default:
		obj = o
	case "config":
		obj = o.Config
	case "model":
		obj = o.Model
	case "stats":
		obj = o.Stats
	case "slots":
		if o.Stats != nil {
			obj = o.Stats.Slots
		}
	case "group":
		if o.Stats != nil {
			obj = o.Stats.Group
		}
	case "list-group":
		if o.Stats != nil {
			obj = o.Stats.Group.Models
		}
	case "proxy":
		if o.Stats != nil {
			obj = o.Stats.Proxy
		}
	case "list-proxy":
		if o.Stats != nil {
			obj = o.Stats.Proxy.Models
		}
	}

	b, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		log.PanicErrorf(err, "json marshal failed")
	}
	fmt.Println(string(b))
}

func (t *cmdDashboard) handleLogLevel(cfg *adminConfig) {
	c := t.newTopomClient()

	s := argumentMust(cfg.LogLevel, "--log-level")

	var v log.LogLevel
	if !v.ParseFromString(s) {
		log.Panicf("option --log-level = %s", s)
	}

	log.Debugf("call rpc loglevel to dashboard %s", t.addr)
	if err := c.LogLevel(v); err != nil {
		log.PanicErrorf(err, "call rpc loglevel to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc loglevel OK")
}

func (t *cmdDashboard) handleShutdown(cfg *adminConfig) {
	c := t.newTopomClient()

	log.Debugf("call rpc shutdown to dashboard %s", t.addr)
	if err := c.Shutdown(); err != nil {
		log.PanicErrorf(err, "call rpc shutdown to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc shutdown OK")
}

func (t *cmdDashboard) handleReload(cfg *adminConfig) {
	c := t.newTopomClient()

	log.Debugf("call rpc reload to dashboard %s", t.addr)
	if err := c.Reload(); err != nil {
		log.PanicErrorf(err, "call rpc reload to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc reload OK")
}

func (t *cmdDashboard) handleSlotsCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch {

	case cfg.SlotsStatus.Value:

		log.Debugf("call rpc slots to dashboard %s", t.addr)
		o, err := c.Slots()
		if err != nil {
			log.PanicErrorf(err, "call rpc slots to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc slots OK")

		b, err := json.MarshalIndent(o, "", "    ")
		if err != nil {
			log.PanicErrorf(err, "json marshal failed")
		}
		fmt.Println(string(b))

	case cfg.SlotsAssign.Value && cfg.Offline.Value:

		beg := argumentIntegerMust(cfg.Beg, "--beg")
		end := argumentIntegerMust(cfg.End, "--end")

		slots := []*models.SlotMapping{}
		for i := beg; i <= end; i++ {
			slots = append(slots, &models.SlotMapping{
				Id: i,
			})
		}

		if !cfg.Confirm.Value {
			b, err := json.MarshalIndent(slots, "", "    ")
			if err != nil {
				log.PanicErrorf(err, "json marshal failed")
			}
			fmt.Println(string(b))
			return
		}

		log.Debugf("call rpc slots-assign to dashboard %s", t.addr)
		if err := c.SlotsAssignOffline(slots); err != nil {
			log.PanicErrorf(err, "call rpc slots-assign to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc slots-assign OK")

	case cfg.SlotsAssign.Value && !cfg.Offline.Value:

		beg := argumentIntegerMust(cfg.Beg, "--beg")
		end := argumentIntegerMust(cfg.End, "--end")
		gid := argumentIntegerMust(cfg.Gid, "--gid")

		slots := []*models.SlotMapping{}
		for i := beg; i <= end; i++ {
			slots = append(slots, &models.SlotMapping{
				Id: i, GroupId: gid,
			})
		}

		if !cfg.Confirm.Value {
			b, err := json.MarshalIndent(slots, "", "    ")
			if err != nil {
				log.PanicErrorf(err, "json marshal failed")
			}
			fmt.Println(string(b))
			return
		}

		log.Debugf("call rpc slots-assign to dashboard %s", t.addr)
		if err := c.SlotsAssignGroup(slots); err != nil {
			log.PanicErrorf(err, "call rpc slots-assign to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc slots-assign OK")

	}
}

func (t *cmdDashboard) handleACLCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch cfg.Command {
	case "acl":
		log.Debugf("call rpc acl get to dashboard %s", t.addr)
		view, err := c.GetACL()
		if err != nil {
			log.PanicErrorf(err, "call rpc acl get to dashboard %s failed", t.addr)
		}
		b, err := json.MarshalIndent(view, "", "    ")
		if err != nil {
			log.PanicErrorf(err, "json marshal failed")
		}
		fmt.Println(string(b))

	case "acl-sync":
		log.Debugf("call rpc acl sync to dashboard %s", t.addr)
		view, err := c.SyncACL()
		if err != nil {
			log.PanicErrorf(err, "call rpc acl sync to dashboard %s failed", t.addr)
		}
		pretty, err := json.MarshalIndent(view, "", "    ")
		if err != nil {
			log.PanicErrorf(err, "json marshal failed")
		}
		fmt.Println(string(pretty))

	case "acl-set":
		path := argumentMust(cfg.ACLSet, "--acl-set")
		b, err := os.ReadFile(path)
		if err != nil {
			log.PanicErrorf(err, "read acl file failed")
		}
		req := &topom.ACLUpdateRequest{}
		if err := json.Unmarshal(b, req); err != nil {
			log.PanicErrorf(err, "decode acl file failed")
		}
		if !cfg.Confirm.Value {
			pretty, err := json.MarshalIndent(redactACLUpdateRequest(req), "", "    ")
			if err != nil {
				log.PanicErrorf(err, "json marshal failed")
			}
			fmt.Println(string(pretty))
			return
		}
		log.Debugf("call rpc acl set to dashboard %s", t.addr)
		view, err := c.SetACL(req)
		if err != nil {
			log.PanicErrorf(err, "call rpc acl set to dashboard %s failed", t.addr)
		}
		pretty, err := json.MarshalIndent(view, "", "    ")
		if err != nil {
			log.PanicErrorf(err, "json marshal failed")
		}
		fmt.Println(string(pretty))
	}
}

func redactACLUpdateRequest(req *topom.ACLUpdateRequest) *topom.ACLUpdateRequest {
	if req == nil {
		return nil
	}
	redacted := &topom.ACLUpdateRequest{
		Enabled: req.Enabled,
		Users:   make([]*topom.ACLUserUpdate, 0, len(req.Users)),
	}
	for _, user := range req.Users {
		if user == nil {
			redacted.Users = append(redacted.Users, nil)
			continue
		}
		dup := &topom.ACLUserUpdate{
			Name:           user.Name,
			Enabled:        user.Enabled,
			PasswordHashes: append([]string(nil), user.PasswordHashes...),
			Rules:          append([]string(nil), user.Rules...),
		}
		if user.DB != nil {
			db := *user.DB
			dup.DB = &db
		}
		if user.NewPassword != "" {
			dup.NewPassword = "<redacted>"
		}
		redacted.Users = append(redacted.Users, dup)
	}
	return redacted
}

func (t *cmdDashboard) parseProxyTokens(cfg *adminConfig) []string {
	switch {

	default:

		log.Panicf("can't find specific proxy")

		return nil

	case cfg.Token.Provided:

		return []string{argumentMust(cfg.Token, "--token")}

	case cfg.Pid.Provided:

		pid := argumentIntegerMust(cfg.Pid, "--pid")

		c := t.newTopomClient()

		log.Debugf("call rpc stats to dashboard %s", t.addr)
		s, err := c.Stats()
		if err != nil {
			log.PanicErrorf(err, "call rpc stats to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc stats OK")

		var tokens []string

		for _, p := range s.Proxy.Models {
			if p.Id == pid {
				tokens = append(tokens, p.Token)
			}
		}

		if len(tokens) != 0 {
			return tokens
		}

		if !cfg.Force.Value {
			log.Panicf("can't find specific proxy with id = %d", pid)
		}
		return nil

	case cfg.Addr.Provided:

		addr := argumentMust(cfg.Addr, "--addr")

		c := t.newTopomClient()

		log.Debugf("call rpc stats to dashboard %s", t.addr)
		s, err := c.Stats()
		if err != nil {
			log.PanicErrorf(err, "call rpc stats to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc stats OK")

		var tokens []string

		for _, p := range s.Proxy.Models {
			if p.AdminAddr == addr {
				tokens = append(tokens, p.Token)
			}
		}

		if len(tokens) != 0 {
			return tokens
		}

		if !cfg.Force.Value {
			log.Panicf("can't find specific proxy with addr = %s", addr)
		}
		return nil

	}
}

func (t *cmdDashboard) handleProxyCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch cfg.Command {

	case "create-proxy":

		addr := argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc create-proxy to dashboard %s", t.addr)
		if err := c.CreateProxy(addr); err != nil {
			log.PanicErrorf(err, "call rpc create-proxy to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc create-proxy OK")

	case "online-proxy":

		addr := argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc online-proxy to dashboard %s", t.addr)
		if err := c.OnlineProxy(addr); err != nil {
			log.PanicErrorf(err, "call rpc online-proxy to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc online-proxy OK")

	case "remove-proxy":

		force := cfg.Force.Value

		for _, token := range t.parseProxyTokens(cfg) {
			log.Debugf("call rpc remove-proxy to dashboard %s", t.addr)
			if err := c.RemoveProxy(token, force); err != nil {
				log.PanicErrorf(err, "call rpc remove-proxy to dashboard %s failed", t.addr)
			}
			log.Debugf("call rpc remove-proxy OK")
		}

	case "reinit-proxy":

		switch {

		default:

			for _, token := range t.parseProxyTokens(cfg) {
				log.Debugf("call rpc reinit-proxy to dashboard %s", t.addr)
				if err := c.ReinitProxy(token); err != nil {
					log.PanicErrorf(err, "call rpc reinit-proxy to dashboard %s failed", t.addr)
				}
				log.Debugf("call rpc reinit-proxy OK")
			}

		case cfg.All.Value:

			log.Debugf("call rpc stats to dashboard %s", t.addr)
			s, err := c.Stats()
			if err != nil {
				log.PanicErrorf(err, "call rpc stats to dashboard %s failed", t.addr)
			}
			log.Debugf("call rpc stats OK")

			for _, p := range s.Proxy.Models {
				fmt.Printf("reinit proxy: %s\n", p.Encode())
				log.Debugf("call rpc reinit-proxy to dashboard %s", t.addr)
				if err := c.ReinitProxy(p.Token); err != nil {
					log.PanicErrorf(err, "call rpc reinit-proxy to dashboard %s failed", t.addr)
				}
				log.Debugf("call rpc reinit-proxy OK")
			}

		}

	case "proxy-status":

		log.Debugf("call rpc stats to dashboard %s", t.addr)
		s, err := c.Stats()
		if err != nil {
			log.PanicErrorf(err, "call rpc stats to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc stats OK")

		var format string
		var wpid int
		for _, p := range s.Proxy.Models {
			wpid = max(wpid, len(strconv.Itoa(p.Id)))
		}
		format += fmt.Sprintf("proxy-%%0%dd [T] %%s", wpid)

		var waddr1, waddr2 int
		for _, p := range s.Proxy.Models {
			waddr1 = max(waddr1, len(p.AdminAddr))
			waddr2 = max(waddr2, len(p.ProxyAddr))
		}
		format += fmt.Sprintf(" [A] %%-%ds", waddr1)
		format += fmt.Sprintf(" [P] %%-%ds", waddr2)

		for _, p := range s.Proxy.Models {
			var xfmt string
			switch stats := s.Proxy.Stats[p.Token]; {
			case stats == nil:
				xfmt = "[?] " + format
			case stats.Error != nil:
				xfmt = "[E] " + format
			case stats.Timeout || stats.Stats == nil:
				xfmt = "[T] " + format
			default:
				xfmt = "[ ] " + format
			}
			fmt.Printf(xfmt, p.Id, p.Token, p.AdminAddr, p.ProxyAddr)
			fmt.Println()
		}
	}
}

func (t *cmdDashboard) handleGroupCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch cfg.Command {

	case "create-group":

		gid := argumentIntegerMust(cfg.Gid, "--gid")

		log.Debugf("call rpc create-group to dashboard %s", t.addr)
		if err := c.CreateGroup(gid); err != nil {
			log.PanicErrorf(err, "call rpc create-group to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc create-group OK")

	case "remove-group":

		gid := argumentIntegerMust(cfg.Gid, "--gid")

		log.Debugf("call rpc remove-group to dashboard %s", t.addr)
		if err := c.RemoveGroup(gid); err != nil {
			log.PanicErrorf(err, "call rpc remove-group to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc remove-group OK")

	case "resync-group":

		switch {

		case cfg.All.Value:

			stats, err := c.Stats()
			if err != nil {
				log.PanicErrorf(err, "call rpc stats to dashboard %s failed", t.addr)
			}
			log.Debugf("call rpc stats OK")

			for _, g := range stats.Group.Models {
				log.Debugf("call rpc resync-group [%d] to dashboard %s", g.Id, t.addr)
				if err := c.ResyncGroup(g.Id); err != nil {
					log.PanicErrorf(err, "call rpc resync-group to dashboard %s failed", t.addr)
				}
			}
			log.Debugf("call rpc resync-group OK")

		default:

			gid := argumentIntegerMust(cfg.Gid, "--gid")

			log.Debugf("call rpc resync-group to dashboard %s", t.addr)
			if err := c.ResyncGroup(gid); err != nil {
				log.PanicErrorf(err, "call rpc resync-group to dashboard %s failed", t.addr)
			}
			log.Debugf("call rpc resync-group OK")

		}

	case "group-add":

		gid, addr := argumentIntegerMust(cfg.Gid, "--gid"), argumentMust(cfg.Addr, "--addr")
		var dc string
		if cfg.Datacenter.Provided {
			dc = cfg.Datacenter.Value
		}

		log.Debugf("call rpc group-add-server to dashboard %s", t.addr)
		if err := c.GroupAddServer(gid, dc, addr); err != nil {
			log.PanicErrorf(err, "call rpc group-add-server to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc group-add-server OK")

	case "group-del":

		gid, addr := argumentIntegerMust(cfg.Gid, "--gid"), argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc group-del-server to dashboard %s", t.addr)
		if err := c.GroupDelServer(gid, addr); err != nil {
			log.PanicErrorf(err, "call rpc group-del-server to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc group-del-server OK")

	case "replica-groups":

		gid, addr := argumentIntegerMust(cfg.Gid, "--gid"), argumentMust(cfg.Addr, "--addr")
		value := cfg.Enable.Value

		log.Debugf("call rpc replica-groups to dashboard %s", t.addr)
		if err := c.EnableReplicaGroups(gid, addr, value); err != nil {
			log.PanicErrorf(err, "call rpc replica-groups to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc replica-groups to dashboard OK")

	case "promote-server":

		gid, addr := argumentIntegerMust(cfg.Gid, "--gid"), argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc group-promote-server to dashboard %s", t.addr)
		if err := c.GroupPromoteServer(gid, addr); err != nil {
			log.PanicErrorf(err, "call rpc group-promote-server to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc group-promote-server OK")

		fallthrough

	case "group-status":

		log.Debugf("call rpc stats to dashboard %s", t.addr)
		s, err := c.Stats()
		if err != nil {
			log.PanicErrorf(err, "call rpc stats to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc stats OK")

		var format string
		var wgid, widx int
		for _, g := range s.Group.Models {
			wgid = max(wgid, len(strconv.Itoa(g.Id)))
			for i, _ := range g.Servers {
				widx = max(widx, len(strconv.Itoa(i)))
			}
		}
		format += fmt.Sprintf("group-%%0%dd [%%0%dd]", wgid, widx)

		var waddr int
		for _, g := range s.Group.Models {
			for _, x := range g.Servers {
				waddr = max(waddr, len(x.Addr))
			}
		}
		format += fmt.Sprintf(" %%-%ds", waddr)

		for _, g := range s.Group.Models {
			for i, x := range g.Servers {
				var addr = x.Addr
				switch stats := s.Group.Stats[addr]; {
				case stats == nil:
					fmt.Printf("[?] "+format, g.Id, i, addr)
				case stats.Error != nil:
					fmt.Printf("[E] "+format, g.Id, i, addr)
				case stats.Timeout || stats.Stats == nil:
					fmt.Printf("[T] "+format, g.Id, i, addr)
				default:
					var master string
					if s, ok := stats.Stats["master_addr"]; ok {
						master = s + ":" + stats.Stats["master_link_status"]
					} else {
						master = "NO:ONE"
					}
					var expect string
					if i == 0 {
						expect = "NO:ONE"
					} else {
						expect = g.Servers[0].Addr + ":up"
					}
					if master == expect {
						fmt.Printf("[ ] "+format, g.Id, i, addr)
					} else {
						fmt.Printf("[X] "+format, g.Id, i, addr)
					}
					fmt.Printf("      ==> %s", master)
				}
				fmt.Println()
			}
		}
	}
}

func (t *cmdDashboard) handleSentinelCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch cfg.Command {

	case "sentinel-add":

		addr := argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc add-sentinel to dashboard %s", t.addr)
		if err := c.AddSentinel(addr); err != nil {
			log.PanicErrorf(err, "call rpc add-sentinel to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc add-sentinel OK")

	case "sentinel-del":

		addr := argumentMust(cfg.Addr, "--addr")

		force := cfg.Force.Value

		log.Debugf("call rpc del-sentinel to dashboard %s", t.addr)
		if err := c.DelSentinel(addr, force); err != nil {
			log.PanicErrorf(err, "call rpc del-sentinel to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc del-sentinel OK")

	case "sentinel-resync":

		log.Debugf("call rpc resync-sentinels to dashboard %s", t.addr)
		if err := c.ResyncSentinels(); err != nil {
			log.PanicErrorf(err, "call rpc resync-sentinels to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc resync-sentinels OK")

	}
}

func (t *cmdDashboard) handleSyncActionCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch {

	case cfg.Create.Value:

		addr := argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc create-sync-action to dashboard %s", t.addr)
		if err := c.SyncCreateAction(addr); err != nil {
			log.PanicErrorf(err, "call rpc create-sync-action to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc create-sync-action OK")

	case cfg.Remove.Value:

		addr := argumentMust(cfg.Addr, "--addr")

		log.Debugf("call rpc remove-sync-action to dashboard %s", t.addr)
		if err := c.SyncRemoveAction(addr); err != nil {
			log.PanicErrorf(err, "call rpc remove-sync-action to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc remove-sync-action OK")

	}

}

func (t *cmdDashboard) handleSlotActionCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch {

	case cfg.Create.Value:

		sid := argumentIntegerMust(cfg.Sid, "--sid")
		gid := argumentIntegerMust(cfg.Gid, "--gid")

		log.Debugf("call rpc create-slot-action to dashboard %s", t.addr)
		if err := c.SlotCreateAction(sid, gid); err != nil {
			log.PanicErrorf(err, "call rpc create-slot-action to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc create-slot-action OK")

	case cfg.Remove.Value:

		sid := argumentIntegerMust(cfg.Sid, "--sid")

		log.Debugf("call rpc remove-slot-action to dashboard %s", t.addr)
		if err := c.SlotRemoveAction(sid); err != nil {
			log.PanicErrorf(err, "call rpc remove-slot-action to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc remove-slot-action OK")

	case cfg.CreateSome.Value:

		src := argumentIntegerMust(cfg.GidFrom, "--gid-from")
		dst := argumentIntegerMust(cfg.GidTo, "--gid-to")
		num := argumentIntegerMust(cfg.NumSlots, "--num-slots")

		log.Debugf("call rpc create-slot-action-some to dashboard %s", t.addr)
		if err := c.SlotCreateActionSome(src, dst, num); err != nil {
			log.PanicErrorf(err, "call rpc create-slot-action-some to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc create-slot-action-some OK")

	case cfg.CreateRange.Value:

		beg := argumentIntegerMust(cfg.Beg, "--beg")
		end := argumentIntegerMust(cfg.End, "--end")
		gid := argumentIntegerMust(cfg.Gid, "--gid")

		log.Debugf("call rpc create-slot-action-range to dashboard %s", t.addr)
		if err := c.SlotCreateActionRange(beg, end, gid); err != nil {
			log.PanicErrorf(err, "call rpc create-slot-action-range to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc create-slot-action-range OK")

	case cfg.Interval.Provided:

		value := argumentIntegerMust(cfg.Interval, "--interval")

		log.Debugf("call rpc slot-action-interval to dashboard %s", t.addr)
		if err := c.SetSlotActionInterval(value); err != nil {
			log.PanicErrorf(err, "call rpc slot-action-interval to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc slot-action-interval OK")

	case cfg.Disabled.Provided:

		value := argumentIntegerMust(cfg.Disabled, "--disabled")

		log.Debugf("call rpc slot-action-disabled to dashboard %s", t.addr)
		if err := c.SetSlotActionDisabled(value != 0); err != nil {
			log.PanicErrorf(err, "call rpc slot-action-disabled to dashboard %s failed", t.addr)
		}
		log.Debugf("call rpc slot-action-disabled OK")

	}
}

func (t *cmdDashboard) handleSlotRebalance(cfg *adminConfig) {
	c := t.newTopomClient()

	confirm := cfg.Confirm.Value

	log.Debugf("call rpc slot-rebalance to dashboard %s", t.addr)
	plans, err := c.SlotsRebalance(confirm)
	if err != nil {
		log.PanicErrorf(err, "call rpc slot-rebalance to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc slot-rebalance OK")

	if len(plans) == 0 {
		fmt.Println("nothing changes")
	} else {
		var slotIds = make([]int, 0, len(plans))
		for sid := range plans {
			slotIds = append(slotIds, sid)
		}
		slices.Sort(slotIds)

		var gid, beg, end = -1, 0, -1
		for _, sid := range slotIds {
			if beg <= end {
				if sid == end+1 && plans[sid] == gid {
					end = sid
					continue
				}
				fmt.Printf("[%04d,%04d] => %d\n", beg, end, gid)
			}
			beg, end, gid = sid, sid, plans[sid]
		}
		if beg <= end {
			fmt.Printf("[%04d,%04d] => %d\n", beg, end, gid)
		}
		fmt.Println("done")
	}
}
