// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	stdcontext "context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

const rdbAnalysisFixtureBase64 = "UkVESVMwMDAz/gAAFmtleV9pbl96ZXJvdGhfZGF0YWJhc2UEemVyb/4CABZrZXlfaW5fc2Vjb25kX2RhdGFiYXNlBnNlY29uZP8="

func writeRDBAnalysisFixture(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(rdbAnalysisFixtureBase64)
	assert.MustNoError(err)
	path := filepath.Join(dir, name)
	assert.MustNoError(os.MkdirAll(filepath.Dir(path), 0755))
	assert.MustNoError(os.WriteFile(path, data, 0644))
	return path
}

func waitRDBAnalysisJob(t *testing.T, get func() (*RDBAnalysisJob, error)) *RDBAnalysisJob {
	t.Helper()
	deadline := time.Now().Add(time.Second * 5)
	for time.Now().Before(deadline) {
		job, err := get()
		assert.MustNoError(err)
		switch job.Status {
		case RDBAnalysisStatusDone, RDBAnalysisStatusError, RDBAnalysisStatusCanceled:
			return job
		}
		time.Sleep(time.Millisecond * 20)
	}
	t.Fatalf("rdb analysis job did not finish")
	return nil
}

func assertRDBAnalysisJobIDV7(t *testing.T, id string) {
	t.Helper()
	u, err := uuid.Parse(id)
	assert.MustNoError(err)
	assert.Must(u.Version() == 7)
}

func openTopomWithConfig(cfg *Config) *Topom {
	t, err := New(newDiskClient(), cfg)
	assert.MustNoError(err)
	assert.MustNoError(t.Start(false))
	return t
}

func TestRDBAnalysisManagerParsesWorkspaceFile(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()

	writeRDBAnalysisFixture(t, cfg.RDBAnalysisWorkspace, "sample.rdb")
	job, err := manager.StartWorkspace("sample.rdb", RDBAnalysisOptions{TopN: 10})
	assert.MustNoError(err)
	assertRDBAnalysisJobIDV7(t, job.ID)

	done := waitRDBAnalysisJob(t, func() (*RDBAnalysisJob, error) {
		return manager.Get(job.ID)
	})
	assert.Must(done.Status == RDBAnalysisStatusDone)
	assert.Must(done.ObjectsRead == 2)
	assert.Must(done.DBCount == 2)
	assert.Must(done.TotalSize > 0)
	assert.Must(len(done.TypeSummary) == 1)
	assert.Must(done.TypeSummary[0].Name == "string")
	assert.Must(len(done.TopBigKeys) == 2)
	assert.Must(done.FlameGraph != nil)
}

func TestRDBAnalysisManagerUploadGeneratesUUIDV7JobID(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()

	data, err := base64.StdEncoding.DecodeString(rdbAnalysisFixtureBase64)
	assert.MustNoError(err)
	job, err := manager.StartUpload("upload.rdb", strings.NewReader(string(data)), RDBAnalysisOptions{})
	assert.MustNoError(err)
	assertRDBAnalysisJobIDV7(t, job.ID)
}

func TestRDBAnalysisConfigAllowsOldConfigWithoutRDBFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashboard.toml")
	assert.MustNoError(os.WriteFile(path, []byte(`
coordinator_name = "filesystem"
coordinator_addr = "/tmp/codis"
product_name = "codis-demo"
product_auth = ""
admin_addr = "127.0.0.1:0"
migration_method = "semi-async"
migration_parallel_slots = 100
migration_async_maxbulks = 200
migration_async_maxbytes = "32mb"
migration_async_numkeys = 500
migration_timeout = "30s"
sentinel_client_timeout = "10s"
sentinel_quorum = 2
sentinel_parallel_syncs = 1
sentinel_down_after = "30s"
sentinel_failover_timeout = "5m"
sentinel_notification_script = ""
sentinel_client_reconfig_script = ""
`), 0644))

	cfg := &Config{}
	assert.MustNoError(cfg.LoadFromFile(path))
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()
	assert.Must(manager.workspace != "")
	assert.Must(manager.maxUpload > 0)
	assert.Must(manager.maxConcurrent > 0)
	assert.Must(manager.maxRetained > 0)
	assert.Must(manager.maxTopN > 0)
	assert.Must(!cfg.RDBAnalysisRemoteFetchEnabled)
	assert.Must(cfg.RDBAnalysisRemoteFetchAuth == "")
}

func TestRDBAnalysisRemoteFetchConfigRequiresAuthWhenEnabled(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisRemoteFetchEnabled = true
	cfg.RDBAnalysisRemoteFetchAuth = ""
	assert.Must(cfg.Validate() != nil)
}

func TestRDBAnalysisManagerRejectsWorkspaceEscape(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()

	_, err := manager.StartWorkspace("../outside.rdb", RDBAnalysisOptions{})
	assert.Must(err != nil)
}

func TestRDBAnalysisManagerReportsInvalidRDB(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()

	assert.MustNoError(os.WriteFile(filepath.Join(cfg.RDBAnalysisWorkspace, "bad.rdb"), []byte("not an rdb"), 0644))
	job, err := manager.StartWorkspace("bad.rdb", RDBAnalysisOptions{})
	assert.MustNoError(err)

	done := waitRDBAnalysisJob(t, func() (*RDBAnalysisJob, error) {
		return manager.Get(job.ID)
	})
	assert.Must(done.Status == RDBAnalysisStatusError)
	assert.Must(done.Error != "")
}

func TestRDBAnalysisManagerRejectsOversizedUpload(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()
	manager.maxUpload = 3

	_, err := manager.StartUpload("too-large.rdb", strings.NewReader("1234"), RDBAnalysisOptions{})
	assert.Must(err != nil)

	entries, err := os.ReadDir(filepath.Join(cfg.RDBAnalysisWorkspace, "uploads"))
	assert.MustNoError(err)
	assert.Must(len(entries) == 0)
}

func TestRDBAnalysisManagerRejectsConcurrentJobLimit(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()
	manager.maxConcurrent = 1

	writeRDBAnalysisFixture(t, cfg.RDBAnalysisWorkspace, "sample.rdb")
	manager.jobs["busy"] = &RDBAnalysisJob{
		ID:     "busy",
		Status: RDBAnalysisStatusRunning,
		mu:     &sync.Mutex{},
	}

	_, err := manager.StartWorkspace("sample.rdb", RDBAnalysisOptions{})
	assert.Must(err != nil)
}

func TestRDBAnalysisOptionsAndPrefixHelpers(t *testing.T) {
	cfg := NewDefaultConfig()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()
	manager.maxTopN = 2

	options := manager.normalizeOptions(RDBAnalysisOptions{
		TopN:             100,
		MaxDepth:         100,
		PrefixSeparators: []string{"", "|", ":"},
	})
	assert.Must(options.TopN == 2)
	assert.Must(options.MaxDepth == rdbAnalysisMaxDepth)
	assert.Must(len(options.PrefixSeparators) == 2)
	assert.Must(options.PrefixSeparators[0] == "|")

	prefixes := analysisPrefixes(0, "svc:tenant:user:42", []string{":"}, 2)
	assert.Must(len(prefixes) == 2)
	assert.Must(prefixes[0] == "db0:svc:*")
	assert.Must(prefixes[1] == "db0:svc:tenant:*")
}

func TestRDBAnalysisTopHotKeysSortsByFrequency(t *testing.T) {
	keys := []RDBAnalysisKeyEntry{
		{Key: "b", Frequency: 1},
		{Key: "c", Frequency: 2},
		{Key: "a", Frequency: 2},
	}
	var top []RDBAnalysisKeyEntry
	for _, key := range keys {
		top = addTopKeyByFrequency(top, key, 2)
	}
	assert.Must(len(top) == 2)
	assert.Must(top[0].Key == "a")
	assert.Must(top[1].Key == "c")
}

func TestApiRDBAnalysis(t *testing.T) {
	topom := openTopom()
	defer topom.Close()
	topom.rdbAnalysis.workspace = t.TempDir()

	writeRDBAnalysisFixture(t, topom.rdbAnalysis.workspace, "api.rdb")

	client := newApiClient(topom)
	id, err := client.StartRDBAnalysis("api.rdb", RDBAnalysisOptions{TopN: 5})
	assert.MustNoError(err)
	assert.Must(id != "")

	done := waitRDBAnalysisJob(t, func() (*RDBAnalysisJob, error) {
		return client.GetRDBAnalysis(id)
	})
	assert.Must(done.Status == RDBAnalysisStatusDone)
	assert.Must(done.Source == "workspace:api.rdb")
	assert.Must(done.ObjectsRead == 2)

	assert.MustNoError(client.CancelRDBAnalysis(id))
	assert.MustNoError(client.RemoveRDBAnalysis(id))
	_, err = client.GetRDBAnalysis(id)
	assert.Must(err != nil)
}

func TestApiRDBAnalysisRequiresXAuth(t *testing.T) {
	topom := openTopom()
	defer topom.Close()
	topom.rdbAnalysis.workspace = t.TempDir()
	writeRDBAnalysisFixture(t, topom.rdbAnalysis.workspace, "api.rdb")

	client := NewApiClient(topom.model.AdminAddr)
	_, err := client.StartRDBAnalysis("api.rdb", RDBAnalysisOptions{})
	assert.Must(err != nil)
}

func TestApiRDBAnalysisRemoteFetchDisabled(t *testing.T) {
	topom := openTopom()
	defer topom.Close()

	client := newApiClient(topom)
	_, err := client.StartRDBAnalysisRemoteFetch("127.0.0.1:6379", RDBAnalysisOptions{})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "remote fetch is disabled"))
}

func TestApiRDBAnalysisRemoteFetchRejectsServerOutsideProduct(t *testing.T) {
	cfg := *config
	cfg.RDBAnalysisRemoteFetchEnabled = true
	cfg.RDBAnalysisRemoteFetchAuth = "secret"
	topom := openTopomWithConfig(&cfg)
	defer topom.Close()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newApiClient(topom)
	addr := strings.TrimPrefix(server.URL, "http://")
	_, err := client.StartRDBAnalysisRemoteFetch(addr, RDBAnalysisOptions{})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "not in current product"))
	assert.Must(hits == 0)
}

func TestApiRDBAnalysisRemoteFetchRequiresXAuthBeforeOutbound(t *testing.T) {
	topom := openRemoteFetchTopom(t)
	defer topom.Close()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := remoteFetchAddr(server)
	assert.MustNoError(topom.CreateGroup(102))
	assert.MustNoError(topom.GroupAddServer(102, "", addr))

	client := NewApiClient(topom.model.AdminAddr)
	_, err := client.StartRDBAnalysisRemoteFetch(addr, RDBAnalysisOptions{})
	assert.Must(err != nil)
	assert.Must(hits == 0)
}

func TestApiRDBAnalysisRemoteFetchRejectsInvalidServerAddr(t *testing.T) {
	topom := openRemoteFetchTopom(t)
	defer topom.Close()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	addr := remoteFetchAddr(server)
	client := newApiClient(topom)
	for _, serverAddr := range []string{
		"http://" + addr,
		addr + "/codis/rdb/latest",
		addr + "?auth=secret",
		addr + "#fragment",
	} {
		_, err := client.StartRDBAnalysisRemoteFetch(serverAddr, RDBAnalysisOptions{})
		assert.Must(err != nil)
		assert.Must(strings.Contains(err.Error(), "invalid rdb analysis remote fetch server_addr"))
	}
	assert.Must(hits == 0)
}

func openRemoteFetchTopom(t *testing.T) *Topom {
	t.Helper()
	cfg := *config
	cfg.RDBAnalysisRemoteFetchEnabled = true
	cfg.RDBAnalysisRemoteFetchAuth = "secret"
	topom := openTopomWithConfig(&cfg)
	topom.rdbAnalysis.workspace = t.TempDir()
	return topom
}

func newRDBExportFixtureServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(rdbAnalysisFixtureBase64)
	assert.MustNoError(err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Must(req.URL.Path == rdbAnalysisRemoteFetchPath)
		assert.Must(req.Header.Get(rdbAnalysisRemoteFetchAuthHeader) == "secret")
		w.Header().Set("Content-Disposition", `attachment; filename="remote.rdb"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	}))
	return server, data
}

func TestApiRDBAnalysisRemoteFetchStartsJob(t *testing.T) {
	topom := openRemoteFetchTopom(t)
	defer topom.Close()
	server, _ := newRDBExportFixtureServer(t)
	defer server.Close()

	addr := remoteFetchAddr(server)
	assert.MustNoError(topom.CreateGroup(100))
	assert.MustNoError(topom.GroupAddServer(100, "", addr))

	client := newApiClient(topom)
	id, err := client.StartRDBAnalysisRemoteFetch(addr, RDBAnalysisOptions{TopN: 5})
	assert.MustNoError(err)
	assert.Must(id != "")
	assertRDBAnalysisJobIDV7(t, id)

	done := waitRDBAnalysisJob(t, func() (*RDBAnalysisJob, error) {
		return client.GetRDBAnalysis(id)
	})
	assert.Must(done.Status == RDBAnalysisStatusDone)
	assert.Must(done.Source == "remote-http:"+addr+"/remote.rdb")
	assert.Must(done.ObjectsRead == 2)
	assert.Must(remoteFetchTempFileCount(t, topom.rdbAnalysis) == 1)

	assert.MustNoError(client.RemoveRDBAnalysis(id))
	assert.Must(remoteFetchTempFileCount(t, topom.rdbAnalysis) == 0)
}

func TestApiRDBAnalysisRemoteFetchCleansUpWhenAnalysisLimitReached(t *testing.T) {
	topom := openRemoteFetchTopom(t)
	defer topom.Close()
	topom.rdbAnalysis.maxConcurrent = 1
	topom.rdbAnalysis.jobs["busy"] = &RDBAnalysisJob{
		ID:     "busy",
		Status: RDBAnalysisStatusRunning,
		mu:     &sync.Mutex{},
	}
	server, _ := newRDBExportFixtureServer(t)
	defer server.Close()

	addr := remoteFetchAddr(server)
	assert.MustNoError(topom.CreateGroup(101))
	assert.MustNoError(topom.GroupAddServer(101, "", addr))

	client := newApiClient(topom)
	_, err := client.StartRDBAnalysisRemoteFetch(addr, RDBAnalysisOptions{})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "too many running rdb analysis jobs"))
	assert.Must(remoteFetchTempFileCount(t, topom.rdbAnalysis) == 0)
}

func TestApiRDBAnalysisRemoteFetchRejectsFetchConcurrencyLimit(t *testing.T) {
	topom := openRemoteFetchTopom(t)
	defer topom.Close()
	topom.rdbAnalysis.remoteFetchMaxConcurrent = 1
	data, err := base64.StdEncoding.DecodeString(rdbAnalysisFixtureBase64)
	assert.MustNoError(err)

	started := make(chan struct{})
	release := make(chan struct{})
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			close(started)
			<-release
		}
		w.Header().Set("Content-Disposition", `attachment; filename="remote.rdb"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	}))
	defer server.Close()

	addr := remoteFetchAddr(server)
	assert.MustNoError(topom.CreateGroup(103))
	assert.MustNoError(topom.GroupAddServer(103, "", addr))

	client := newApiClient(topom)
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, err := client.StartRDBAnalysisRemoteFetch(addr, RDBAnalysisOptions{})
		done <- result{id: id, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("first remote fetch did not start")
	}
	_, err = client.StartRDBAnalysisRemoteFetch(addr, RDBAnalysisOptions{})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "too many running rdb analysis remote fetches"))
	assert.Must(atomic.LoadInt32(&hits) == 1)

	close(release)
	first := <-done
	assert.MustNoError(first.err)
	assertRDBAnalysisJobIDV7(t, first.id)
}

func newRemoteFetchTestManager(t *testing.T) *RDBAnalysisManager {
	t.Helper()
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	cfg.RDBAnalysisRemoteFetchAuth = "secret"
	manager := NewRDBAnalysisManager(cfg)
	manager.maxUpload = 1024
	return manager
}

func remoteFetchAddr(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

func remoteFetchTarget(server *httptest.Server) rdbAnalysisRemoteFetchTarget {
	return rdbAnalysisRemoteFetchTarget{serverAddr: remoteFetchAddr(server)}
}

func remoteFetchTempFileCount(t *testing.T, manager *RDBAnalysisManager) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(manager.workspace, "remote"))
	if os.IsNotExist(err) {
		return 0
	}
	assert.MustNoError(err)
	return len(entries)
}

func TestRDBAnalysisRemoteFetchDownloadsToTempFile(t *testing.T) {
	manager := newRemoteFetchTestManager(t)
	defer manager.Close()
	data, err := base64.StdEncoding.DecodeString(rdbAnalysisFixtureBase64)
	assert.MustNoError(err)

	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth = req.Header.Get(rdbAnalysisRemoteFetchAuthHeader)
		w.Header().Set("Content-Disposition", `attachment; filename="dump.rdb"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
	}))
	defer server.Close()

	file, err := manager.fetchRemoteRDB(stdcontext.Background(), remoteFetchTarget(server))
	assert.MustNoError(err)
	defer os.Remove(file.path)
	assert.Must(auth == "secret")
	assert.Must(file.size == int64(len(data)))
	assert.Must(file.source == "remote-http:"+remoteFetchAddr(server)+"/dump.rdb")
	got, err := os.ReadFile(file.path)
	assert.MustNoError(err)
	assert.Must(string(got) == string(data))
}

func TestRDBAnalysisRemoteFetchRejectsHTTPError(t *testing.T) {
	manager := newRemoteFetchTestManager(t)
	defer manager.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("secret path should not be relayed"))
	}))
	defer server.Close()

	_, err := manager.fetchRemoteRDB(stdcontext.Background(), remoteFetchTarget(server))
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "403 Forbidden"))
	assert.Must(!strings.Contains(err.Error(), "secret path"))
	assert.Must(remoteFetchTempFileCount(t, manager) == 0)
}

func TestRDBAnalysisRemoteFetchDoesNotFollowRedirect(t *testing.T) {
	manager := newRemoteFetchTestManager(t)
	defer manager.Close()
	var redirectedAuth string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		redirectedAuth = req.Header.Get(rdbAnalysisRemoteFetchAuthHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, target.URL, http.StatusFound)
	}))
	defer server.Close()

	_, err := manager.fetchRemoteRDB(stdcontext.Background(), remoteFetchTarget(server))
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "302 Found"))
	assert.Must(redirectedAuth == "")
}

func TestRDBAnalysisRemoteFetchRejectsOversizedContentLength(t *testing.T) {
	manager := newRemoteFetchTestManager(t)
	defer manager.Close()
	manager.maxUpload = 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.Write([]byte("1234"))
	}))
	defer server.Close()

	_, err := manager.fetchRemoteRDB(stdcontext.Background(), remoteFetchTarget(server))
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "exceeds max size"))
	assert.Must(remoteFetchTempFileCount(t, manager) == 0)
}

func TestRDBAnalysisRemoteFetchRejectsOversizedBody(t *testing.T) {
	manager := newRemoteFetchTestManager(t)
	defer manager.Close()
	manager.maxUpload = 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("1234"))
	}))
	defer server.Close()

	_, err := manager.fetchRemoteRDB(stdcontext.Background(), remoteFetchTarget(server))
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "exceeds max size"))
	assert.Must(remoteFetchTempFileCount(t, manager) == 0)
}

func TestRDBAnalysisRemoteFetchHonorsContextCancel(t *testing.T) {
	manager := newRemoteFetchTestManager(t)
	defer manager.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("REDIS0001"))
	}))
	defer server.Close()

	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	cancel()
	_, err := manager.fetchRemoteRDB(ctx, remoteFetchTarget(server))
	assert.Must(err != nil)
	assert.Must(remoteFetchTempFileCount(t, manager) == 0)
}
