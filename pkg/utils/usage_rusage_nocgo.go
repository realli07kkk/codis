// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

//go:build !linux && !cgo

package utils

// Fallback used when cgo is disabled. USER_HZ is 100 on the supported
// platforms; the rusage fields are page counts on those platforms.
var rssUnit = 1024 * int64(100)
