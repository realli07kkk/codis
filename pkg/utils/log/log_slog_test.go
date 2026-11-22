// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package log

import (
	"bytes"
	stderrors "errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestStructuredAttrsAndWith(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetLevel(LevelInfo)
	logger.SetTraceLevel(LevelNone)

	logger.With("component", "proxy").Info("connected", "addr", "127.0.0.1:6379", "count", 3)

	got := out.String()
	want := regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} log_slog_test\.go:\d+: \[INFO\] connected component=proxy addr=127\.0\.0\.1:6379 count=3\n$`)
	if !want.MatchString(got) {
		t.Fatalf("structured output = %q", got)
	}
}

func TestStructuredGroupsAndNestedAttrs(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetTraceLevel(LevelNone)

	logger.WithGroup("db").With("driver", "redis").Info("query",
		slog.String("op", "get"),
		slog.Group("args", slog.Int("slot", 1), slog.String("key", "k")),
	)

	got := out.String()
	for _, want := range []string{
		"[INFO] query db.driver=redis db.op=get db.args.slot=1 db.args.key=k",
		"log_slog_test.go",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
}

func TestStructuredLevelFilteringAndLazyValues(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetLevel(LevelWarn)

	formatted := 0
	logger.Slog().Info("hidden", "value", countingLogValuer{&formatted})
	logger.Slog().Debug("hidden", "value", countingLogValuer{&formatted})
	if out.Len() != 0 {
		t.Fatalf("filtered structured records produced output: %q", out.String())
	}
	if formatted != 0 {
		t.Fatalf("filtered structured records evaluated LogValuer %d times", formatted)
	}

	logger.Slog().Warn("visible", "value", countingLogValuer{&formatted})
	if got := out.String(); !strings.Contains(got, "[WARN] visible value=lazy\n") {
		t.Fatalf("warn output = %q", got)
	}
	if formatted != 1 {
		t.Fatalf("enabled structured record evaluated LogValuer %d times, want 1", formatted)
	}
}

func TestStructuredErrorAddsStackAndHonorsTraceLevel(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetTraceLevel(LevelError)

	logger.Slog().Error("boom", "attempt", 1)
	got := out.String()
	if !strings.Contains(got, "[ERROR] boom attempt=1\n[stack]: \n") {
		t.Fatalf("structured error output = %q", got)
	}
	if !strings.Contains(got, "log_slog_test.go") {
		t.Fatalf("structured stack does not point at test file: %q", got)
	}

	out.Reset()
	logger.SetTraceLevel(LevelNone)
	logger.Slog().Error("no stack")
	got = out.String()
	if strings.Contains(got, "[stack]:") {
		t.Fatalf("trace-disabled error unexpectedly has stack: %q", got)
	}
}

func TestStructuredErrorLevelMapping(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetLevel(LevelError)
	logger.SetTraceLevel(LevelNone)

	logger.Slog().Warn("warn")
	logger.Slog().Info("info")
	logger.Slog().Error("error")
	got := out.String()
	if strings.Contains(got, "[WARN]") || strings.Contains(got, "[INFO]") {
		t.Fatalf("levels below ERROR leaked: %q", got)
	}
	if !strings.Contains(got, "[ERROR] error\n") {
		t.Fatalf("error record missing: %q", got)
	}
}

func TestPrintIsNeverFiltered(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetLevel(LevelNone)

	logger.Print("plain")
	logger.Printf("formatted %d", 1)
	logger.Println("line")
	got := out.String()
	for _, want := range []string{"[LOG] plain\n", "[LOG] formatted 1\n", "[LOG] line\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q does not contain %q", got, want)
		}
	}
}

func TestFlagsPrefixAndLevelString(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetFlags(0)
	logger.SetPrefix("p: ")
	logger.Info("x")
	if got := out.String(); got != "p: [INFO] x\n" {
		t.Fatalf("output = %q", got)
	}
	if logger.Flags() != 0 || logger.Prefix() != "p: " {
		t.Fatalf("flags/prefix = %d/%q", logger.Flags(), logger.Prefix())
	}
	if !logger.SetLevelString("warning") {
		t.Fatal("SetLevelString(warning) should succeed")
	}
	if logger.level != LevelWarn {
		t.Fatalf("level = %v, want %v", logger.level, LevelWarn)
	}
	if logger.SetLevelString("bogus") {
		t.Fatal("SetLevelString(bogus) should fail")
	}
}

func TestPanicWritesAndUsesExitOne(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetLevel(LevelNone)

	restore := replaceExit(t)
	logger.Panic("fatal")
	if code := restore(); code != 1 {
		t.Fatalf("Panic exit code = %d, want 1", code)
	}
	got := out.String()
	if !strings.Contains(got, "[PANIC] fatal\n") {
		t.Fatalf("panic output = %q", got)
	}
	if !strings.Contains(got, "[stack]: \n") {
		t.Fatalf("panic output has no stack: %q", got)
	}

	out.Reset()
	restore = replaceExit(t)
	logger.PanicError(stderrors.New("root"), "fatal error")
	if code := restore(); code != 1 {
		t.Fatalf("PanicError exit code = %d, want 1", code)
	}
	got = out.String()
	if !strings.Contains(got, "[PANIC] fatal error\n[error]: root\n") {
		t.Fatalf("panic error output = %q", got)
	}
}

func TestCloseIsIdempotentAndWriteAfterCloseFails(t *testing.T) {
	w := &countingWriteCloser{}
	logger := New(w, "")
	logger.Info("before close")
	logger.Close()
	logger.Close()
	if w.closes != 1 {
		t.Fatalf("Close called %d times, want 1", w.closes)
	}
	if err := logger.write([]byte("after")); !stderrors.Is(err, ErrClosed) {
		t.Fatalf("write after close error = %v, want %v", err, ErrClosed)
	}
	if !strings.Contains(w.String(), "[INFO] before close") {
		t.Fatalf("missing pre-close output: %q", w.String())
	}
}

func TestCloseDoesNotCloseSharedStderr(t *testing.T) {
	logger := New(os.Stderr, "")
	logger.Close()
	logger.Close()
	if _, err := os.Stderr.Stat(); err != nil {
		t.Fatalf("os.Stderr was closed by Logger.Close: %v", err)
	}
}

func TestConcurrentLoggingAndClose(t *testing.T) {
	w := &countingWriteCloser{}
	logger := New(w, "")
	logger.SetTraceLevel(LevelNone)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				logger.Infof("line %d", j)
			}
		}()
	}
	logger.Close()
	wg.Wait()

	if w.closes != 1 {
		t.Fatalf("Close called %d times, want 1", w.closes)
	}
}

func TestSetStdLogOverridesPackageFunctions(t *testing.T) {
	restore := preserveStdLog(t)
	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetTraceLevel(LevelNone)
	SetStdLog(logger)

	Info("through override")
	if got := out.String(); !strings.Contains(got, "[INFO] through override\n") {
		t.Fatalf("package log output = %q", got)
	}
	if got := SwapStdLog(nil); got != logger {
		t.Fatalf("SwapStdLog returned %p, want %p", got, logger)
	}
	restore()
}

func TestDirectStdLogAssignmentStillWorks(t *testing.T) {
	restore := preserveStdLog(t)
	SetStdLog(nil)

	var out bytes.Buffer
	logger := New(&out, "")
	logger.SetTraceLevel(LevelNone)
	StdLog = logger

	Info("through legacy assignment")
	if got := out.String(); !strings.Contains(got, "[INFO] through legacy assignment\n") {
		t.Fatalf("package log output = %q", got)
	}
	restore()
}

func TestStdLogSwapRaceFree(t *testing.T) {
	restore := preserveStdLog(t)
	a := New(io.Discard, "")
	b := New(io.Discard, "")
	SetStdLog(a)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				Info("concurrent")
			}
		}
	}()
	for i := 0; i < 500; i++ {
		SetStdLog(b)
		SetStdLog(a)
	}
	close(stop)
	wg.Wait()
	restore()
}

func preserveStdLog(t *testing.T) func() {
	t.Helper()
	savedLog := StdLog
	savedOverride := stdLogOverride.Swap(nil)
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		stdLogOverride.Store(savedOverride)
		StdLog = savedLog
	}
	t.Cleanup(restore)
	return restore
}

func replaceExit(t *testing.T) func() int {
	t.Helper()
	prev := osExit
	code := -1
	osExit = func(c int) { code = c }
	restored := false
	restore := func() int {
		if !restored {
			restored = true
			osExit = prev
		}
		return code
	}
	t.Cleanup(func() { restore() })
	return restore
}

type countingLogValuer struct {
	count *int
}

func (v countingLogValuer) LogValue() slog.Value {
	*v.count++
	return slog.StringValue("lazy")
}

type countingWriteCloser struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closes int
}

func (w *countingWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *countingWriteCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closes++
	return nil
}

func (w *countingWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
