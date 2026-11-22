// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

type cmdProxy struct {
	addr string
	auth string
}

func (t *cmdProxy) Main(cfg *adminConfig) {
	t.addr = argumentMust(cfg.Proxy, "--proxy")
	t.auth = cfg.Auth.Value

	switch cfg.Command {
	default:
		t.handleOverview(cfg)
	case "start":
		t.handleStart(cfg)
	case "shutdown":
		t.handleShutdown(cfg)
	case "log-level":
		t.handleLogLevel(cfg)
	case "fillslots":
		t.handleFillSlots(cfg)
	case "reset-stats":
		t.handleResetStats(cfg)
	case "forcegc":
		t.handleForceGC(cfg)
	}
}

func (t *cmdProxy) newProxyClient(xauth bool) *proxy.ApiClient {
	c := proxy.NewApiClient(t.addr)

	if !xauth {
		return c
	}

	log.Debugf("call rpc model to proxy %s", t.addr)
	p, err := c.Model()
	if err != nil {
		log.PanicErrorf(err, "call rpc model to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc model OK")

	c.SetXAuth(p.ProductName, t.auth, p.Token)

	log.Debugf("call rpc xping to proxy %s", t.addr)
	if err := c.XPing(); err != nil {
		log.PanicErrorf(err, "call rpc xping failed")
	}
	log.Debugf("call rpc xping OK")

	return c
}

func (t *cmdProxy) handleOverview(cfg *adminConfig) {
	c := t.newProxyClient(false)

	log.Debugf("call rpc overview to proxy %s", t.addr)
	o, err := c.Overview()
	if err != nil {
		log.PanicErrorf(err, "call rpc overview to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc overview OK")

	var cmd = cfg.Command
	var obj any
	switch cmd {
	default:
		obj = o
	case "config":
		obj = o.Config
	case "model":
		obj = o.Model
	case "slots":
		obj = o.Slots
	case "stats":
		obj = o.Stats
	}

	b, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		log.PanicErrorf(err, "json marshal failed")
	}
	fmt.Println(string(b))
}

func (t *cmdProxy) handleStart(cfg *adminConfig) {
	c := t.newProxyClient(true)

	log.Debugf("call rpc start to proxy %s", t.addr)
	if err := c.Start(); err != nil {
		log.PanicErrorf(err, "call rpc start to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc start to proxy OK")
}

func (t *cmdProxy) handleLogLevel(cfg *adminConfig) {
	c := t.newProxyClient(true)

	s := argumentMust(cfg.LogLevel, "--log-level")

	var v log.LogLevel
	if !v.ParseFromString(s) {
		log.Panicf("option --log-level = %s", s)
	}

	log.Debugf("call rpc loglevel to proxy %s", t.addr)
	if err := c.LogLevel(v); err != nil {
		log.PanicErrorf(err, "call rpc loglevel to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc loglevel OK")
}

func (t *cmdProxy) handleFillSlots(cfg *adminConfig) {
	c := t.newProxyClient(true)

	b, err := os.ReadFile(argumentMust(cfg.FillSlots, "--fillslots"))
	if err != nil {
		log.PanicErrorf(err, "load slots from file failed")
	}

	var slots []*models.Slot
	if err := json.Unmarshal(b, &slots); err != nil {
		log.PanicErrorf(err, "decode slots from json failed")
	}

	for _, m := range slots {
		if m.Id < 0 || m.Id >= models.MaxSlotNum {
			log.Panicf("invalid slot id = %d", m.Id)
		}
	}

	if cfg.Locked.Value {
		for _, m := range slots {
			m.Locked = true
		}
	}

	log.Debugf("call rpc fillslots to proxy %s", t.addr)
	if err := c.FillSlots(slots...); err != nil {
		log.PanicErrorf(err, "call rpc fillslots to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc fillslots OK")
}

func (t *cmdProxy) handleResetStats(cfg *adminConfig) {
	c := t.newProxyClient(true)

	log.Debugf("call rpc resetstats to proxy %s", t.addr)
	if err := c.ResetStats(); err != nil {
		log.PanicErrorf(err, "call rpc resetstats to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc resetstats OK")
}

func (t *cmdProxy) handleForceGC(cfg *adminConfig) {
	c := t.newProxyClient(true)

	log.Debugf("call rpc forcegc to proxy %s", t.addr)
	if err := c.ForceGC(); err != nil {
		log.PanicErrorf(err, "call rpc forcegc to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc forcegc OK")
}

func (t *cmdProxy) handleShutdown(cfg *adminConfig) {
	c := t.newProxyClient(true)

	log.Debugf("call rpc shutdown to proxy %s", t.addr)
	if err := c.Shutdown(); err != nil {
		log.PanicErrorf(err, "call rpc shutdown to proxy %s failed", t.addr)
	}
	log.Debugf("call rpc shutdown OK")
}
