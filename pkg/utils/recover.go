// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"fmt"
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

// RecoverAsError is Recover for goroutines whose owner must react to the death
// of the task. It logs the panic with its stack and stores it into err, so the
// owner shuts the task down instead of leaving a half-dead worker that still
// looks healthy.
func RecoverAsError(scope string, err *error) {
	if x := recover(); x != nil {
		log.Errorf("[%s] panic recovered: %v\n%s", scope, x, debug.Stack())
		*err = fmt.Errorf("%s: panic: %v", scope, x)
	}
}
