// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestProxyQPSLimitDefaultView(x *testing.T) {
	t := openTopom()
	defer t.Close()

	view, err := t.GetProxyQPSLimit()
	assert.MustNoError(err)
	assert.Must(view.Revision == 0)
	assert.Must(view.Limit == 0)
	assert.Must(!view.Enabled)
	assert.Must(view.SyncStatus == "not_configured")
}

func TestUpdateProxyQPSLimitStoresRevision(x *testing.T) {
	t := openTopom()
	defer t.Close()

	view, err := t.UpdateProxyQPSLimit(&ProxyQPSLimitUpdateRequest{Limit: 100})
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.Limit == 100)
	assert.Must(view.Enabled)
	assert.Must(view.SyncStatus == "ready")

	config, err := t.store.LoadProxyQPSLimit(true)
	assert.MustNoError(err)
	assert.Must(config.Revision == 1)
	assert.Must(config.Limit == 100)
	assert.Must(config.UpdatedAt != "")

	view, err = t.UpdateProxyQPSLimit(&ProxyQPSLimitUpdateRequest{Limit: 0})
	assert.MustNoError(err)
	assert.Must(view.Revision == 2)
	assert.Must(view.Limit == 0)
	assert.Must(!view.Enabled)
	assert.Must(view.SyncStatus == "ready")

	view, err = t.GetProxyQPSLimit()
	assert.MustNoError(err)
	assert.Must(view.Revision == 2)
	assert.Must(view.Limit == 0)
	assert.Must(view.SyncStatus == "ready")
}

func TestUpdateProxyQPSLimitRejectsNegative(x *testing.T) {
	t := openTopom()
	defer t.Close()

	_, err := t.UpdateProxyQPSLimit(&ProxyQPSLimitUpdateRequest{Limit: -1})
	assert.Must(err != nil)

	config, err := t.store.LoadProxyQPSLimit(false)
	assert.MustNoError(err)
	assert.Must(config == nil)
}

func TestApiProxyQPSLimit(x *testing.T) {
	t := openTopom()
	defer t.Close()

	c := newApiClient(t)

	view, err := c.GetProxyQPSLimit()
	assert.MustNoError(err)
	assert.Must(view.Revision == 0)
	assert.Must(view.Limit == 0)
	assert.Must(view.SyncStatus == "not_configured")

	view, err = c.SetProxyQPSLimit(123)
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.Limit == 123)
	assert.Must(view.Enabled)
	assert.Must(view.SyncStatus == "ready")

	view, err = c.GetProxyQPSLimit()
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.Limit == 123)

	_, err = c.SetProxyQPSLimit(-1)
	assert.Must(err != nil)
}

func TestUpdateProxyQPSLimitProxySyncFailureRecordsAllFailedTokens(x *testing.T) {
	topom := openTopom()
	defer topom.Close()

	var applied proxy.QPSLimitConfig
	received := make(chan *proxy.QPSLimitConfig, 1)
	okProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if strings.Contains(r.URL.Path, "/qps-limit/") {
			config := &proxy.QPSLimitConfig{}
			assert.MustNoError(json.NewDecoder(r.Body).Decode(config))
			applied = *config
			received <- config
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/stats/") {
			qpsLimit := &proxy.QPSLimitStats{
				Revision: applied.Revision,
				Limit:    applied.Limit,
				Enabled:  applied.Limit > 0,
			}
			assert.MustNoError(json.NewEncoder(w).Encode(&proxy.Stats{QPSLimit: qpsLimit}))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer okProxy.Close()

	failedProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer failedProxy.Close()

	failedProxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer failedProxy2.Close()

	okURL, err := url.Parse(okProxy.URL)
	assert.MustNoError(err)
	failedURL, err := url.Parse(failedProxy.URL)
	assert.MustNoError(err)
	failedURL2, err := url.Parse(failedProxy2.URL)
	assert.MustNoError(err)

	contextCreateProxy(topom, &models.Proxy{Token: "a_failed", AdminAddr: failedURL.Host})
	contextCreateProxy(topom, &models.Proxy{Token: "b_ok", AdminAddr: okURL.Host})
	contextCreateProxy(topom, &models.Proxy{Token: "c_failed", AdminAddr: failedURL2.Host})

	_, err = topom.UpdateProxyQPSLimit(&ProxyQPSLimitUpdateRequest{Limit: 77})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "a_failed"))
	assert.Must(strings.Contains(err.Error(), "c_failed"))

	select {
	case config := <-received:
		assert.Must(config.Revision == 1)
		assert.Must(config.Limit == 77)
	default:
		x.Fatal("successful proxy did not receive qps limit sync")
	}

	config, err := topom.store.LoadProxyQPSLimit(true)
	assert.MustNoError(err)
	assert.Must(config.Revision == 1)
	assert.Must(config.Limit == 77)

	view, err := topom.GetProxyQPSLimit()
	assert.MustNoError(err)
	assert.Must(view.SyncStatus == "proxy_sync_failed:a_failed,c_failed")
}

func TestProxyQPSLimitStatusAfterTopomRestartDoesNotAssumeReady(x *testing.T) {
	client := newDiskClient()

	unappliedProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if strings.Contains(r.URL.Path, "/stats/") {
			assert.MustNoError(json.NewEncoder(w).Encode(&proxy.Stats{}))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer unappliedProxy.Close()

	u, err := url.Parse(unappliedProxy.URL)
	assert.MustNoError(err)

	t1, err := New(newForkClient(client), config)
	assert.MustNoError(err)
	assert.MustNoError(t1.Start(false))
	contextCreateProxy(t1, &models.Proxy{Token: "unapplied", AdminAddr: u.Host})
	assert.MustNoError(t1.storeUpdateProxyQPSLimit(&models.ProxyQPSLimit{
		Revision:  1,
		Limit:     88,
		UpdatedAt: "test",
	}))
	assert.MustNoError(t1.Close())

	t2, err := New(newForkClient(client), config)
	assert.MustNoError(err)
	assert.MustNoError(t2.Start(false))
	defer t2.Close()

	view, err := t2.GetProxyQPSLimit()
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.Limit == 88)
	assert.Must(view.SyncStatus == "proxy_sync_failed:unapplied")
}

func TestGetProxyQPSLimitStatsProbeDoesNotHoldTopomLock(x *testing.T) {
	t := openTopom()
	defer t.Close()

	statsStarted := make(chan struct{}, 1)
	releaseStats := make(chan struct{})
	slowProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if strings.Contains(r.URL.Path, "/stats/") {
			statsStarted <- struct{}{}
			<-releaseStats
			assert.MustNoError(json.NewEncoder(w).Encode(&proxy.Stats{}))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer slowProxy.Close()

	u, err := url.Parse(slowProxy.URL)
	assert.MustNoError(err)
	contextCreateProxy(t, &models.Proxy{Token: "slow", AdminAddr: u.Host})
	assert.MustNoError(t.storeUpdateProxyQPSLimit(&models.ProxyQPSLimit{
		Revision:  1,
		Limit:     99,
		UpdatedAt: "test",
	}))
	t.dirtyProxyQPSLimitCache()

	getDone := make(chan struct{})
	go func() {
		_, _ = t.GetProxyQPSLimit()
		close(getDone)
	}()

	select {
	case <-statsStarted:
	case <-time.After(time.Second):
		x.Fatal("GetProxyQPSLimit did not start stats probe")
	}

	lightOpDone := make(chan struct{})
	go func() {
		_, _ = t.Stats()
		close(lightOpDone)
	}()
	select {
	case <-lightOpDone:
	case <-time.After(100 * time.Millisecond):
		close(releaseStats)
		x.Fatal("GetProxyQPSLimit held topom lock while probing proxy stats")
	}

	close(releaseStats)
	select {
	case <-getDone:
	case <-time.After(time.Second):
		x.Fatal("GetProxyQPSLimit did not finish")
	}
}

func TestReinitProxyReplaysProxyQPSLimit(x *testing.T) {
	t := openTopom()
	defer t.Close()

	p, c := openProxy()
	defer c.Shutdown()

	view, err := t.UpdateProxyQPSLimit(&ProxyQPSLimitUpdateRequest{Limit: 321})
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.Limit == 321)

	assert.MustNoError(t.CreateProxy(p.AdminAddr))

	stats, err := c.StatsSimple()
	assert.MustNoError(err)
	assert.Must(stats.QPSLimit != nil)
	assert.Must(stats.QPSLimit.Revision == 1)
	assert.Must(stats.QPSLimit.Limit == 321)
	assert.Must(stats.QPSLimit.Enabled)

	view, err = t.UpdateProxyQPSLimit(&ProxyQPSLimitUpdateRequest{Limit: 0})
	assert.MustNoError(err)
	assert.Must(view.Revision == 2)
	assert.Must(view.Limit == 0)

	stats, err = c.StatsSimple()
	assert.MustNoError(err)
	assert.Must(stats.QPSLimit != nil)
	assert.Must(stats.QPSLimit.Revision == 2)
	assert.Must(stats.QPSLimit.Limit == 0)
	assert.Must(!stats.QPSLimit.Enabled)
}
