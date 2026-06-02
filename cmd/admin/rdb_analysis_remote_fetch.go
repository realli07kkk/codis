// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"fmt"
	"strings"

	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

func (t *cmdDashboard) handleRDBAnalysisRemoteFetch(d map[string]interface{}) {
	c := t.newTopomClient()
	serverAddr := utils.ArgumentMust(d, "--server")
	options := parseRDBAnalysisRemoteFetchOptions(d)

	log.Debugf("call rpc rdb-analysis-remote-fetch to dashboard %s", t.addr)
	id, err := c.StartRDBAnalysisRemoteFetch(serverAddr, options)
	if err != nil {
		log.PanicErrorf(err, "call rpc rdb-analysis-remote-fetch to dashboard %s failed", t.addr)
	}
	log.Debugf("call rpc rdb-analysis-remote-fetch OK")
	fmt.Println(id)
}

func parseRDBAnalysisRemoteFetchOptions(d map[string]interface{}) topom.RDBAnalysisOptions {
	var options topom.RDBAnalysisOptions
	if n, ok := utils.ArgumentInteger(d, "--topn"); ok {
		options.TopN = n
	}
	if n, ok := utils.ArgumentInteger(d, "--max-depth"); ok {
		options.MaxDepth = n
	}
	if s, ok := utils.Argument(d, "--regex"); ok {
		options.Regex = s
	}
	if s, ok := utils.Argument(d, "--prefix-sep"); ok {
		options.PrefixSeparators = splitRDBAnalysisRemoteFetchSeparators(s)
	}
	if v, ok := d["--include-expired"].(bool); ok {
		options.IncludeExpired = v
	}
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
