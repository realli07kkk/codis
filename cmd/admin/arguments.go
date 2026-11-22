// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"strconv"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// argumentMust returns a required typed option value, preserving the
// historical panic when a required option is missing.
func argumentMust(arg cli.String, name string) string {
	if !arg.Provided {
		log.Panicf("option %s is required", name)
	}
	return arg.Value
}

func argumentInteger(arg cli.String, name string) (int, bool) {
	if !arg.Provided {
		return 0, false
	}
	n, err := strconv.Atoi(arg.Value)
	if err != nil {
		log.PanicErrorf(err, "option %s isn't a valid integer", name)
	}
	return n, true
}

func argumentIntegerMust(arg cli.String, name string) int {
	n, ok := argumentInteger(arg, name)
	if ok {
		return n
	}
	log.Panicf("option %s is required", name)
	return 0
}
