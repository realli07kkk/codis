// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package bytesize

import (
	"errors"
	"testing"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestByteSize(t *testing.T) {
	t.Parallel()
	assert.Must(t, MustParse("1") == 1)
	assert.Must(t, MustParse("1b") == 1)
	assert.Must(t, MustParse("1k") == KB)
	assert.Must(t, MustParse("1m") == MB)
	assert.Must(t, MustParse("1g") == GB)
	assert.Must(t, MustParse("1t") == TB)
	assert.Must(t, MustParse("1p") == PB)

	assert.Must(t, MustParse(" -1") == -1)
	assert.Must(t, MustParse(" -1 b") == -1)
	assert.Must(t, MustParse(" -1 kb ") == -1*KB)
	assert.Must(t, MustParse(" -1 mb ") == -1*MB)
	assert.Must(t, MustParse(" -1 gb ") == -1*GB)
	assert.Must(t, MustParse(" -1 tb ") == -1*TB)
	assert.Must(t, MustParse(" -1 pb ") == -1*PB)

	assert.Must(t, MustParse(" 1.5") == 1)
	assert.Must(t, MustParse(" 1.5 kb ") == 1.5*KB)
	assert.Must(t, MustParse(" 1.5 mb ") == 1.5*MB)
	assert.Must(t, MustParse(" 1.5 gb ") == 1.5*GB)
	assert.Must(t, MustParse(" 1.5 tb ") == 1.5*TB)
	assert.Must(t, MustParse(" 1.5 pb ") == 1.5*PB)
}

func TestByteSizeError(t *testing.T) {
	t.Parallel()
	var err error
	_, err = Parse("--1")
	assert.Must(t, errors.Is(err, ErrBadByteSize))
	_, err = Parse("hello world")
	assert.Must(t, errors.Is(err, ErrBadByteSize))
	_, err = Parse("123.132.32")
	assert.Must(t, errors.Is(err, ErrBadByteSize))
}

func TestByteSizeBoundaryAccepted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"  0  ", 0},
		{"-0", 0},
		{"\t1\tmb\t", MB},
		{"1.", 1},
		{"-1.5kb", -1.5 * KB},
		{"1.5pb", int64(1.5 * PB)},
		{"9223372036854775807", 1<<63 - 1},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("Parse(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestByteSizeBoundaryRejected(t *testing.T) {
	t.Parallel()
	tests := []string{
		"",
		"   ",
		"b",
		"-",
		"1kb extra",
		"1KB",
		"1kb2",
		"1x",
		"1e3",
		"1..2",
		"1.2.3kb",
		"9223372036854775808",
	}
	for _, in := range tests {
		if _, err := Parse(in); !errors.Is(err, ErrBadByteSize) {
			t.Fatalf("Parse(%q) error = %v, want %v", in, err, ErrBadByteSize)
		}
	}
}

func TestByteSizeTextRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value Int64
		text  string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{1536, "1536"},
		{KB, "1kb"},
		{-2 * MB, "-2mb"},
		{PB, "1pb"},
	}
	for _, tt := range tests {
		text, err := tt.value.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%d) returned error: %v", tt.value, err)
		}
		if string(text) != tt.text {
			t.Fatalf("MarshalText(%d) = %q, want %q", tt.value, text, tt.text)
		}
		var got Int64
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) returned error: %v", text, err)
		}
		if got != tt.value {
			t.Fatalf("UnmarshalText(%q) = %d, want %d", text, got, tt.value)
		}
	}
}
