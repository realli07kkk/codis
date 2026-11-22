// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net/http"
	"testing"
)

// The martini route table is now a http.ServeMux pattern table. These tables
// pin every documented admin route so a pattern typo cannot silently turn a
// route into a 404. A bad xauth makes each route answer 800 before any
// business logic runs, which keeps the test free of Redis / sentinel peers.

const badXAuth = "not-the-xauth"

type apiRoute struct {
	method string
	path   string
}

func proxyXAuthRoutes() []apiRoute {
	var paths = []string{
		"/api/proxy/xping/" + badXAuth,
		"/api/proxy/stats/" + badXAuth,
		"/api/proxy/stats/" + badXAuth + "/0",
		"/api/proxy/slots/" + badXAuth,
	}
	routes := make([]apiRoute, 0, 20)
	for _, p := range paths {
		routes = append(routes, apiRoute{"GET", p})
	}
	for _, p := range []string{
		"/api/proxy/start/" + badXAuth,
		"/api/proxy/stats/reset/" + badXAuth,
		"/api/proxy/forcegc/" + badXAuth,
		"/api/proxy/shutdown/" + badXAuth,
		"/api/proxy/loglevel/" + badXAuth + "/error",
		"/api/proxy/fillslots/" + badXAuth,
		"/api/proxy/sentinels/" + badXAuth,
		"/api/proxy/sentinels/" + badXAuth + "/rewatch",
		"/api/proxy/acl/" + badXAuth,
		"/api/proxy/qps-limit/" + badXAuth,
		"/api/proxy/hot-key-cache/invalidate/" + badXAuth,
	} {
		routes = append(routes, apiRoute{"PUT", p})
	}
	return routes
}

func TestProxyAPIRouteTable(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	for _, rt := range proxyXAuthRoutes() {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := serveAPIRequest(t, h, apiContractRequest{method: rt.method, path: rt.path})
			requireAPIStatus(t, rec, 800)
			requireAPIContains(t, rec.Body.String(), "invalid xauth")
		})
	}
}

// martini appended an optional "/" to every route, so each route also matches
// with exactly one trailing slash.
func TestProxyAPIRouteTableTrailingSlash(t *testing.T) {
	_, h, _ := newProxyContractServer(t)

	for _, rt := range proxyXAuthRoutes() {
		t.Run(rt.method+" "+rt.path+"/", func(t *testing.T) {
			rec := serveAPIRequest(t, h, apiContractRequest{method: rt.method, path: rt.path + "/"})
			requireAPIStatus(t, rec, 800)
			requireAPIContains(t, rec.Body.String(), "invalid xauth")
		})
	}
}

// ServeMux differences that must not leak through the router: HEAD is served
// by GET routes only, non-GET routes answer 404 for HEAD, ServeMux redirects
// and its 405 answer are replaced by the martini 404.
func TestProxyAPIServeMuxAdaptations(t *testing.T) {
	_, h, xauth := newProxyContractServer(t)

	t.Run("head on get route", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "HEAD", path: "/api/proxy/model"})
		requireAPIStatus(t, rec, http.StatusOK)
	})

	t.Run("head on put route", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "HEAD", path: "/api/proxy/forcegc/" + xauth})
		requireAPIStatus(t, rec, http.StatusNotFound)
	})

	t.Run("options on get route", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{method: "OPTIONS", path: "/api/proxy/model"})
		requireAPIStatus(t, rec, http.StatusNotFound)
	})

	t.Run("unclean path is not redirected", func(t *testing.T) {
		for _, path := range []string{
			"//api/proxy/model",
			"/api//proxy/model",
			"/api/proxy/model//",
			"/api/proxy/./model",
		} {
			rec := serveAPIRequest(t, h, apiContractRequest{method: "GET", path: path})
			requireAPIStatus(t, rec, http.StatusNotFound)
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Fatalf("%s: unexpected redirect to %q", path, loc)
			}
		}
	})

	t.Run("debug prefix is 404 for every method", func(t *testing.T) {
		for _, method := range []string{"GET", "POST", "PUT"} {
			rec := serveAPIRequest(t, h, apiContractRequest{method: method, path: "/debug"})
			requireAPIStatus(t, rec, http.StatusNotFound)
			requireAPIContains(t, rec.Body.String(), "404 page not found")
		}
	})

	t.Run("debug wildcard root is 404", func(t *testing.T) {
		rec := serveAPIRequest(t, h, apiContractRequest{
			method: "GET", path: "/debug/", remoteAddr: "127.0.0.1:45678",
		})
		requireAPIStatus(t, rec, http.StatusNotFound)
	})
}
