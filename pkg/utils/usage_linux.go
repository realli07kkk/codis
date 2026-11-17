// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

//go:build linux

package utils

/*
#include <unistd.h>
*/
import "C"

import "time"

var clockTicks = time.Second / time.Duration(C.sysconf(C._SC_CLK_TCK))
