// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"context"
	"net/http"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/log"
)

// NewAdminHTTPServer returns an http.Server with conservative timeouts for the
// Codis admin APIs. ReadHeaderTimeout and IdleTimeout close the slow-header and
// idle-connection holes without imposing a body deadline, because the admin API
// accepts large RDB uploads and long-running analysis responses.
func NewAdminHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// ShutdownHTTPServer drains in-flight admin requests before the listener is
// torn down. Errors are logged because shutdown must not block process exit.
func ShutdownHTTPServer(hs *http.Server, timeout time.Duration) {
	if hs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		log.WarnErrorf(err, "http admin server shutdown failed")
	}
}
