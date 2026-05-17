// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package consulclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
)

type fakeConsul struct {
	mu    sync.Mutex
	index uint64
	kv    map[string]*capi.KVPair
	next  int
	wake  chan struct{}
}

func newFakeConsul() *fakeConsul {
	return &fakeConsul{index: 1, kv: make(map[string]*capi.KVPair), wake: make(chan struct{})}
}

func (f *fakeConsul) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/kv/") {
		f.serveKV(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/session/") {
		f.serveSession(w, r)
		return
	}
	http.NotFound(w, r)
}

func (f *fakeConsul) serveKV(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/v1/kv/")
	switch r.Method {
	case "GET":
		f.handleKVGet(w, r, key)
	case "PUT":
		f.handleKVPut(w, r, key)
	case "DELETE":
		f.handleKVDelete(w, r, key)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeConsul) handleKVGet(w http.ResponseWriter, r *http.Request, key string) {
	f.waitIndex(r)

	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("X-Consul-Index", strconv.FormatUint(f.index, 10))
	if _, ok := r.URL.Query()["keys"]; ok {
		keys := f.keys(key, r.URL.Query().Get("separator"))
		_ = json.NewEncoder(w).Encode(keys)
		return
	}

	pair := f.kv[key]
	if pair == nil {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode([]*capi.KVPair{pair})
}

func (f *fakeConsul) handleKVPut(w http.ResponseWriter, r *http.Request, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.URL.Query().Get("cas") == "0" && f.kv[key] != nil {
		_, _ = w.Write([]byte("false"))
		return
	}
	if session := r.URL.Query().Get("acquire"); session != "" {
		if pair := f.kv[key]; pair != nil && pair.Session != "" && pair.Session != session {
			_, _ = w.Write([]byte("false"))
			return
		}
		f.bumpIndex()
		f.kv[key] = &capi.KVPair{Key: key, Value: body, ModifyIndex: f.index, Session: session}
		_, _ = w.Write([]byte("true"))
		return
	}

	f.bumpIndex()
	f.kv[key] = &capi.KVPair{Key: key, Value: body, ModifyIndex: f.index}
	_, _ = w.Write([]byte("true"))
}

func (f *fakeConsul) handleKVDelete(w http.ResponseWriter, r *http.Request, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if cas := r.URL.Query().Get("cas"); cas != "" {
		index, _ := strconv.ParseUint(cas, 10, 64)
		if pair := f.kv[key]; pair != nil && pair.ModifyIndex == index {
			f.bumpIndex()
			delete(f.kv, key)
			_, _ = w.Write([]byte("true"))
			return
		}
		_, _ = w.Write([]byte("false"))
		return
	}
	if f.kv[key] != nil {
		f.bumpIndex()
		delete(f.kv, key)
	}
	_, _ = w.Write([]byte("true"))
}

func (f *fakeConsul) waitIndex(r *http.Request) {
	waitIndex, _ := strconv.ParseUint(r.URL.Query().Get("index"), 10, 64)
	if waitIndex == 0 {
		return
	}
	waitTime := time.Second
	if value := r.URL.Query().Get("wait"); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			waitTime = d
		}
	}
	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	for {
		f.mu.Lock()
		if f.index > waitIndex {
			f.mu.Unlock()
			return
		}
		wake := f.wake
		f.mu.Unlock()

		select {
		case <-wake:
		case <-timer.C:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (f *fakeConsul) bumpIndex() {
	f.index++
	close(f.wake)
	f.wake = make(chan struct{})
}

func (f *fakeConsul) keys(prefix string, separator string) []string {
	var keys []string
	seen := make(map[string]bool)
	for key := range f.kv {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		value := key
		if separator != "" {
			rest := strings.TrimPrefix(key, prefix)
			if i := strings.Index(rest, separator); i >= 0 {
				value = prefix + rest[:i+1]
			}
		}
		if !seen[value] {
			keys = append(keys, value)
			seen[value] = true
		}
	}
	return keys
}

func (f *fakeConsul) serveSession(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "PUT" && r.URL.Path == "/v1/session/create":
		f.handleSessionCreate(w)
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v1/session/renew/"):
		f.handleSessionRenew(w, strings.TrimPrefix(r.URL.Path, "/v1/session/renew/"))
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/v1/session/destroy/"):
		f.handleSessionDestroy(w, strings.TrimPrefix(r.URL.Path, "/v1/session/destroy/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeConsul) handleSessionCreate(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.next++
	_ = json.NewEncoder(w).Encode(map[string]string{"ID": fmt.Sprintf("session-%d", f.next)})
}

func (f *fakeConsul) handleSessionRenew(w http.ResponseWriter, id string) {
	_ = json.NewEncoder(w).Encode([]*capi.SessionEntry{{ID: id, TTL: "10s"}})
}

func (f *fakeConsul) handleSessionDestroy(w http.ResponseWriter, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for key, pair := range f.kv {
		if pair.Session == id {
			f.bumpIndex()
			delete(f.kv, key)
		}
	}
	_, _ = w.Write([]byte("true"))
}

func TestCleanKey(t *testing.T) {
	key, err := cleanKey("/codis3/demo/topom")
	if err != nil {
		t.Fatalf("cleanKey failed: %v", err)
	}
	if key != "codis3/demo/topom" {
		t.Fatalf("unexpected key: %q", key)
	}
	if _, err := cleanKey("/"); err == nil {
		t.Fatalf("cleanKey should reject root path")
	}
}

func TestKVOperations(t *testing.T) {
	server := httptest.NewServer(newFakeConsul())
	defer server.Close()

	client, err := New(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}
	defer client.Close()

	if err := client.Create("/codis3/p/topom", []byte("topom-1")); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if err := client.Create("/codis3/p/topom", []byte("topom-2")); err == nil {
		t.Fatalf("second create should fail")
	}

	value, err := client.Read("/codis3/p/topom", true)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(value) != "topom-1" {
		t.Fatalf("create should not be overwritten, got %q", value)
	}

	if err := client.Update("/codis3/p/topom", []byte("topom-3")); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	value, err = client.Read("/codis3/p/topom", true)
	if err != nil {
		t.Fatalf("read after update failed: %v", err)
	}
	if string(value) != "topom-3" {
		t.Fatalf("unexpected updated value: %q", value)
	}

	value, err = client.Read("/codis3/p/missing", false)
	if err != nil {
		t.Fatalf("optional missing read failed: %v", err)
	}
	if value != nil {
		t.Fatalf("optional missing read should return nil")
	}
	if _, err := client.Read("/codis3/p/missing", true); err == nil {
		t.Fatalf("required missing read should fail")
	}

	for path, data := range map[string]string{
		"/codis3/p/group/group-0001": "g1",
		"/codis3/p/group/group-0002": "g2",
		"/codis3/p/group/sub/child":  "child",
	} {
		if err := client.Update(path, []byte(data)); err != nil {
			t.Fatalf("update %s failed: %v", path, err)
		}
	}

	paths, err := client.List("/codis3/p/group", true)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	expected := []string{
		"/codis3/p/group/group-0001",
		"/codis3/p/group/group-0002",
		"/codis3/p/group/sub",
	}
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("unexpected list result:\n got %v\nwant %v", paths, expected)
	}
}

func TestEphemeralLifecycle(t *testing.T) {
	server := httptest.NewServer(newFakeConsul())
	defer server.Close()

	client, err := New(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}
	defer client.Close()

	signal, err := client.CreateEphemeral("/jodis/p/proxy-1", []byte("proxy"))
	if err != nil {
		t.Fatalf("create ephemeral failed: %v", err)
	}
	value, err := client.Read("/jodis/p/proxy-1", true)
	if err != nil {
		t.Fatalf("read ephemeral failed: %v", err)
	}
	if string(value) != "proxy" {
		t.Fatalf("unexpected ephemeral value: %q", value)
	}

	if err := client.Delete("/jodis/p/proxy-1"); err != nil {
		t.Fatalf("delete ephemeral failed: %v", err)
	}
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("ephemeral signal was not closed")
	}
	value, err = client.Read("/jodis/p/proxy-1", false)
	if err != nil {
		t.Fatalf("read deleted ephemeral failed: %v", err)
	}
	if value != nil {
		t.Fatalf("deleted ephemeral should be missing")
	}
}

func TestEphemeralUsesDistinctSessions(t *testing.T) {
	fake := newFakeConsul()
	server := httptest.NewServer(fake)
	defer server.Close()

	client, err := New(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}
	defer client.Close()

	if _, err := client.CreateEphemeral("/jodis/p/proxy-1", []byte("proxy-1")); err != nil {
		t.Fatalf("create first ephemeral failed: %v", err)
	}
	if _, err := client.CreateEphemeral("/jodis/p/proxy-2", []byte("proxy-2")); err != nil {
		t.Fatalf("create second ephemeral failed: %v", err)
	}

	fake.mu.Lock()
	first := fake.kv["jodis/p/proxy-1"].Session
	second := fake.kv["jodis/p/proxy-2"].Session
	fake.mu.Unlock()

	if first == "" || second == "" {
		t.Fatalf("ephemeral sessions should be recorded, got %q and %q", first, second)
	}
	if first == second {
		t.Fatalf("ephemeral sessions should be distinct, got %q", first)
	}
}

func TestEphemeralInOrder(t *testing.T) {
	server := httptest.NewServer(newFakeConsul())
	defer server.Close()

	client, err := New(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}
	defer client.Close()

	signal, node, err := client.CreateEphemeralInOrder("/jodis/p/proxy", []byte("proxy"))
	if err != nil {
		t.Fatalf("create ephemeral in order failed: %v", err)
	}
	defer client.Delete(node)
	if !strings.HasPrefix(node, "/jodis/p/proxy/") {
		t.Fatalf("unexpected inorder node: %s", node)
	}
	select {
	case <-signal:
		t.Fatalf("signal closed before delete")
	default:
	}
}

func TestWatchInOrder(t *testing.T) {
	server := httptest.NewServer(newFakeConsul())
	defer server.Close()

	client, err := New(server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}
	defer client.Close()

	if err := client.Update("/jodis/p/proxy-1", []byte("proxy-1")); err != nil {
		t.Fatalf("initial update failed: %v", err)
	}
	signal, paths, err := client.WatchInOrder("/jodis/p")
	if err != nil {
		t.Fatalf("watch failed: %v", err)
	}
	expected := []string{"/jodis/p/proxy-1"}
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("unexpected initial watch list:\n got %v\nwant %v", paths, expected)
	}

	if err := client.Update("/jodis/p/proxy-2", []byte("proxy-2")); err != nil {
		t.Fatalf("watched update failed: %v", err)
	}
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("watch signal was not closed")
	}
}

func TestIntegrationConsulClient(t *testing.T) {
	addr := os.Getenv("CODIS_CONSUL_ADDR")
	if addr == "" {
		t.Skip("set CODIS_CONSUL_ADDR to run Consul integration test")
	}

	client, err := New(addr, "", time.Second*2)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}
	defer client.Close()

	product := fmt.Sprintf("codis-integration-%d", time.Now().UnixNano())
	prefix := "/codis3/" + product
	topomPath := prefix + "/topom"
	if err := client.Create(topomPath, []byte("topom")); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer client.Delete(topomPath)

	value, err := client.Read(topomPath, true)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(value) != "topom" {
		t.Fatalf("unexpected value: %q", value)
	}

	groupPath := prefix + "/group/group-0001"
	if err := client.Update(groupPath, []byte("group")); err != nil {
		t.Fatalf("update group failed: %v", err)
	}
	defer client.Delete(groupPath)

	paths, err := client.List(prefix+"/group", true)
	if err != nil {
		t.Fatalf("list group failed: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{groupPath}) {
		t.Fatalf("unexpected group list: %v", paths)
	}

	watchPath := "/jodis/" + product
	firstProxy := watchPath + "/proxy-1"
	secondProxy := watchPath + "/proxy-2"
	if err := client.Update(firstProxy, []byte("proxy-1")); err != nil {
		t.Fatalf("update first proxy failed: %v", err)
	}
	defer client.Delete(firstProxy)
	defer client.Delete(secondProxy)

	signal, paths, err := client.WatchInOrder(watchPath)
	if err != nil {
		t.Fatalf("watch failed: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{firstProxy}) {
		t.Fatalf("unexpected initial watch list: %v", paths)
	}
	if err := client.Update(secondProxy, []byte("proxy-2")); err != nil {
		t.Fatalf("update second proxy failed: %v", err)
	}
	select {
	case <-signal:
	case <-time.After(time.Second * 5):
		t.Fatalf("watch signal was not closed")
	}

	ephemeralPath := watchPath + "/proxy-ephemeral"
	ephemeral, err := client.CreateEphemeral(ephemeralPath, []byte("ephemeral"))
	if err != nil {
		t.Fatalf("create ephemeral failed: %v", err)
	}
	if err := client.Delete(ephemeralPath); err != nil {
		t.Fatalf("delete ephemeral failed: %v", err)
	}
	select {
	case <-ephemeral:
	case <-time.After(time.Second * 5):
		t.Fatalf("ephemeral signal was not closed")
	}
	value, err = client.Read(ephemeralPath, false)
	if err != nil {
		t.Fatalf("read deleted ephemeral failed: %v", err)
	}
	if value != nil {
		t.Fatalf("deleted ephemeral should be missing")
	}
}
