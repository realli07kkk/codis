// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"fmt"
	"strings"

	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

func (t *cmdDashboard) handleRDBAnalysisRemoteFetch(cfg *adminConfig) {
	c := t.newTopomClient()
	serverAddr := argumentMust(cfg.Server, "--server")
	options := parseRDBAnalysisRemoteFetchOptions(cfg)

	log.Debugf("call rpc rdb-analysis-remote-fetch to dashboard %s", t.addr)
	id, err := c.StartRDBAnalysisRemoteFetch(serverAddr, options)
	if err != nil {
		log.PanicErrorf(err, "call rpc rdb-analysis-remote-fetch to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc rdb-analysis-remote-fetch OK")
	fmt.Println(id)
}

func parseRDBAnalysisRemoteFetchOptions(cfg *adminConfig) topom.RDBAnalysisOptions {
	var options topom.RDBAnalysisOptions
	if n, ok := argumentInteger(cfg.TopN, "--topn"); ok {
		options.TopN = n
	}
	if n, ok := argumentInteger(cfg.MaxDepth, "--max-depth"); ok {
		options.MaxDepth = n
	}
	if cfg.Regex.Provided {
		options.Regex = cfg.Regex.Value
	}
	if cfg.PrefixSep.Provided {
		options.PrefixSeparators = splitRDBAnalysisRemoteFetchSeparators(cfg.PrefixSep.Value)
	}
	options.IncludeExpired = cfg.IncludeExpired.Value
	return options
}

func splitRDBAnalysisRemoteFetchSeparators(s string) []string {
	var separators []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			separators = append(separators, part)
		}
	}
	return separators
}
