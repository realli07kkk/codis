// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// handlePitrCommand dispatches the --pitr-* flags to the dashboard ApiClient
// methods. codis-admin never connects to the dashboard's Redis targets
// directly; every call goes through ApiClient (CreatePitr/ListPitr/...).
func (t *cmdDashboard) handlePitrCommand(cfg *adminConfig) {
	c := t.newTopomClient()

	switch cfg.Command {
	case "pitr-create":
		t.handlePitrCreate(cfg, c)
	case "pitr-list":
		t.handlePitrList(cfg, c)
	case "pitr-get":
		t.handlePitrGet(cfg, c)
	case "pitr-cancel":
		t.handlePitrCancel(cfg, c)
	case "pitr-remove":
		t.handlePitrRemove(cfg, c)
	}
}

func (t *cmdDashboard) handlePitrCreate(cfg *adminConfig, c *topom.ApiClient) {
	serverAddr := argumentMust(cfg.Server, "--server")
	tsText := argumentMust(cfg.TruncateTS, "--truncate-ts")
	ts, err := strconv.ParseInt(tsText, 10, 64)
	if err != nil {
		log.PanicErrorf(err, "invalid --truncate-ts %q", tsText)
	}

	log.Debugf("call rpc pitr-create to dashboard %s", t.addr)
	id, err := c.CreatePitr(serverAddr, ts)
	if err != nil {
		log.PanicErrorf(err, "call rpc pitr-create to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc pitr-create OK")
	fmt.Println(id)
}

func (t *cmdDashboard) handlePitrList(cfg *adminConfig, c *topom.ApiClient) {
	log.Debugf("call rpc pitr-list to dashboard %s", t.addr)
	jobs, err := c.ListPitr()
	if err != nil {
		log.PanicErrorf(err, "call rpc pitr-list to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc pitr-list OK")

	b, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		log.PanicErrorf(err, "marshal pitr jobs failed")
	}
	fmt.Println(string(b))
}

func (t *cmdDashboard) handlePitrGet(cfg *adminConfig, c *topom.ApiClient) {
	id := argumentMust(cfg.Job, "--job")

	log.Debugf("call rpc pitr-get to dashboard %s", t.addr)
	job, err := c.GetPitr(id)
	if err != nil {
		log.PanicErrorf(err, "call rpc pitr-get to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc pitr-get OK")

	b, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		log.PanicErrorf(err, "marshal pitr job failed")
	}
	fmt.Println(string(b))
}

func (t *cmdDashboard) handlePitrCancel(cfg *adminConfig, c *topom.ApiClient) {
	id := argumentMust(cfg.Job, "--job")

	log.Debugf("call rpc pitr-cancel to dashboard %s", t.addr)
	if err := c.CancelPitr(id); err != nil {
		log.PanicErrorf(err, "call rpc pitr-cancel to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc pitr-cancel OK")
	fmt.Println("OK")
}

func (t *cmdDashboard) handlePitrRemove(cfg *adminConfig, c *topom.ApiClient) {
	id := argumentMust(cfg.Job, "--job")

	log.Debugf("call rpc pitr-remove to dashboard %s", t.addr)
	if err := c.RemovePitr(id); err != nil {
		log.PanicErrorf(err, "call rpc pitr-remove to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc pitr-remove OK")
	fmt.Println("OK")
}
