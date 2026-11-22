// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"net/http"
	"testing"
)

// The martini route table is now a http.ServeMux pattern table. These tables
// pin every documented xauth-guarded admin route so a pattern typo cannot
// silently turn a route into a 404. A bad xauth makes each route answer 800
// before any business logic runs, which keeps the test free of Redis / proxy /
// sentinel peers.

const routeTableBadXAuth = "not-the-xauth"
const routeTableAddr = "127.0.0.1:6379"

type topomAPIRoute struct {
	method string
	path   string
}

func topomXAuthRoutes() []topomAPIRoute {
	var x = routeTableBadXAuth
	routes := []topomAPIRoute{
		{"GET", "/api/topom/xping/" + x},
		{"GET", "/api/topom/stats/" + x},
		{"GET", "/api/topom/slots/" + x},
		{"GET", "/api/topom/rdb-analysis/" + x + "/job"},
		{"GET", "/api/topom/pitr/jobs/" + x},
		{"GET", "/api/topom/pitr/" + x + "/job"},
		{"GET", "/api/topom/acl/" + x},
		{"GET", "/api/topom/proxy/qps-limit/" + x},
		{"POST", "/api/topom/rdb-analysis/upload/" + x},

		{"PUT", "/api/topom/reload/" + x},
		{"PUT", "/api/topom/shutdown/" + x},
		{"PUT", "/api/topom/loglevel/" + x + "/error"},

		{"PUT", "/api/topom/rdb-analysis/start/" + x},
		{"PUT", "/api/topom/rdb-analysis/remote-fetch/" + x},
		{"PUT", "/api/topom/rdb-analysis/cancel/" + x + "/job"},
		{"PUT", "/api/topom/rdb-analysis/remove/" + x + "/job"},

		{"PUT", "/api/topom/pitr/create/" + x},
		{"PUT", "/api/topom/pitr/cancel/" + x + "/job"},
		{"PUT", "/api/topom/pitr/remove/" + x + "/job"},

		{"PUT", "/api/topom/acl/" + x},
		{"PUT", "/api/topom/acl/sync/" + x},
		{"PUT", "/api/topom/proxy/qps-limit/" + x},
		{"PUT", "/api/topom/hot-key-cache/invalidate/" + x},

		{"PUT", "/api/topom/proxy/create/" + x + "/" + routeTableAddr},
		{"PUT", "/api/topom/proxy/online/" + x + "/" + routeTableAddr},
		{"PUT", "/api/topom/proxy/reinit/" + x + "/token"},
		{"PUT", "/api/topom/proxy/remove/" + x + "/token/1"},

		{"PUT", "/api/topom/group/create/" + x + "/1"},
		{"PUT", "/api/topom/group/remove/" + x + "/1"},
		{"PUT", "/api/topom/group/resync/" + x + "/1"},
		{"PUT", "/api/topom/group/resync-all/" + x},
		{"PUT", "/api/topom/group/add/" + x + "/1/" + routeTableAddr},
		{"PUT", "/api/topom/group/add/" + x + "/1/" + routeTableAddr + "/dc1"},
		{"PUT", "/api/topom/group/del/" + x + "/1/" + routeTableAddr},
		{"PUT", "/api/topom/group/promote/" + x + "/1/" + routeTableAddr},
		{"PUT", "/api/topom/group/replica-groups/" + x + "/1/" + routeTableAddr + "/1"},
		{"PUT", "/api/topom/group/replica-groups-all/" + x + "/1"},
		{"PUT", "/api/topom/group/action/create/" + x + "/" + routeTableAddr},
		{"PUT", "/api/topom/group/action/remove/" + x + "/" + routeTableAddr},

		{"PUT", "/api/topom/slots/action/create/" + x + "/1/1"},
		{"PUT", "/api/topom/slots/action/create-some/" + x + "/1/2/3"},
		{"PUT", "/api/topom/slots/action/create-range/" + x + "/0/10/1"},
		{"PUT", "/api/topom/slots/action/remove/" + x + "/1"},
		{"PUT", "/api/topom/slots/action/interval/" + x + "/1000"},
		{"PUT", "/api/topom/slots/action/disabled/" + x + "/1"},
		{"PUT", "/api/topom/slots/assign/" + x},
		{"PUT", "/api/topom/slots/assign/" + x + "/offline"},
		{"PUT", "/api/topom/slots/rebalance/" + x + "/0"},

		{"PUT", "/api/topom/sentinels/add/" + x + "/" + routeTableAddr},
		{"PUT", "/api/topom/sentinels/del/" + x + "/" + routeTableAddr + "/1"},
		{"PUT", "/api/topom/sentinels/resync-all/" + x},
	}
	return routes
}

func TestTopomAPIRouteTable(t *testing.T) {
	_, h, _ := newTopomContractServer(t)

	for _, rt := range topomXAuthRoutes() {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: rt.method, path: rt.path})
			requireTopomAPIStatus(t, rec, 800)
			requireTopomAPIContains(t, rec.Body.String(), "invalid xauth")
		})
	}
}

// martini appended an optional "/" to every route, so each route also matches
// with exactly one trailing slash.
func TestTopomAPIRouteTableTrailingSlash(t *testing.T) {
	_, h, _ := newTopomContractServer(t)

	for _, rt := range topomXAuthRoutes() {
		t.Run(rt.method+" "+rt.path+"/", func(t *testing.T) {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: rt.method, path: rt.path + "/"})
			requireTopomAPIStatus(t, rec, 800)
			requireTopomAPIContains(t, rec.Body.String(), "invalid xauth")
		})
	}
}

// ServeMux differences that must not leak through the router: HEAD is served
// by GET routes only, non-GET routes answer 404 for HEAD, ServeMux redirects
// and its 405 answer are replaced by the martini 404.
func TestTopomAPIServeMuxAdaptations(t *testing.T) {
	_, h, xauth := newTopomContractServer(t)

	t.Run("head on get route", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "HEAD", path: "/api/topom/model"})
		requireTopomAPIStatus(t, rec, http.StatusOK)
	})

	t.Run("head on put route", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "HEAD", path: "/api/topom/reload/" + xauth})
		requireTopomAPIStatus(t, rec, http.StatusNotFound)
	})

	t.Run("options on get route", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "OPTIONS", path: "/api/topom/model"})
		requireTopomAPIStatus(t, rec, http.StatusNotFound)
	})

	t.Run("unclean path is not redirected", func(t *testing.T) {
		for _, path := range []string{
			"//api/topom/model",
			"/api//topom/model",
			"/api/topom/model//",
			"/api/topom/./model",
		} {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: "GET", path: path})
			requireTopomAPIStatus(t, rec, http.StatusNotFound)
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Fatalf("%s: unexpected redirect to %q", path, loc)
			}
		}
	})

	t.Run("debug prefix is 404 for every method", func(t *testing.T) {
		for _, method := range []string{"GET", "POST", "PUT"} {
			rec := serveTopomAPIRequest(t, h, topomAPIRequest{method: method, path: "/debug"})
			requireTopomAPIStatus(t, rec, http.StatusNotFound)
			requireTopomAPIContains(t, rec.Body.String(), "404 page not found")
		}
	})

	t.Run("debug wildcard root is 404", func(t *testing.T) {
		rec := serveTopomAPIRequest(t, h, topomAPIRequest{
			method: "GET", path: "/debug/", remoteAddr: "127.0.0.1:45678",
		})
		requireTopomAPIStatus(t, rec, http.StatusNotFound)
	})
}
