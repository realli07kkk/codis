// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	stderrors "errors"
	"fmt"
	"net"
	"os"
	"testing"

	codiserrors "github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/unsafe2"
)

func benchmarkConn(b *testing.B, n int) {
	unsafe2.SetMaxOffheapBytes(0)
	for b.Loop() {
		c := NewConn(&net.TCPConn{}, n, n)
		c.Close()
	}
}

func benchmarkConnOffheap(b *testing.B, n int) {
	unsafe2.SetMaxOffheapBytes(1024 * 1024 * 512)
	for b.Loop() {
		c := NewConn(&net.TCPConn{}, n, n)
		c.Close()
	}
}

func BenchmarkConn16K(b *testing.B)  { benchmarkConn(b, 1024*16) }
func BenchmarkConn32K(b *testing.B)  { benchmarkConn(b, 1024*32) }
func BenchmarkConn64K(b *testing.B)  { benchmarkConn(b, 1024*64) }
func BenchmarkConn128K(b *testing.B) { benchmarkConn(b, 1024*128) }
func BenchmarkConn256K(b *testing.B) { benchmarkConn(b, 1024*256) }

func BenchmarkConnOffheap16K(b *testing.B)  { benchmarkConnOffheap(b, 1024*16) }
func BenchmarkConnOffheap32K(b *testing.B)  { benchmarkConnOffheap(b, 1024*32) }
func BenchmarkConnOffheap64K(b *testing.B)  { benchmarkConnOffheap(b, 1024*64) }
func BenchmarkConnOffheap128K(b *testing.B) { benchmarkConnOffheap(b, 1024*128) }
func BenchmarkConnOffheap256K(b *testing.B) { benchmarkConnOffheap(b, 1024*256) }

func TestIsTimeoutClassifiesWrappedNetErrors(t *testing.T) {
	timeout := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
	nonTimeout := &net.OpError{Op: "read", Net: "tcp", Err: stderrors.New("boom")}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain timeout", timeout, true},
		{"plain non-timeout", nonTimeout, false},
		{"traced timeout", codiserrors.Trace(timeout), true},
		{"multi-layer timeout", codiserrors.Trace(fmt.Errorf("redis: %w", fmt.Errorf("read: %w", timeout))), true},
		{"multi-layer non-timeout", codiserrors.Trace(fmt.Errorf("redis: %w", nonTimeout)), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsTimeout(c.err); got != c.want {
				t.Fatalf("IsTimeout(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
