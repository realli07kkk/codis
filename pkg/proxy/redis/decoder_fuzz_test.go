// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	"bytes"
	"testing"
)

func FuzzDecodeMultiBulk(f *testing.F) {
	f.Add([]byte("*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n"))
	f.Add([]byte("*1\r\n$3\r\nGET\r\n"))
	f.Add([]byte("-ERR unknown command\r\n"))
	f.Add([]byte("$536870911\r\n"))
	f.Add([]byte("*1048576\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			t.Skip()
		}
		d := NewDecoder(bytes.NewReader(data))
		_, _ = d.DecodeMultiBulk()
	})
}

func FuzzBtoi64(f *testing.F) {
	f.Add([]byte("0"))
	f.Add([]byte("-1"))
	f.Add([]byte("9223372036854775807"))
	f.Add([]byte("+42"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64 {
			t.Skip()
		}
		_, _ = Btoi64(data)
	})
}
