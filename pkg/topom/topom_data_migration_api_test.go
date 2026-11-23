// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/migration"
)

// These tests pin the HTTP contract of the /api/topom/data-migration/* routes
// on top of the manager tests. They reuse the helpers of
// topom_api_contract_test.go (serveTopomAPIRequest & friends) and the helper
// worker of topom_data_migration_test.go, so no Redis or codis-shake binary is
// required.

// newDataMigrationAPIServer builds an online Topom whose data migration manager
// is enabled against a per-test workspace, using this test binary as the
// codis-shake worker. It returns the handler and the xauth the routes expect.
func newDataMigrationAPIServer(t *testing.T) (*Topom, http.Handler, string) {
	t.Helper()
	cfg := NewDefaultConfig()
	cfg.AdminAddr = "127.0.0.1:0"
	cfg.ProductName = "topom_data_migration_api_test"
	cfg.ProductAuth = "topom_auth"
	cfg.DataMigrationEnabled = true
	cfg.DataMigrationWorkspace = t.TempDir()
	cfg.DataMigrationShakeBin = mustTestBinary(t)
	cfg.DataMigrationMaxConcurrentJobs = 2
	cfg.DataMigrationMaxJobsRetained = 10
	s := openTopomWithConfig(t, cfg)
	t.Cleanup(func() {
		_ = s.Close()
	})

	m := s.dataMigration
	m.pollInterval = 200 * time.Millisecond
	m.stopGrace = 3 * time.Second
	m.startTimeout = 5 * time.Second
	m.spawnArgs = func(specPath string) []string {
		return []string{"-test.run=^TestDataMigrationHelperProcess$", "--", specPath}
	}
	return s, newApiServer(s), s.XAuth()
}

func newDataMigrationDisabledAPIServer(t *testing.T) (*Topom, http.Handler, string) {
	t.Helper()
	cfg := NewDefaultConfig()
	cfg.AdminAddr = "127.0.0.1:0"
	cfg.ProductName = "topom_data_migration_off_test"
	cfg.ProductAuth = "topom_auth"
	cfg.DataMigrationEnabled = false
	cfg.DataMigrationWorkspace = t.TempDir()
	s := openTopomWithConfig(t, cfg)
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s, newApiServer(s), s.XAuth()
}

func TestDataMigrationAPIDisabled(t *testing.T) {
	_, h, xauth := newDataMigrationDisabledAPIServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"jobs", "GET", "/api/topom/data-migration/jobs/" + xauth, ""},
		{"topology", "GET", "/api/topom/data-migration/topology/codis/" + xauth, ""},
		{"create", "PUT", "/api/topom/data-migration/create/" + xauth, "{}"},
		{"get", "GET", "/api/topom/data-migration/get/" + xauth + "/unknown-id", ""},
		{"stop", "PUT", "/api/topom/data-migration/stop/" + xauth + "/unknown-id", ""},
		{"remove", "PUT", "/api/topom/data-migration/remove/" + xauth + "/unknown-id", ""},
		{"log", "GET", "/api/topom/data-migration/log/" + xauth + "/unknown-id", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: tc.method, path: tc.path, body: tc.body})
			requireTopomAPIStatus(t, rec, 800)
			var remoteError struct {
				Cause string
			}
			decodeTopomAPIResponse(t, rec, &remoteError)
			requireTopomAPIContains(t, remoteError.Cause, "disabled")
		})
	}
}

func TestDataMigrationAPIListAndTopology(t *testing.T) {
	s, h, xauth := newDataMigrationAPIServer(t)

	t.Run("empty job list is an array", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/jobs/" + xauth})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body = %q, want []", body)
		}
		var jobs []*DataMigrationJob
		decodeTopomAPIResponse(t, rec, &jobs)
		if jobs == nil || len(jobs) != 0 {
			t.Fatalf("jobs = %+v, want empty non-nil slice", jobs)
		}
	})

	t.Run("topology preview", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/topology/codis/" + xauth})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var topology DataMigrationTopology
		decodeTopomAPIResponse(t, rec, &topology)
		if !topology.Online {
			t.Fatalf("topology = %+v, want online", topology)
		}
		if topology.Complete || len(topology.Groups) != 0 {
			t.Fatalf("empty product topology = %+v", topology)
		}
	})

	t.Run("api client round trip", func(t *testing.T) {
		client := NewApiClient(s.Model().AdminAddr)
		client.SetXAuth(s.Config().ProductName)

		jobs, err := client.ListDataMigration()
		if err != nil {
			t.Fatal(err)
		}
		if jobs == nil || len(jobs) != 0 {
			t.Fatalf("api client jobs = %+v, want empty non-nil slice", jobs)
		}
		topology, err := client.GetDataMigrationTopology()
		if err != nil {
			t.Fatal(err)
		}
		if !topology.Online {
			t.Fatalf("api client topology = %+v, want online", topology)
		}
	})
}

func TestDataMigrationAPICreateValidation(t *testing.T) {
	_, h, xauth := newDataMigrationAPIServer(t)
	createPath := "/api/topom/data-migration/create/" + xauth

	putCreate := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		return serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "PUT", path: createPath, body: body,
			headers: map[string]string{"Content-Type": "application/json"},
		})
	}

	t.Run("missing name", func(t *testing.T) {
		rec := putCreate(t, `{}`)
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		requireTopomAPIContains(t, remoteError.Cause, "missing name")
	})

	t.Run("invalid json", func(t *testing.T) {
		rec := putCreate(t, `{"name":`)
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		if strings.TrimSpace(remoteError.Cause) == "" {
			t.Fatalf("empty cause for invalid json: %q", rec.Body.String())
		}
	})

	t.Run("invalid mode", func(t *testing.T) {
		rec := putCreate(t, `{
			"name": "copy job",
			"mode": "copy",
			"source": {"kind": "standalone", "address": "127.0.0.1:6379"},
			"target": {"kind": "standalone", "address": "127.0.0.1:6380"}
		}`)
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		requireTopomAPIContains(t, remoteError.Cause, "invalid mode")
	})

	t.Run("rejected requests create no jobs", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/jobs/" + xauth})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var jobs []*DataMigrationJob
		decodeTopomAPIResponse(t, rec, &jobs)
		if len(jobs) != 0 {
			t.Fatalf("jobs = %+v, want none", jobs)
		}
	})
}

func TestDataMigrationAPIUnknownJob(t *testing.T) {
	_, h, xauth := newDataMigrationAPIServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{"get", "GET", "/api/topom/data-migration/get/" + xauth + "/unknown-id", "not found"},
		{"stop", "PUT", "/api/topom/data-migration/stop/" + xauth + "/unknown-id", "not found"},
		{"remove", "PUT", "/api/topom/data-migration/remove/" + xauth + "/unknown-id", "not found"},
		{"log", "GET", "/api/topom/data-migration/log/" + xauth + "/unknown-id", "not found"},
		// Query parsing runs before the job lookup, so no job is required.
		{"invalid offset", "GET", "/api/topom/data-migration/log/" + xauth + "/unknown-id?offset=abc", "invalid offset"},
		{"invalid max bytes", "GET", "/api/topom/data-migration/log/" + xauth + "/unknown-id?max_bytes=abc", "invalid max_bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: tc.method, path: tc.path})
			requireTopomAPIStatus(t, rec, 800)
			var remoteError struct {
				Cause string
			}
			decodeTopomAPIResponse(t, rec, &remoteError)
			requireTopomAPIContains(t, remoteError.Cause, tc.want)
		})
	}
}

func TestDataMigrationAPIRejectsInvalidXAuth(t *testing.T) {
	_, h, _ := newDataMigrationAPIServer(t)
	bad := "not-the-xauth"

	for _, path := range []string{
		"/api/topom/data-migration/jobs/" + bad,
		"/api/topom/data-migration/topology/codis/" + bad,
		"/api/topom/data-migration/create/" + bad,
		"/api/topom/data-migration/get/" + bad + "/some-id",
		"/api/topom/data-migration/stop/" + bad + "/some-id",
		"/api/topom/data-migration/remove/" + bad + "/some-id",
		"/api/topom/data-migration/log/" + bad + "/some-id",
	} {
		method := "GET"
		if strings.Contains(path, "/create/") || strings.Contains(path, "/stop/") || strings.Contains(path, "/remove/") {
			method = "PUT"
		}
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: method, path: path, body: "{}"})
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		requireTopomAPIContains(t, remoteError.Cause, "invalid xauth")
	}
}

func TestDataMigrationAPICreateAndLifecycle(t *testing.T) {
	s, h, xauth := newDataMigrationAPIServer(t)
	m := s.dataMigration

	body := `{
		"name": "api scan",
		"mode": "scan",
		"source": {"kind": "standalone", "address": "127.0.0.1:6379"},
		"target": {"kind": "standalone", "address": "127.0.0.1:6380", "password": "pw"},
		"filter": {"block_command": ["FLUSHALL"]}
	}`
	rec := serveTopomAPIRequest(t, h, topomAPIRequest{
		method: "PUT", path: "/api/topom/data-migration/create/" + xauth, body: body,
		headers: map[string]string{"Content-Type": "application/json"},
	})
	requireTopomAPIStatus(t, rec, http.StatusOK)
	var created DataMigrationCreateResponse
	decodeTopomAPIResponse(t, rec, &created)
	if created.ID == "" {
		t.Fatalf("create response = %+v", created)
	}

	waitForDataMigrationState(t, m, created.ID, true)

	t.Run("get", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/get/" + xauth + "/" + created.ID})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var job DataMigrationJob
		decodeTopomAPIResponse(t, rec, &job)
		if job.ID != created.ID || job.State != DataMigrationStateSucceeded || job.Mode != migration.ModeScan {
			t.Fatalf("job = %+v", job)
		}
		if job.Source.Address != "127.0.0.1:6379" || job.Target.Address != "127.0.0.1:6380" {
			t.Fatalf("endpoints = %+v -> %+v", job.Source, job.Target)
		}
		if !job.Target.HasPassword || job.CreatedAt.IsZero() || job.Dir == "" {
			t.Fatalf("job fields = %+v", job)
		}
	})

	t.Run("log", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/log/" + xauth + "/" + created.ID})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var page DataMigrationLog
		decodeTopomAPIResponse(t, rec, &page)
		if page.File != migration.LogFileName || !strings.Contains(page.Text, "start syncing") {
			t.Fatalf("log page = %+v", page)
		}
		if page.NextOffset != int64(len(page.Text)) || page.Truncated || page.Size == 0 {
			t.Fatalf("log offsets = %+v", page)
		}

		rec = serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/log/" + xauth + "/" + created.ID + "?offset=0&max_bytes=10"})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var small DataMigrationLog
		decodeTopomAPIResponse(t, rec, &small)
		if len(small.Text) != 10 || !small.Truncated || small.NextOffset != 10 {
			t.Fatalf("small log page = %+v", small)
		}

		rec = serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/log/" + xauth + "/" + created.ID + "?offset=abc"})
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		requireTopomAPIContains(t, remoteError.Cause, "invalid offset")
	})

	t.Run("list", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/jobs/" + xauth})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		var jobs []*DataMigrationJob
		decodeTopomAPIResponse(t, rec, &jobs)
		if len(jobs) != 1 || jobs[0].ID != created.ID {
			t.Fatalf("jobs = %+v", jobs)
		}
	})

	t.Run("api client", func(t *testing.T) {
		client := NewApiClient(s.Model().AdminAddr)
		client.SetXAuth(s.Config().ProductName)

		job, err := client.GetDataMigration(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.ID != created.ID || job.State != DataMigrationStateSucceeded {
			t.Fatalf("api client job = %+v", job)
		}
		page, err := client.GetDataMigrationLog(created.ID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(page.Text, "start syncing") {
			t.Fatalf("api client log page = %+v", page)
		}
		if err := client.StopDataMigration(created.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("remove", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "PUT", path: "/api/topom/data-migration/remove/" + xauth + "/" + created.ID})
		requireTopomAPIStatus(t, rec, http.StatusOK)
		if body := rec.Body.String(); body != `"OK"` {
			t.Fatalf("remove body = %q", body)
		}
		rec = serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: "/api/topom/data-migration/get/" + xauth + "/" + created.ID})
		requireTopomAPIStatus(t, rec, 800)
		var remoteError struct {
			Cause string
		}
		decodeTopomAPIResponse(t, rec, &remoteError)
		requireTopomAPIContains(t, remoteError.Cause, "not found")
	})
}
