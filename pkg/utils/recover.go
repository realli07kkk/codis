// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"runtime/debug"

	"github.com/CodisLabs/codis/pkg/utils/log"
)

// Recover logs a recovered panic with the goroutine stack. It is meant to be
// used as a deferred call at the root of long-lived goroutines so one bad
// request cannot take down the whole process.
func Recover(scope string) {
	if x := recover(); x != nil {
		log.Errorf("[%s] panic recovered: %v\n%s", scope, x, debug.Stack())
	}
}
