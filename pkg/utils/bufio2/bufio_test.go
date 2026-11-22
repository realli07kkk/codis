// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package bufio2

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func newReader(n int, input string) *Reader {
	return &Reader{rd: strings.NewReader(input), buf: make([]byte, n)}
}

func TestRead(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "hello world %d", i)
	}
	var input = b.String()
	for n := 1; n < len(input); n++ {
		r := newReader(n, input)
		b := make([]byte, len(input))
		_, err := io.ReadFull(r, b)
		assert.MustNoError(t, err)
		assert.Must(t, string(b) == input)
	}
}

func TestReadByte(t *testing.T) {
	t.Parallel()
	var input = "hello world"
	for n := 1; n < len(input); n++ {
		r := newReader(n, input)
		var s string
		for i := 0; i < len(input); i++ {
			b1, err := r.PeekByte()
			assert.MustNoError(t, err)
			b2, err := r.ReadByte()
			assert.MustNoError(t, err)
			assert.Must(t, b1 == b2)
			s += string(b1)
		}
		assert.Must(t, s == input)
	}
}

func TestReadBytes(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "hello world %d ", i)
	}
	var input = b.String()
	for n := 1; n < len(input); n++ {
		r := newReader(n, input)
		var s string
		for i := 0; i < 30; i++ {
			b, err := r.ReadBytes(' ')
			assert.MustNoError(t, err)
			s += string(b)
		}
		assert.Must(t, s == input)
	}
}

func TestReadFull(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "hello world %d ", i)
	}
	var input = b.String()
	for n := 1; n < len(input); n++ {
		r := newReader(n, input)
		b, err := r.ReadFull(len(input))
		assert.MustNoError(t, err)
		assert.Must(t, string(b) == input)
	}
}

func newWriter(n int, b *bytes.Buffer) *Writer {
	return &Writer{wr: b, buf: make([]byte, n)}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	for n := 1; n < 20; n++ {
		var input string
		var b bytes.Buffer
		var w = newWriter(n, &b)
		for i := 0; i < 10; i++ {
			s := fmt.Sprintf("hello world %d", i)
			_, err := w.Write([]byte(s))
			assert.MustNoError(t, err)
			input += s
		}
		assert.MustNoError(t, w.Flush())
		assert.Must(t, b.String() == input)
	}
}

func TestWriteBytes(t *testing.T) {
	t.Parallel()
	var input = "hello world!!"
	for n := 1; n < 20; n++ {
		var b bytes.Buffer
		var w = newWriter(n, &b)
		for i := 0; i < len(input); i++ {
			assert.MustNoError(t, w.WriteByte(input[i]))
		}
		assert.MustNoError(t, w.Flush())
		assert.Must(t, b.String() == input)
	}
}

func TestWriteString(t *testing.T) {
	t.Parallel()
	for n := 1; n < 20; n++ {
		var input string
		var b bytes.Buffer
		var w = newWriter(n, &b)
		for i := 0; i < 10; i++ {
			s := fmt.Sprintf("hello world %d", i)
			_, err := w.WriteString(s)
			assert.MustNoError(t, err)
			input += s
		}
		assert.MustNoError(t, w.Flush())
		assert.Must(t, b.String() == input)
	}
}
