// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestHotKeyCacheInvalidateFansOutToOtherProxies(t *testing.T) {
	topom := openTopom()
	defer topom.Close()

	binaryKey := []byte{0xff, 'h', 'o', 't'}
	requests := make(chan proxy.HotKeyCacheInvalidationRequest, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req proxy.HotKeyCacheInvalidationRequest
		assert.MustNoError(json.NewDecoder(r.Body).Decode(&req))
		requests <- req
		_ = json.NewEncoder(w).Encode(&proxy.HotKeyCacheInvalidationResult{
			TotalKeys:   len(req.Keys),
			Invalidated: 1,
		})
	}))
	defer target.Close()

	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer failed.Close()

	targetURL, err := url.Parse(target.URL)
	assert.MustNoError(err)
	failedURL, err := url.Parse(failed.URL)
	assert.MustNoError(err)

	contextCreateProxy(topom, &models.Proxy{Token: "source", AdminAddr: "127.0.0.1:1"})
	contextCreateProxy(topom, &models.Proxy{Token: "target", AdminAddr: targetURL.Host})
	contextCreateProxy(topom, &models.Proxy{Token: "failed", AdminAddr: failedURL.Host})

	result, err := topom.HotKeyCacheInvalidate(&proxy.HotKeyCacheBroadcastRequest{
		SourceProxyToken: "source",
		Database:         2,
		Keys:             [][]byte{binaryKey},
		TimeoutMillis:    1000,
	})
	assert.MustNoError(err)
	if result.TotalProxies != 2 || result.FailedProxies != 1 || result.Invalidated != 1 {
		t.Fatalf("fan-out result = %+v", result)
	}

	select {
	case req := <-requests:
		if req.Database != 2 || len(req.Keys) != 1 || !bytes.Equal(req.Keys[0], binaryKey) {
			t.Fatalf("target request = %+v", req)
		}
	default:
		t.Fatal("target proxy did not receive invalidation")
	}
}

func TestNormalizeHotKeyCacheBroadcastTimeout(t *testing.T) {
	tests := []struct {
		input int64
		want  int64
	}{
		{0, int64(hotKeyCacheBroadcastDefaultTimeout / time.Millisecond)},
		{-1, int64(hotKeyCacheBroadcastDefaultTimeout / time.Millisecond)},
		{1, int64(hotKeyCacheBroadcastMinTimeout / time.Millisecond)},
		{100, 100},
		{5000, int64(hotKeyCacheBroadcastMaxTimeout / time.Millisecond)},
	}
	for _, tt := range tests {
		got := normalizeHotKeyCacheBroadcastTimeout(tt.input)
		if int64(got/time.Millisecond) != tt.want {
			t.Fatalf("timeout(%d) = %v, want %dms", tt.input, got, tt.want)
		}
	}
}
