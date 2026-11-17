// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

//go:build !cgo

package unsafe2

import "unsafe"

// Pure-Go fallback used when cgo is disabled. The returned memory is a normal
// Go allocation; cgoSlice keeps it reachable through its byte slice.
func cgo_malloc(n int) unsafe.Pointer {
	if n <= 0 {
		return nil
	}
	return unsafe.Pointer(unsafe.SliceData(make([]byte, n)))
}

func cgo_free(unsafe.Pointer) {
}
