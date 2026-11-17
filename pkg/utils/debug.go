// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"net"
	"net/http"
	"strings"
)

// RedactPathSecret replaces every occurrence of secret in a URL path with a
// fixed placeholder so credentials carried in path segments do not reach logs.
func RedactPathSecret(path, secret string) string {
	if secret == "" {
		return path
	}
	return strings.ReplaceAll(path, secret, ":xauth")
}

// ServeDebugHTTP serves the default mux (including net/http/pprof) but only
// for loopback clients. Debug endpoints expose goroutine stacks, heap profiles
// and CPU profiles, so they must not be reachable from the network by default.
func ServeDebugHTTP(w http.ResponseWriter, req *http.Request) {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		http.Error(w, "debug endpoints are only available from loopback", http.StatusForbidden)
		return
	}
	http.DefaultServeMux.ServeHTTP(w, req)
}
