// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	"net/http"

	"github.com/go-martini/martini"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/rpc"
)

// PitrCreateRequest is the body of PUT /api/topom/pitr/create/:xauth.
type PitrCreateRequest struct {
	ServerAddr string `json:"server_addr"`
	TruncateTs int64  `json:"truncate_ts"`
}

// PitrCreateResponse returns the UUID v7 job id of the newly created job. The
// wire field is "job_id" per the SDD API contract.
type PitrCreateResponse struct {
	JobID string `json:"job_id"`
}

// pitrManager returns the manager or an error if PITR is not initialized. The
// "pitr disabled" check happens in the handler so callers can distinguish
// "feature off" from "manager missing".
func (s *apiServer) pitrManager() (*PitrManager, error) {
	if s.topom == nil || s.topom.pitr == nil {
		return nil, errors.New("pitr is not initialized")
	}
	return s.topom.pitr, nil
}

// pitrManagerEnabled returns the manager only when PITR is enabled. Every /pitr/*
// handler (not just create) goes through this so that disabled means fully
// disabled — list/get/cancel/remove also refuse, matching the SDD "pitr_enabled=
// false 时所有 /pitr/* 返回 503" contract.
func (s *apiServer) pitrManagerEnabled() (*PitrManager, error) {
	manager, err := s.pitrManager()
	if err != nil {
		return nil, err
	}
	if !manager.Enabled() {
		return nil, errors.New("pitr is disabled")
	}
	return manager, nil
}

// PitrCreate is the PUT /api/topom/pitr/create/:xauth handler. It validates
// xauth, then delegates to PitrManager.Create. The manager performs the
// enabled / concurrency / per-server-lock checks synchronously and launches
// the state machine asynchronously.
func (s *apiServer) PitrCreate(params martini.Params, req *http.Request) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.pitrManagerEnabled()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	var request PitrCreateRequest
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return rpc.ApiResponseError(errors.Trace(err))
	}
	if request.ServerAddr == "" {
		return rpc.ApiResponseError(errors.New("missing server_addr"))
	}
	if request.TruncateTs <= 0 {
		return rpc.ApiResponseError(errors.New("invalid truncate_ts"))
	}
	deps := s.topom.newPitrDeps()
	job, err := manager.Create(s.topom.Config().ProductName, request.ServerAddr, request.TruncateTs, deps)
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(&PitrCreateResponse{JobID: job.ID})
}

// PitrList is the GET /api/topom/pitr/jobs/:xauth handler. It returns
// snapshots of all jobs (newest first) without secrets or AOF contents.
func (s *apiServer) PitrList(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.pitrManagerEnabled()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(manager.List())
}

// PitrGet is the GET /api/topom/pitr/:xauth/:id handler.
func (s *apiServer) PitrGet(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.pitrManagerEnabled()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	job, err := manager.Get(params["id"])
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(job)
}

// PitrCancel is the PUT /api/topom/pitr/cancel/:xauth/:id handler. Cancelling
// a terminal job is a no-op (returns OK).
func (s *apiServer) PitrCancel(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.pitrManagerEnabled()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	if err := manager.Cancel(params["id"]); err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson("OK")
}

// PitrRemove is the PUT /api/topom/pitr/remove/:xauth/:id handler. It deletes
// the job and best-effort removes its snapshot directory.
func (s *apiServer) PitrRemove(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.pitrManagerEnabled()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	if err := manager.Remove(params["id"]); err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson("OK")
}

// --- ApiClient methods (used by codis-admin, which never connects directly) ---

// CreatePitr starts a PITR job via the dashboard and returns the job id.
func (c *ApiClient) CreatePitr(serverAddr string, truncateTs int64) (string, error) {
	url := c.encodeURL("/api/topom/pitr/create/%s", c.xauth)
	var resp PitrCreateResponse
	if err := rpc.ApiPutJson(url, &PitrCreateRequest{ServerAddr: serverAddr, TruncateTs: truncateTs}, &resp); err != nil {
		return "", err
	}
	return resp.JobID, nil
}

// ListPitr returns all known PITR job snapshots.
func (c *ApiClient) ListPitr() ([]*PitrJob, error) {
	url := c.encodeURL("/api/topom/pitr/jobs/%s", c.xauth)
	var jobs []*PitrJob
	if err := rpc.ApiGetJson(url, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// GetPitr returns a single PITR job snapshot.
func (c *ApiClient) GetPitr(id string) (*PitrJob, error) {
	url := c.encodeURL("/api/topom/pitr/%s/%s", c.xauth, id)
	job := &PitrJob{}
	if err := rpc.ApiGetJson(url, job); err != nil {
		return nil, err
	}
	return job, nil
}

// CancelPitr cancels a running PITR job (no-op for terminal jobs).
func (c *ApiClient) CancelPitr(id string) error {
	url := c.encodeURL("/api/topom/pitr/cancel/%s/%s", c.xauth, id)
	return rpc.ApiPutJson(url, nil, nil)
}

// RemovePitr removes a PITR job and its snapshot directory.
func (c *ApiClient) RemovePitr(id string) error {
	url := c.encodeURL("/api/topom/pitr/remove/%s/%s", c.xauth, id)
	return rpc.ApiPutJson(url, nil, nil)
}
