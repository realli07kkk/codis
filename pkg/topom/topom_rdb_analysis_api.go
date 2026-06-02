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

type RDBAnalysisStartRequest struct {
	Path    string             `json:"path"`
	Options RDBAnalysisOptions `json:"options"`
}

type RDBAnalysisStartResponse struct {
	ID string `json:"id"`
}

type RDBAnalysisRemoteFetchRequest struct {
	ServerAddr string             `json:"server_addr"`
	Options    RDBAnalysisOptions `json:"options"`
}

func (s *apiServer) rdbAnalysisManager() (*RDBAnalysisManager, error) {
	if s.topom == nil || s.topom.rdbAnalysis == nil {
		return nil, errors.New("rdb analysis is not initialized")
	}
	return s.topom.rdbAnalysis, nil
}

func (s *apiServer) RDBAnalysisUpload(params martini.Params, w http.ResponseWriter, req *http.Request) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.rdbAnalysisManager()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	req.Body = http.MaxBytesReader(w, req.Body, manager.MaxUploadSize()+1)
	if err := req.ParseMultipartForm(rdbAnalysisMultipartMemoryMax); err != nil {
		return rpc.ApiResponseError(errors.Trace(err))
	}
	file, header, err := req.FormFile("file")
	if err != nil {
		return rpc.ApiResponseError(errors.Trace(err))
	}
	defer file.Close()

	options, err := parseRDBAnalysisOptionsFromRequest(req)
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	job, err := manager.StartUpload(header.Filename, file, options)
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(&RDBAnalysisStartResponse{ID: job.ID})
}

func (s *apiServer) RDBAnalysisStart(params martini.Params, req *http.Request) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.rdbAnalysisManager()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	var request RDBAnalysisStartRequest
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return rpc.ApiResponseError(errors.Trace(err))
	}
	job, err := manager.StartWorkspace(request.Path, request.Options)
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(&RDBAnalysisStartResponse{ID: job.ID})
}

func (s *apiServer) RDBAnalysisRemoteFetch(params martini.Params, req *http.Request) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	if _, err := s.rdbAnalysisManager(); err != nil {
		return rpc.ApiResponseError(err)
	}
	config := s.topom.Config()
	if !config.RDBAnalysisRemoteFetchEnabled {
		return rpc.ApiResponseError(errors.New("rdb analysis remote fetch is disabled"))
	}
	if config.RDBAnalysisRemoteFetchAuth == "" {
		return rpc.ApiResponseError(errors.New("missing rdb_analysis_remote_fetch_auth"))
	}

	var request RDBAnalysisRemoteFetchRequest
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return rpc.ApiResponseError(errors.Trace(err))
	}
	job, err := s.topom.startRDBAnalysisRemoteFetch(req.Context(), request.ServerAddr, request.Options)
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(&RDBAnalysisStartResponse{ID: job.ID})
}

func (s *apiServer) RDBAnalysisGet(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.rdbAnalysisManager()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	job, err := manager.Get(params["id"])
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson(job)
}

func (s *apiServer) RDBAnalysisCancel(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.rdbAnalysisManager()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	if err := manager.Cancel(params["id"]); err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson("OK")
}

func (s *apiServer) RDBAnalysisRemove(params martini.Params) (int, string) {
	if err := s.verifyXAuth(params); err != nil {
		return rpc.ApiResponseError(err)
	}
	manager, err := s.rdbAnalysisManager()
	if err != nil {
		return rpc.ApiResponseError(err)
	}
	if err := manager.Remove(params["id"]); err != nil {
		return rpc.ApiResponseError(err)
	}
	return rpc.ApiResponseJson("OK")
}

func parseRDBAnalysisOptionsFromRequest(req *http.Request) (RDBAnalysisOptions, error) {
	var options RDBAnalysisOptions
	if text := req.FormValue("options"); text != "" {
		if err := json.Unmarshal([]byte(text), &options); err != nil {
			return options, errors.Trace(err)
		}
		return options, nil
	}
	options.TopN = parseRDBAnalysisInt(req.FormValue("top_n"))
	options.MaxDepth = parseRDBAnalysisInt(req.FormValue("max_depth"))
	options.Regex = req.FormValue("regex")
	options.IncludeExpired = parseRDBAnalysisBool(req.FormValue("include_expired"))
	options.PrefixSeparators = splitRDBAnalysisSeparators(req.FormValue("prefix_separators"))
	return options, nil
}

func (c *ApiClient) StartRDBAnalysis(path string, options RDBAnalysisOptions) (string, error) {
	url := c.encodeURL("/api/topom/rdb-analysis/start/%s", c.xauth)
	var resp RDBAnalysisStartResponse
	if err := rpc.ApiPutJson(url, &RDBAnalysisStartRequest{Path: path, Options: options}, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *ApiClient) StartRDBAnalysisRemoteFetch(serverAddr string, options RDBAnalysisOptions) (string, error) {
	url := c.encodeURL("/api/topom/rdb-analysis/remote-fetch/%s", c.xauth)
	var resp RDBAnalysisStartResponse
	if err := rpc.ApiPutJson(url, &RDBAnalysisRemoteFetchRequest{ServerAddr: serverAddr, Options: options}, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *ApiClient) GetRDBAnalysis(id string) (*RDBAnalysisJob, error) {
	url := c.encodeURL("/api/topom/rdb-analysis/%s/%s", c.xauth, id)
	job := &RDBAnalysisJob{}
	if err := rpc.ApiGetJson(url, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (c *ApiClient) CancelRDBAnalysis(id string) error {
	url := c.encodeURL("/api/topom/rdb-analysis/cancel/%s/%s", c.xauth, id)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) RemoveRDBAnalysis(id string) error {
	url := c.encodeURL("/api/topom/rdb-analysis/remove/%s/%s", c.xauth, id)
	return rpc.ApiPutJson(url, nil, nil)
}
