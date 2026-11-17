// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

//go:build !linux && cgo

package utils

/*
#include <unistd.h>
*/
import "C"

var rssUnit = 1024 * int64(C.sysconf(C._SC_CLK_TCK))
