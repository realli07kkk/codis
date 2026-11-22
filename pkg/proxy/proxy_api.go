// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net/http"
	"runtime"
	"strconv"
	"time"

	_ "net/http/pprof"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/httpx"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/rpc"
)

type apiServer struct {
	proxy *Proxy
}

// newApiServer builds the admin HTTP handler from Go 1.22+ http.ServeMux
// method/path patterns. Handlers are plain http.HandlerFuncs: path parameters
// come from Request.PathValue and responses use http.ResponseWriter.
func newApiServer(p *Proxy) http.Handler {
	api := &apiServer{proxy: p}

	r := httpx.NewRouter()
	r.Get(httpx.Root, api.RedirectToOverview)
	// martini's "/debug/**" did not match "/debug", so keep the 404 instead of
	// letting ServeMux redirect "/debug" to "/debug/".
	r.NotFound("/debug")
	r.Any("/debug/{rest...}", utils.ServeDebugHTTP)

	r.Get("/proxy", api.Overview)
	r.Get("/proxy/model", api.Model)
	r.Get("/proxy/stats", api.StatsNoXAuth)
	r.Get("/proxy/slots", api.SlotsNoXAuth)

	r.Get("/api/proxy/model", api.Model)
	r.Get("/api/proxy/xping/{xauth}", api.XPing)
	r.Get("/api/proxy/stats/{xauth}", api.Stats)
	r.Get("/api/proxy/stats/{xauth}/{flags}", api.Stats)
	r.Get("/api/proxy/slots/{xauth}", api.Slots)
	r.Put("/api/proxy/start/{xauth}", api.Start)
	r.Put("/api/proxy/stats/reset/{xauth}", api.ResetStats)
	r.Put("/api/proxy/forcegc/{xauth}", api.ForceGC)
	r.Put("/api/proxy/shutdown/{xauth}", api.Shutdown)
	r.Put("/api/proxy/loglevel/{xauth}/{value}", api.LogLevel)
	r.Put("/api/proxy/fillslots/{xauth}", api.FillSlots)
	r.Put("/api/proxy/sentinels/{xauth}", api.SetSentinels)
	r.Put("/api/proxy/sentinels/{xauth}/rewatch", api.RewatchSentinels)
	r.Put("/api/proxy/acl/{xauth}", api.SetACL)
	r.Put("/api/proxy/qps-limit/{xauth}", api.SetQPSLimit)
	r.Put("/api/proxy/hot-key-cache/invalidate/{xauth}", api.HotKeyCacheInvalidate)

	return httpx.Chain(r,
		httpx.Recovery(),
		httpx.AuditLog(p, p.XAuth),
		httpx.Gzip(),
		httpx.ContentType("application/json; charset=utf-8"),
	)
}

func (s *apiServer) verifyXAuth(req *http.Request) error {
	if s.proxy.IsClosed() {
		return ErrClosedProxy
	}
	xauth := req.PathValue("xauth")
	if xauth == "" {
		return errors.New("missing xauth, please check product name & auth")
	}
	if !rpc.ConstantTimeCompareString(xauth, s.proxy.XAuth()) {
		return errors.New("invalid xauth, please check product name & auth")
	}
	return nil
}

// writeError writes the legacy 800 response used by the codis admin API.
func (s *apiServer) writeError(w http.ResponseWriter, err error) {
	status, body := rpc.ApiResponseError(err)
	httpx.WriteResponse(w, status, body)
}

func (s *apiServer) writeJSON(w http.ResponseWriter, v any) {
	status, body := rpc.ApiResponseJson(v)
	httpx.WriteResponse(w, status, body)
}

func (s *apiServer) RedirectToOverview(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, "/proxy", http.StatusFound)
}

func (s *apiServer) Overview(w http.ResponseWriter, req *http.Request) {
	s.writeJSON(w, s.proxy.Overview(StatsFull))
}

func (s *apiServer) Model(w http.ResponseWriter, req *http.Request) {
	s.writeJSON(w, s.proxy.Model())
}

func (s *apiServer) StatsNoXAuth(w http.ResponseWriter, req *http.Request) {
	s.writeJSON(w, s.proxy.Stats(StatsFull))
}

func (s *apiServer) SlotsNoXAuth(w http.ResponseWriter, req *http.Request) {
	s.writeJSON(w, s.proxy.Slots())
}

func (s *apiServer) XPing(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) Stats(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	var flags StatsFlags
	if v := req.PathValue("flags"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			s.writeError(w, err)
			return
		}
		flags = StatsFlags(n)
	}
	s.writeJSON(w, s.proxy.Stats(flags))
}

func (s *apiServer) Slots(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, s.proxy.Slots())
}

func (s *apiServer) Start(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.proxy.Start(); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) ResetStats(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	s.proxy.ResetStats()
	s.writeJSON(w, "OK")
}

func (s *apiServer) ForceGC(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	runtime.GC()
	s.writeJSON(w, "OK")
}

func (s *apiServer) LogLevel(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	v := req.PathValue("value")
	if v == "" {
		s.writeError(w, errors.New("missing loglevel"))
		return
	}
	if !log.SetLevelString(v) {
		s.writeError(w, errors.New("invalid loglevel"))
		return
	}
	log.Warnf("set loglevel to %s", v)
	s.writeJSON(w, "OK")
}

func (s *apiServer) Shutdown(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.proxy.Close(); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) FillSlots(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	var slots []*models.Slot
	httpx.BindJSON(req, &slots)
	if err := s.proxy.FillSlots(slots); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) SetSentinels(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	var sentinel models.Sentinel
	httpx.BindJSON(req, &sentinel)
	if err := s.proxy.SetSentinels(sentinel.Servers); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) RewatchSentinels(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	if err := s.proxy.RewatchSentinels(); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) SetACL(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	var acl models.ACL
	httpx.BindJSON(req, &acl)
	if err := s.proxy.SetACL(&acl); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) SetQPSLimit(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	var config QPSLimitConfig
	httpx.BindJSON(req, &config)
	if err := s.proxy.SetQPSLimit(&config); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, "OK")
}

func (s *apiServer) HotKeyCacheInvalidate(w http.ResponseWriter, req *http.Request) {
	if err := s.verifyXAuth(req); err != nil {
		s.writeError(w, err)
		return
	}
	var request HotKeyCacheInvalidationRequest
	httpx.BindJSON(req, &request)
	if len(request.Keys) > HotKeyCacheBroadcastMaxKeys {
		s.writeError(w, errors.Errorf("too many hot key cache invalidation keys: %d", len(request.Keys)))
		return
	}
	n, err := s.proxy.InvalidateHotKeyCache(request.Database, request.Keys)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, &HotKeyCacheInvalidationResult{
		TotalKeys:   len(request.Keys),
		Invalidated: n,
	})
}

type ApiClient struct {
	addr    string
	xauth   string
	timeout time.Duration
}

func NewApiClient(addr string) *ApiClient {
	return &ApiClient{addr: addr}
}

func (c *ApiClient) SetXAuth(name, auth string, token string) {
	c.xauth = rpc.NewXAuth(name, auth, token)
}

func (c *ApiClient) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

func (c *ApiClient) encodeURL(format string, args ...any) string {
	return rpc.EncodeURL(c.addr, format, args...)
}

func (c *ApiClient) Overview() (*Overview, error) {
	url := c.encodeURL("/proxy")
	var o = &Overview{}
	if err := rpc.ApiGetJson(url, o); err != nil {
		return nil, err
	}
	return o, nil
}

func (c *ApiClient) Model() (*models.Proxy, error) {
	url := c.encodeURL("/api/proxy/model")
	model := &models.Proxy{}
	if err := rpc.ApiGetJson(url, model); err != nil {
		return nil, err
	}
	return model, nil
}

func (c *ApiClient) XPing() error {
	url := c.encodeURL("/api/proxy/xping/%s", c.xauth)
	return rpc.ApiGetJson(url, nil)
}

func (c *ApiClient) StatsSimple() (*Stats, error) {
	url := c.encodeURL("/api/proxy/stats/%s", c.xauth)
	stats := &Stats{}
	if err := rpc.ApiGetJson(url, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

func (c *ApiClient) Stats(flags StatsFlags) (*Stats, error) {
	url := c.encodeURL("/api/proxy/stats/%s/%d", c.xauth, flags)
	stats := &Stats{}
	if err := rpc.ApiGetJson(url, stats); err != nil {
		return nil, err
	}
	return stats, nil
}

func (c *ApiClient) Slots() ([]*models.Slot, error) {
	url := c.encodeURL("/api/proxy/slots/%s", c.xauth)
	slots := []*models.Slot{}
	if err := rpc.ApiGetJson(url, &slots); err != nil {
		return nil, err
	}
	return slots, nil
}

func (c *ApiClient) ResetStats() error {
	url := c.encodeURL("/api/proxy/stats/reset/%s", c.xauth)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) ForceGC() error {
	url := c.encodeURL("/api/proxy/forcegc/%s", c.xauth)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) Start() error {
	url := c.encodeURL("/api/proxy/start/%s", c.xauth)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) LogLevel(level log.LogLevel) error {
	url := c.encodeURL("/api/proxy/loglevel/%s/%s", c.xauth, level)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) Shutdown() error {
	url := c.encodeURL("/api/proxy/shutdown/%s", c.xauth)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) FillSlots(slots ...*models.Slot) error {
	url := c.encodeURL("/api/proxy/fillslots/%s", c.xauth)
	return rpc.ApiPutJson(url, slots, nil)
}

func (c *ApiClient) SetSentinels(sentinel *models.Sentinel) error {
	url := c.encodeURL("/api/proxy/sentinels/%s", c.xauth)
	return rpc.ApiPutJson(url, sentinel, nil)
}

func (c *ApiClient) RewatchSentinels() error {
	url := c.encodeURL("/api/proxy/sentinels/%s/rewatch", c.xauth)
	return rpc.ApiPutJson(url, nil, nil)
}

func (c *ApiClient) SetACL(acl *models.ACL) error {
	url := c.encodeURL("/api/proxy/acl/%s", c.xauth)
	return rpc.ApiPutJson(url, acl, nil)
}

func (c *ApiClient) SetQPSLimit(revision, limit int64) error {
	url := c.encodeURL("/api/proxy/qps-limit/%s", c.xauth)
	return rpc.ApiPutJson(url, &QPSLimitConfig{Revision: revision, Limit: limit}, nil)
}

func (c *ApiClient) InvalidateHotKeyCache(req *HotKeyCacheInvalidationRequest) (*HotKeyCacheInvalidationResult, error) {
	url := c.encodeURL("/api/proxy/hot-key-cache/invalidate/%s", c.xauth)
	result := &HotKeyCacheInvalidationResult{}
	if err := rpc.ApiPutJsonWithTimeout(url, req, result, c.timeout); err != nil {
		return nil, err
	}
	return result, nil
}
