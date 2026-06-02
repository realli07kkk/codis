// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	stdcontext "context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/bytesize"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

const (
	rdbAnalysisRemoteFetchPath            = "/codis/rdb/latest"
	rdbAnalysisRemoteFetchAuthHeader      = "X-Codis-RDB-Auth"
	rdbAnalysisRemoteFetchDefaultFilename = "dump.rdb"
)

type rdbAnalysisRemoteFetchFile struct {
	source string
	path   string
	size   int64
}

type rdbAnalysisRemoteFetchTarget struct {
	serverAddr string
}

func (s *Topom) verifyRDBAnalysisRemoteFetchServer(addr string) (rdbAnalysisRemoteFetchTarget, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return rdbAnalysisRemoteFetchTarget{}, errors.New("missing rdb analysis remote fetch server_addr")
	}
	if strings.Contains(addr, "://") || strings.ContainsAny(addr, "/?#") {
		return rdbAnalysisRemoteFetchTarget{}, errors.Errorf("invalid rdb analysis remote fetch server_addr = %s", addr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.newContext()
	if err != nil {
		return rdbAnalysisRemoteFetchTarget{}, err
	}
	if _, _, err := ctx.getGroupByServer(addr); err != nil {
		return rdbAnalysisRemoteFetchTarget{}, errors.Errorf("rdb analysis remote fetch server %s is not in current product", addr)
	}
	return rdbAnalysisRemoteFetchTarget{serverAddr: addr}, nil
}

func (s *Topom) startRDBAnalysisRemoteFetch(ctx stdcontext.Context, serverAddr string, options RDBAnalysisOptions) (*RDBAnalysisJob, error) {
	if s == nil || s.rdbAnalysis == nil {
		return nil, errors.New("rdb analysis is not initialized")
	}
	target, err := s.verifyRDBAnalysisRemoteFetchServer(serverAddr)
	if err != nil {
		return nil, err
	}
	log.Warnf("rdb analysis remote fetch product=[%s] server-[%s] accepted", s.Config().ProductName, target.serverAddr)
	return s.rdbAnalysis.startRemoteFetch(ctx, target, options)
}

func (m *RDBAnalysisManager) acquireRemoteFetch() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remoteFetchActive >= m.remoteFetchMaxConcurrent {
		return errors.Errorf("too many running rdb analysis remote fetches")
	}
	m.remoteFetchActive++
	return nil
}

func (m *RDBAnalysisManager) releaseRemoteFetch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remoteFetchActive > 0 {
		m.remoteFetchActive--
	}
}

func (m *RDBAnalysisManager) fetchRemoteRDB(ctx stdcontext.Context, target rdbAnalysisRemoteFetchTarget) (*rdbAnalysisRemoteFetchFile, error) {
	serverAddr := target.serverAddr
	start := time.Now()
	if m.remoteFetchAuth == "" {
		err := errors.New("missing rdb_analysis_remote_fetch_auth")
		log.Warnf("rdb analysis remote fetch server-[%s] reject err=%s", serverAddr, err)
		return nil, err
	}
	if err := m.acquireRemoteFetch(); err != nil {
		log.Warnf("rdb analysis remote fetch server-[%s] reject err=%s", serverAddr, err)
		return nil, err
	}
	defer m.releaseRemoteFetch()
	log.Warnf("rdb analysis remote fetch server-[%s] start", serverAddr)

	u := &url.URL{
		Scheme: "http",
		Host:   serverAddr,
		Path:   rdbAnalysisRemoteFetchPath,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		err = errors.Trace(err)
		log.Warnf("rdb analysis remote fetch server-[%s] create request failed in %v err=%s", serverAddr, time.Since(start), err)
		return nil, err
	}
	req.Header.Set(rdbAnalysisRemoteFetchAuthHeader, m.remoteFetchAuth)

	rsp, err := m.newRemoteFetchHTTPClient().Do(req)
	if err != nil {
		err = errors.Trace(err)
		log.Warnf("rdb analysis remote fetch server-[%s] http failed in %v err=%s", serverAddr, time.Since(start), err)
		return nil, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusOK {
		err := errors.Errorf("redis rdb export returned %d %s", rsp.StatusCode, http.StatusText(rsp.StatusCode))
		log.Warnf("rdb analysis remote fetch server-[%s] status=%d content_length=%d failed in %v err=%s",
			serverAddr, rsp.StatusCode, rsp.ContentLength, time.Since(start), err)
		return nil, err
	}
	if rsp.ContentLength > m.maxUpload {
		err := errors.Errorf("rdb remote fetch exceeds max size %s", bytesize.Int64(m.maxUpload).HumanString())
		log.Warnf("rdb analysis remote fetch server-[%s] content_length=%d max_size=%d failed in %v err=%s",
			serverAddr, rsp.ContentLength, m.maxUpload, time.Since(start), err)
		return nil, err
	}

	if err := os.MkdirAll(filepath.Join(m.workspace, "remote"), 0755); err != nil {
		err = errors.Trace(err)
		log.Warnf("rdb analysis remote fetch server-[%s] mkdir failed in %v err=%s", serverAddr, time.Since(start), err)
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Join(m.workspace, "remote"), "rdb-remote-*.rdb")
	if err != nil {
		err = errors.Trace(err)
		log.Warnf("rdb analysis remote fetch server-[%s] create temp failed in %v err=%s", serverAddr, time.Since(start), err)
		return nil, err
	}
	path := tmp.Name()
	defer tmp.Close()

	limited := &io.LimitedReader{R: rsp.Body, N: m.maxUpload + 1}
	n, err := io.Copy(tmp, limited)
	if err != nil {
		os.Remove(path)
		err = errors.Trace(err)
		log.Warnf("rdb analysis remote fetch server-[%s] copy failed bytes=%d content_length=%d in %v err=%s",
			serverAddr, n, rsp.ContentLength, time.Since(start), err)
		return nil, err
	}
	if n > m.maxUpload {
		os.Remove(path)
		err := errors.Errorf("rdb remote fetch exceeds max size %s", bytesize.Int64(m.maxUpload).HumanString())
		log.Warnf("rdb analysis remote fetch server-[%s] copy exceeded max bytes=%d max_size=%d content_length=%d in %v err=%s",
			serverAddr, n, m.maxUpload, rsp.ContentLength, time.Since(start), err)
		return nil, err
	}
	source := "remote-http:" + serverAddr + "/" + rdbAnalysisRemoteFetchFilename(rsp.Header.Get("Content-Disposition"))
	log.Warnf("rdb analysis remote fetch server-[%s] downloaded bytes=%d content_length=%d source=%s in %v",
		serverAddr, n, rsp.ContentLength, source, time.Since(start))
	return &rdbAnalysisRemoteFetchFile{
		source: source,
		path:   path,
		size:   n,
	}, nil
}

func (m *RDBAnalysisManager) startRemoteFetch(ctx stdcontext.Context, target rdbAnalysisRemoteFetchTarget, options RDBAnalysisOptions) (*RDBAnalysisJob, error) {
	file, err := m.fetchRemoteRDB(ctx, target)
	if err != nil {
		return nil, err
	}
	job, err := m.startJob(file.source, file.path, file.size, true, options)
	if err != nil {
		os.Remove(file.path)
		log.Warnf("rdb analysis remote fetch server-[%s] create job failed source=%s size=%d err=%s",
			target.serverAddr, file.source, file.size, err)
		return nil, err
	}
	log.Warnf("rdb analysis remote fetch server-[%s] create job-[%s] source=%s size=%d",
		target.serverAddr, job.ID, file.source, file.size)
	return job, nil
}

func rdbAnalysisRemoteFetchFilename(disposition string) string {
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if name := strings.TrimSpace(filepath.Base(params["filename"])); name != "" && name != "." && name != string(filepath.Separator) {
				return name
			}
		}
	}
	return rdbAnalysisRemoteFetchDefaultFilename
}
