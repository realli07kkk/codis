// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package log

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	codiserrors "github.com/CodisLabs/codis/pkg/utils/errors"
)

func TestLegacyOutputFormatAndLevelFiltering(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "prefix: ")
	logger.SetTraceLevel(LevelNone)

	logger.Infof("hello %d", 42)
	got := out.String()
	want := regexp.MustCompile(`^prefix: \d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} log_compat_test\.go:\d+: \[INFO\] hello 42\n$`)
	if !want.MatchString(got) {
		t.Fatalf("unexpected info line: %q", got)
	}

	out.Reset()
	logger.Print("plain")
	if got := out.String(); !strings.Contains(got, "[LOG] plain\n") {
		t.Fatalf("Print output = %q", got)
	}

	out.Reset()
	logger.Println("line")
	if got := out.String(); !strings.Contains(got, "[LOG] line\n") {
		t.Fatalf("Println output = %q", got)
	}
}

func TestLegacyLevelFilteringSkipsFormatting(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetLevel(LevelError)
	logger.SetTraceLevel(LevelNone)

	formatted := 0
	logger.Warnf("warn %s", countingStringer{&formatted})
	logger.Infof("info %s", countingStringer{&formatted})
	logger.Debugf("debug %s", countingStringer{&formatted})
	if out.Len() != 0 {
		t.Fatalf("disabled levels produced output: %q", out.String())
	}
	if formatted != 0 {
		t.Fatalf("disabled levels formatted messages %d times", formatted)
	}

	logger.Errorf("error %s", countingStringer{&formatted})
	if got := out.String(); !strings.Contains(got, "[ERROR] error formatted\n") {
		t.Fatalf("error output = %q", got)
	}
	if formatted != 1 {
		t.Fatalf("enabled level formatted messages %d times, want 1", formatted)
	}
}

func TestLegacyErrorMessageAndStackOrder(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetTraceLevel(LevelError)

	root := codiserrors.New("root cause")
	traced := codiserrors.Trace(root)
	logger.ErrorErrorf(traced, "operation failed: %d", 7)

	got := out.String()
	msgIdx := strings.Index(got, "[ERROR] operation failed: 7\n")
	errIdx := strings.Index(got, "[error]: root cause\n")
	stackIdx := strings.Index(got, "[stack]: \n")
	if msgIdx < 0 || errIdx < 0 || stackIdx < 0 {
		t.Fatalf("missing sections in %q", got)
	}
	if !(msgIdx < errIdx && errIdx < stackIdx) {
		t.Fatalf("section order broken: msg=%d err=%d stack=%d in %q", msgIdx, errIdx, stackIdx, got)
	}
	if !strings.Contains(got[errIdx:stackIdx], "log_compat_test.go") {
		t.Fatalf("error stack should point at test file: %q", got)
	}
	if !strings.Contains(got[stackIdx:], "log_compat_test.go") {
		t.Fatalf("log stack should point at test file: %q", got)
	}
}

func TestLegacyTraceLevelAddsStackToNonError(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetTraceLevel(LevelAll)

	logger.Info("with stack")
	got := out.String()
	if !strings.Contains(got, "[INFO] with stack\n[stack]: \n") {
		t.Fatalf("missing stack section: %q", got)
	}
	if !strings.Contains(got, "log_compat_test.go") {
		t.Fatalf("stack should point at test file: %q", got)
	}
}

func TestRollingFileWriteCloseAndReopen(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "codis.log")

	w, err := NewRollingFile(base, DailyRolling)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("first line\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close should be a no-op: %v", err)
	}
	if _, err := w.Write([]byte("after close\n")); !stderrors.Is(err, ErrClosedRollingFile) {
		t.Fatalf("write after close error = %v, want %v", err, ErrClosedRollingFile)
	}

	path := base + "." + time.Now().Format(string(DailyRolling))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first line\n" {
		t.Fatalf("rolling file content = %q", data)
	}
}

type countingStringer struct {
	count *int
}

func (s countingStringer) String() string {
	*s.count++
	return "formatted"
}
