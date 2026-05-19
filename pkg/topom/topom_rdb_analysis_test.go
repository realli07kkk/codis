// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestRDBAnalysisManagerParsesWorkspaceFile(t *testing.T) {
	cfg := NewDefaultConfig()
	cfg.RDBAnalysisWorkspace = t.TempDir()
	manager := NewRDBAnalysisManager(cfg)
	defer manager.Close()

	writeRDBAnalysisFixture(t, cfg.RDBAnalysisWorkspace, "sample.rdb")
	job, err := manager.StartWorkspace("sample.rdb", RDBAnalysisOptions{TopN: 10})
	assert.MustNoError(err)

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
