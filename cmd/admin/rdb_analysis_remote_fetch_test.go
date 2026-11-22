// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestRDBAnalysisRemoteFetchOptions(t *testing.T) {
	cfg := &adminConfig{
		TopN:           cli.String{Value: "10", Provided: true},
		MaxDepth:       cli.String{Value: "4", Provided: true},
		Regex:          cli.String{Value: "^user:", Provided: true},
		PrefixSep:      cli.String{Value: ":,|", Provided: true},
		IncludeExpired: cli.Bool{Value: true, Provided: true},
	}
	options := parseRDBAnalysisRemoteFetchOptions(cfg)
	assert.Must(t, options.TopN == 10)
	assert.Must(t, options.MaxDepth == 4)
	assert.Must(t, options.Regex == "^user:")
	assert.Must(t, options.IncludeExpired)
	assert.Must(t, len(options.PrefixSeparators) == 2)
	assert.Must(t, options.PrefixSeparators[0] == ":")
	assert.Must(t, options.PrefixSeparators[1] == "|")
}

func TestRDBAnalysisRemoteFetchOptionsOmitAuth(t *testing.T) {
	cfg := &adminConfig{
		TopN: cli.String{Value: "5", Provided: true},
	}
	options := parseRDBAnalysisRemoteFetchOptions(cfg)
	assert.Must(t, options.TopN == 5)
	assert.Must(t, options.Regex == "")
	assert.Must(t, len(options.PrefixSeparators) == 0)
}
