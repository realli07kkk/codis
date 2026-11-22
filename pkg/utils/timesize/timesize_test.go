// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package timesize

import (
	"errors"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestTimeSize(t *testing.T) {
	t.Parallel()
	assert.Must(t, MustParse("1") == time.Second)
	assert.Must(t, MustParse("1s") == time.Second)
	assert.Must(t, MustParse("1m") == time.Minute)
	assert.Must(t, MustParse("1h") == time.Hour)
	assert.Must(t, MustParse("1ms") == time.Millisecond)
	assert.Must(t, MustParse("1us") == time.Microsecond)
	assert.Must(t, MustParse("1ns") == time.Nanosecond)

	assert.Must(t, MustParse(" -1 ") == -1*time.Second)
	assert.Must(t, MustParse(" -1s ") == -1*time.Second)
	assert.Must(t, MustParse(" -1m ") == -1*time.Minute)
	assert.Must(t, MustParse(" -1h ") == -1*time.Hour)
	assert.Must(t, MustParse(" -1ms ") == -1*time.Millisecond)
	assert.Must(t, MustParse(" -1us ") == -1*time.Microsecond)
	assert.Must(t, MustParse(" -1ns ") == -1*time.Nanosecond)

	assert.Must(t, MustParse(" 1.5 ") == time.Duration(1.5*float64(time.Second)))
	assert.Must(t, MustParse(" 1.5 s ") == time.Duration(1.5*float64(time.Second)))
	assert.Must(t, MustParse(" 1.5 m ") == time.Duration(1.5*float64(time.Minute)))
	assert.Must(t, MustParse(" 1.5 h ") == time.Duration(1.5*float64(time.Hour)))
	assert.Must(t, MustParse(" 1.5 ms ") == time.Duration(1.5*float64(time.Millisecond)))
	assert.Must(t, MustParse(" 1.5 us ") == time.Duration(1.5*float64(time.Microsecond)))
}

func TestTimeSizeBoundaryAccepted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"0", 0},
		{"  0  ", 0},
		{"-0", 0},
		{"100", 100 * time.Second},
		{"1.5", 1500 * time.Millisecond},
		{"-1.5", -(1500 * time.Millisecond)},
		{"\t1\ts\t", time.Second},
		{"1.5m", 90 * time.Second},
		{"9223372036", 9223372036 * time.Second},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("Parse(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestTimeSizeBoundaryRejected(t *testing.T) {
	t.Parallel()
	tests := []string{
		"",
		"   ",
		"-",
		"1.2.3",
		"1 s extra",
		"1S",
		"1e3",
		"9223372036854775808",
	}
	for _, in := range tests {
		if _, err := Parse(in); !errors.Is(err, ErrBadTimeSize) {
			t.Fatalf("Parse(%q) error = %v, want %v", in, err, ErrBadTimeSize)
		}
	}
}

// Unit errors come from time.ParseDuration, not from timesize itself; the
// distinction predates this change and must stay observable.
func TestTimeSizeBadUnitPassesThroughParseDurationError(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"1d", "1kb", "1.2.3s"} {
		_, err := Parse(in)
		if err == nil {
			t.Fatalf("Parse(%q) unexpectedly succeeded", in)
		}
		if errors.Is(err, ErrBadTimeSize) {
			t.Fatalf("Parse(%q) error = %v, want time.ParseDuration error", in, err)
		}
	}
}

func TestTimeSizeTextRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value Duration
		text  string
	}{
		{0, "0"},
		{Duration(time.Hour), "1h"},
		{Duration(-2 * time.Hour), "-2h"},
		{Duration(90 * time.Minute), "90m"},
		{Duration(1500 * time.Millisecond), "1500ms"},
		{Duration(2500 * time.Microsecond), "2500us"},
	}
	for _, tt := range tests {
		text, err := tt.value.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v) returned error: %v", tt.value.Duration(), err)
		}
		if string(text) != tt.text {
			t.Fatalf("MarshalText(%v) = %q, want %q", tt.value.Duration(), text, tt.text)
		}
		var got Duration
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) returned error: %v", text, err)
		}
		if got != tt.value {
			t.Fatalf("UnmarshalText(%q) = %v, want %v", text, got.Duration(), tt.value.Duration())
		}
	}
}

// Durations below microsecond precision fall back to time.Duration.String(),
// whose symbol is not in the config grammar. That asymmetry predates this
// change, so it is pinned here instead of silently changing the format.
func TestTimeSizeMarshalTextBelowMicrosecond(t *testing.T) {
	t.Parallel()
	text, err := Duration(1234 * time.Nanosecond).MarshalText()
	if err != nil {
		t.Fatalf("MarshalText returned error: %v", err)
	}
	if string(text) != "1.234µs" {
		t.Fatalf("MarshalText = %q, want %q", text, "1.234µs")
	}
	var got Duration
	if err := got.UnmarshalText(text); !errors.Is(err, ErrBadTimeSize) {
		t.Fatalf("UnmarshalText(%q) error = %v, want %v", text, err, ErrBadTimeSize)
	}
}
