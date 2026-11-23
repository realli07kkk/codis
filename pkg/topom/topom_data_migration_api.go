// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/rpc"
)

// DataMigrationCreateResponse returns the UUID v7 id of the newly created job.
type DataMigrationCreateResponse struct {
	ID string `json:"id"`
}

// dataMigrationManager returns the manager only when the feature is enabled,
// so every /data-migration/* route refuses consistently when it is off.
func (s *apiServer) dataMigrationManager() (*DataMigrationManager, error) {
	if s.topom == nil || s.topom.dataMigration == nil {
		return nil, errors.New("data migration is not initialized")
	}
	if !s.topom.dataMigration.Enabled() {
		return nil, errors.New("data migration is disabled")
	}
	return s.topom.dataMigration, nil
}

// DataMigrationJobs is GET /api/topom/data-migration/jobs/:xauth.
func (s *apiServer) DataMigrationJobs(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	manager, err := s.dataMigrationManager()
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, manager.List())
}

// DataMigrationTopologyPreview is GET /api/topom/data-migration/topology/codis/:xauth.
func (s *apiServer) DataMigrationTopologyPreview(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	if _, err := s.dataMigrationManager(); err != nil {
		s.writeError(w, err)
		return
	}
	topology, err := s.topom.dataMigration.Topology()
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, topology)
}

// DataMigrationCreate is PUT /api/topom/data-migration/create/:xauth. It
// validates the request, resolves the Codis topology when a side is the
// current product and starts the worker synchronously.
func (s *apiServer) DataMigrationCreate(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	manager, err := s.dataMigrationManager()
	if err != nil {
		s.writeError(w, err)
		return
	}
	var request DataMigrationCreateRequest
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		s.writeError(w, errors.Trace(err))
		return
	}
	job, err := manager.Create(&request)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, &DataMigrationCreateResponse{ID: job.ID})
}

// DataMigrationGet is GET /api/topom/data-migration/get/:xauth/:id.
func (s *apiServer) DataMigrationGet(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	manager, err := s.dataMigrationManager()
	if err != nil {
		s.writeError(w, err)
		return
	}
	job, err := manager.Get(req.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, job)
}

// DataMigrationStop is PUT /api/topom/data-migration/stop/:xauth/:id.
func (s *apiServer) DataMigrationStop(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	manager, err := s.dataMigrationManager()
	if err != nil {
		s.writeError(w, err)
		return
	}
	if err := manager.Stop(req.PathValue("id")); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

// DataMigrationRemove is PUT /api/topom/data-migration/remove/:xauth/:id.
func (s *apiServer) DataMigrationRemove(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	manager, err := s.dataMigrationManager()
	if err != nil {
		s.writeError(w, err)
		return
	}
	if err := manager.Remove(req.PathValue("id")); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

// DataMigrationLog is GET /api/topom/data-migration/log/:xauth/:id.
func (s *apiServer) DataMigrationLog(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	manager, err := s.dataMigrationManager()
	if err != nil {
		s.writeError(w, err)
		return
	}
	offset, err := parseDataMigrationInt(req, "offset", 0)
	if err != nil {
		s.writeError(w, err)
		return
	}
	maxBytes, err := parseDataMigrationInt(req, "max_bytes", 0)
	if err != nil {
		s.writeError(w, err)
		return
	}
	page, err := manager.Logs(req.PathValue("id"), int64(offset), maxBytes)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, page)
}

func parseDataMigrationInt(req *http.Request, name string, fallback int) (int, error) {
	text := req.URL.Query().Get(name)
	if text == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, errors.Errorf("invalid %s", name)
	}
	return v, nil
}

// --- ApiClient methods (used by codis-admin, which never connects directly) ---

// CreateDataMigration starts a migration task and returns the job id.
func (c *ApiClient) CreateDataMigration(req *DataMigrationCreateRequest) (string, error) {
	url := c.encodeURL("/api/topom/data-migration/create/%s", c.xauth)
	var resp DataMigrationCreateResponse
	if err := rpc.ApiPutJson(url, req, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// ListDataMigration returns all known migration job snapshots.
func (c *ApiClient) ListDataMigration() ([]*DataMigrationJob, error) {
	url := c.encodeURL("/api/topom/data-migration/jobs/%s", c.xauth)
	var jobs []*DataMigrationJob
	if err := rpc.ApiGetJson(url, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// GetDataMigration returns a single migration job snapshot.
func (c *ApiClient) GetDataMigration(id string) (*DataMigrationJob, error) {
	url := c.encodeURL("/api/topom/data-migration/get/%s/%s", c.xauth, id)
	job := &DataMigrationJob{}
	if err := rpc.ApiGetJson(url, job); err != nil {
		return nil, err
	}
	return job, nil
}

// StopDataMigration stops a running job (no-op for terminal jobs).
func (c *ApiClient) StopDataMigration(id string) error {
	url := c.encodeURL("/api/topom/data-migration/stop/%s/%s", c.xauth, id)
	return rpc.ApiPutJson(url, nil, nil)
}

// RemoveDataMigration deletes a terminal job and its worker directory.
func (c *ApiClient) RemoveDataMigration(id string) error {
	url := c.encodeURL("/api/topom/data-migration/remove/%s/%s", c.xauth, id)
	return rpc.ApiPutJson(url, nil, nil)
}

// GetDataMigrationLog reads a page of the worker log.
func (c *ApiClient) GetDataMigrationLog(id string, offset, maxBytes int) (*DataMigrationLog, error) {
	// encodeURL escapes everything into the URL path, so the query string must
	// be appended after it instead of being part of the format string.
	url := c.encodeURL("/api/topom/data-migration/log/%s/%s", c.xauth, id)
	url = fmt.Sprintf("%s?offset=%d&max_bytes=%d", url, offset, maxBytes)
	page := &DataMigrationLog{}
	if err := rpc.ApiGetJson(url, page); err != nil {
		return nil, err
	}
	return page, nil
}

// GetDataMigrationTopology returns the Codis topology preview of the product.
func (c *ApiClient) GetDataMigrationTopology() (*DataMigrationTopology, error) {
	url := c.encodeURL("/api/topom/data-migration/topology/codis/%s", c.xauth)
	topology := &DataMigrationTopology{}
	if err := rpc.ApiGetJson(url, topology); err != nil {
		return nil, err
	}
	return topology, nil
}
