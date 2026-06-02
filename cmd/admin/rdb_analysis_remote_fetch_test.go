// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestRDBAnalysisRemoteFetchOptions(t *testing.T) {
	options := parseRDBAnalysisRemoteFetchOptions(map[string]interface{}{
		"--topn":            "10",
		"--max-depth":       "4",
		"--regex":           "^user:",
		"--prefix-sep":      ":,|",
		"--include-expired": true,
	})
	assert.Must(options.TopN == 10)
	assert.Must(options.MaxDepth == 4)
	assert.Must(options.Regex == "^user:")
	assert.Must(options.IncludeExpired)
	assert.Must(len(options.PrefixSeparators) == 2)
	assert.Must(options.PrefixSeparators[0] == ":")
	assert.Must(options.PrefixSeparators[1] == "|")
}

func TestRDBAnalysisRemoteFetchOptionsOmitAuth(t *testing.T) {
	options := parseRDBAnalysisRemoteFetchOptions(map[string]interface{}{
		"--topn": "5",
	})
	assert.Must(options.TopN == 5)
	assert.Must(options.Regex == "")
	assert.Must(len(options.PrefixSeparators) == 0)
}
