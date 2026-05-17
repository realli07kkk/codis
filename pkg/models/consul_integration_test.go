// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
)

func TestConsulStoreAndJodisIntegration(t *testing.T) {
	addr := os.Getenv("CODIS_CONSUL_ADDR")
	if addr == "" {
		t.Skip("set CODIS_CONSUL_ADDR to run Consul store/Jodis integration test")
	}

	product := fmt.Sprintf("consul_acceptance_%d", time.Now().UnixNano())

	client, err := models.NewClient("consul", addr, "", time.Second*2)
	if err != nil {
		t.Fatalf("new consul client failed: %v", err)
	}
	store := models.NewStore(client, product)

	topom := &models.Topom{
		Token:       "topom-token",
		StartTime:   time.Now().String(),
		AdminAddr:   "127.0.0.1:0",
		ProductName: product,
		Pid:         os.Getpid(),
		Pwd:         ".",
		Sys:         "test",
	}
	if err := store.Acquire(topom); err != nil {
		t.Fatalf("store acquire failed: %v", err)
	}
	loaded, err := store.LoadTopom(true)
	if err != nil {
		t.Fatalf("load topom failed: %v", err)
	}
	if loaded.ProductName != product || loaded.AdminAddr != topom.AdminAddr {
		t.Fatalf("unexpected topom: %+v", loaded)
	}
	if err := store.Release(); err != nil {
		t.Fatalf("store release failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close store client failed: %v", err)
	}

	config := proxy.NewDefaultConfig()
	config.ProductName = product
	config.ProxyAddr = "127.0.0.1:0"
	config.AdminAddr = "127.0.0.1:0"
	config.JodisName = "consul"
	config.JodisAddr = addr

	p, err := proxy.New(config)
	if err != nil {
		t.Fatalf("new proxy failed: %v", err)
	}
	defer p.Close()
	if err := p.Start(); err != nil {
		t.Fatalf("start proxy failed: %v", err)
	}

	jodisClient, err := models.NewClient("consul", addr, "", time.Second*2)
	if err != nil {
		t.Fatalf("new jodis client failed: %v", err)
	}
	defer jodisClient.Close()

	path := waitForJodisPath(t, jodisClient, product)
	data, err := jodisClient.Read(path, true)
	if err != nil {
		t.Fatalf("read jodis registration failed: %v", err)
	}
	if !strings.Contains(string(data), `"state": "online"`) {
		t.Fatalf("unexpected jodis registration: %s", data)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("close proxy failed: %v", err)
	}
	waitForMissingPath(t, jodisClient, path)
}

func waitForJodisPath(t *testing.T, client models.Client, product string) string {
	t.Helper()

	prefix := models.JodisDir + "/" + product
	deadline := time.Now().Add(time.Second * 3)
	for time.Now().Before(deadline) {
		paths, err := client.List(prefix, false)
		if err == nil && len(paths) == 1 {
			return paths[0]
		}
		time.Sleep(time.Millisecond * 50)
	}
	t.Fatalf("jodis registration not found under %s", prefix)
	return ""
}

func waitForMissingPath(t *testing.T, client models.Client, path string) {
	t.Helper()

	deadline := time.Now().Add(time.Second * 3)
	for time.Now().Before(deadline) {
		data, err := client.Read(path, false)
		if err == nil && data == nil {
			return
		}
		time.Sleep(time.Millisecond * 50)
	}
	t.Fatalf("jodis registration still exists: %s", path)
}
