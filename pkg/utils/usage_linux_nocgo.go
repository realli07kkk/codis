// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

//go:build linux && !cgo

package utils

import "time"

// Linux USER_HZ is 100 on the architectures Codis supports. Without cgo there
// is no portable sysconf(_SC_CLK_TCK) wrapper, so use the standard value.
var clockTicks = time.Second / 100
