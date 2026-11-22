// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/trace"
)

const (
	Ldate         = log.Ldate
	Llongfile     = log.Llongfile
	Lmicroseconds = log.Lmicroseconds
	Lshortfile    = log.Lshortfile
	LstdFlags     = log.LstdFlags
	Ltime         = log.Ltime
	Lmsgprefix    = log.Lmsgprefix
	LUTC          = log.LUTC
)

type (
	LogType  int64
	LogLevel int64
)

const (
	TYPE_ERROR = LogType(1 << iota)
	TYPE_WARN
	TYPE_INFO
	TYPE_DEBUG
	TYPE_PANIC = LogType(^0)
)

const (
	LevelNone = LogLevel(1<<iota - 1)
	LevelError
	LevelWarn
	LevelInfo
	LevelDebug
	LevelAll = LevelDebug
)

func (t LogType) String() string {
	switch t {
	default:
		return "[LOG]"
	case TYPE_PANIC:
		return "[PANIC]"
	case TYPE_ERROR:
		return "[ERROR]"
	case TYPE_WARN:
		return "[WARN]"
	case TYPE_INFO:
		return "[INFO]"
	case TYPE_DEBUG:
		return "[DEBUG]"
	}
}

func (l LogLevel) String() string {
	switch l {
	default:
		return "UNKNOWN"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelNone:
		return "NONE"
	}
}

func (l *LogLevel) ParseFromString(s string) bool {
	switch strings.ToUpper(s) {
	case "ERROR":
		*l = LevelError
	case "DEBUG":
		*l = LevelDebug
	case "WARN", "WARNING":
		*l = LevelWarn
	case "INFO":
		*l = LevelInfo
	case "NONE":
		*l = LevelNone
	default:
		return false
	}
	return true
}

func (l *LogLevel) Set(v LogLevel) {
	atomic.StoreInt64((*int64)(l), int64(v))
}

func (l *LogLevel) Test(m LogType) bool {
	v := atomic.LoadInt64((*int64)(l))
	return (v & int64(m)) != 0
}

// slog levels used for records that do not map 1:1 onto the level bitmask.
const (
	// levelLog is used by Print/Println, which are never filtered.
	levelLog = slog.Level(2)
	// levelPanic is above Error and is always enabled.
	levelPanic = slog.Level(12)
)

// Private attribute keys used to carry the legacy printf API diagnostics
// through slog.Record without changing their rendered layout.
const (
	attrKeyError = "\x00codis.log.error"
	attrKeyStack = "\x00codis.log.stack"
)

type nopCloser struct {
	io.Writer
}

func (*nopCloser) Close() error {
	return nil
}

func NopCloser(w io.Writer) io.WriteCloser {
	return &nopCloser{w}
}

type Logger struct {
	// level and trace come first so 64-bit atomic access is aligned even on
	// 32-bit platforms.
	level LogLevel
	trace LogLevel

	mu     sync.Mutex
	out    io.WriteCloser
	closed bool

	flags  atomic.Int64
	prefix atomic.Value // string

	slog *slog.Logger
}

var StdLog = New(NopCloser(os.Stderr), "")

// stdLogOverride lets new code replace the package-level logger without
// racing with concurrent logging. When it is nil (the default), package-level
// functions keep reading the legacy StdLog variable so existing startup code
// that assigns log.StdLog keeps working.
var stdLogOverride atomic.Pointer[Logger]

// ErrClosed is returned when writing to a closed Logger.
var ErrClosed = errors.New("logger is closed")

// osExit is a test seam for Panic*. Callers observe os.Exit(1) semantics; the
// logger tests replace it so the test process does not terminate.
var osExit = os.Exit

// SetStdLog atomically replaces the logger used by package-level functions.
// Passing nil clears the override and restores reading the legacy StdLog
// variable. Unlike assigning StdLog directly, SetStdLog may be called while
// other goroutines are logging.
func SetStdLog(l *Logger) {
	stdLogOverride.Store(l)
}

// SwapStdLog atomically replaces the package-level logger and returns the one
// that was in use before the call.
func SwapStdLog(l *Logger) *Logger {
	old := currentStdLog()
	stdLogOverride.Store(l)
	return old
}

// GetStdLog returns the logger currently used by package-level functions.
func GetStdLog() *Logger {
	return currentStdLog()
}

func currentStdLog() *Logger {
	if p := stdLogOverride.Load(); p != nil {
		return p
	}
	return StdLog
}

func New(writer io.Writer, prefix string) *Logger {
	if writer == nil {
		writer = io.Discard
	}
	out, ok := writer.(io.WriteCloser)
	if !ok || isSharedStdFile(writer) {
		out = NopCloser(writer)
	}
	l := &Logger{
		out:   out,
		level: LevelAll,
		trace: LevelError,
	}
	l.flags.Store(int64(LstdFlags | Lshortfile))
	l.prefix.Store(prefix)
	h := &handler{logger: l}
	l.slog = slog.New(h)
	return l
}

func isSharedStdFile(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && (f == os.Stdout || f == os.Stderr)
}

func (l *Logger) Flags() int {
	return int(l.flags.Load())
}

func (l *Logger) Prefix() string {
	if v := l.prefix.Load(); v != nil {
		return v.(string)
	}
	return ""
}

func (l *Logger) SetFlags(flags int) {
	l.flags.Store(int64(flags))
}

func (l *Logger) SetPrefix(prefix string) {
	l.prefix.Store(prefix)
}

func (l *Logger) SetLevel(v LogLevel) {
	l.level.Set(v)
}

func (l *Logger) SetLevelString(s string) bool {
	var v LogLevel
	if !v.ParseFromString(s) {
		return false
	} else {
		l.SetLevel(v)
		return true
	}
}

func (l *Logger) SetTraceLevel(v LogLevel) {
	l.trace.Set(v)
}

// Close is idempotent. It never closes os.Stdout/os.Stderr, even when they
// were passed to New directly.
func (l *Logger) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	out := l.out
	l.mu.Unlock()
	if out != nil {
		_ = out.Close()
	}
}

func (l *Logger) isDisabled(t LogType) bool {
	return t != TYPE_PANIC && !l.level.Test(t)
}

func (l *Logger) isTraceEnabled(t LogType) bool {
	return t == TYPE_PANIC || l.trace.Test(t)
}

// Slog returns the structured logger backed by this Logger's handler. Records
// emitted through it keep the configured level filtering, output format and
// automatic stacks for Error and above.
func (l *Logger) Slog() *slog.Logger {
	return l.slog
}

// With returns a structured logger carrying the supplied attributes.
func (l *Logger) With(args ...any) *slog.Logger {
	return l.slog.With(args...)
}

// WithGroup returns a structured logger with a group prefix.
func (l *Logger) WithGroup(name string) *slog.Logger {
	return l.slog.WithGroup(name)
}

// LogAttrs emits a structured record at level.
func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l.slog.LogAttrs(ctx, level, msg, attrs...)
}

func (l *Logger) Panic(v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprint(v...)
	l.output(1, nil, t, s)
	osExit(1)
}

func (l *Logger) Panicf(format string, v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprintf(format, v...)
	l.output(1, nil, t, s)
	osExit(1)
}

func (l *Logger) PanicError(err error, v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprint(v...)
	l.output(1, err, t, s)
	osExit(1)
}

func (l *Logger) PanicErrorf(err error, format string, v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprintf(format, v...)
	l.output(1, err, t, s)
	osExit(1)
}

func (l *Logger) Error(v ...any) {
	t := TYPE_ERROR
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, nil, t, s)
}

func (l *Logger) Errorf(format string, v ...any) {
	t := TYPE_ERROR
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, nil, t, s)
}

func (l *Logger) ErrorError(err error, v ...any) {
	t := TYPE_ERROR
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, err, t, s)
}

func (l *Logger) ErrorErrorf(err error, format string, v ...any) {
	t := TYPE_ERROR
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, err, t, s)
}

func (l *Logger) Warn(v ...any) {
	t := TYPE_WARN
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, nil, t, s)
}

func (l *Logger) Warnf(format string, v ...any) {
	t := TYPE_WARN
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, nil, t, s)
}

func (l *Logger) WarnError(err error, v ...any) {
	t := TYPE_WARN
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, err, t, s)
}

func (l *Logger) WarnErrorf(err error, format string, v ...any) {
	t := TYPE_WARN
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, err, t, s)
}

func (l *Logger) Info(v ...any) {
	t := TYPE_INFO
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, nil, t, s)
}

func (l *Logger) Infof(format string, v ...any) {
	t := TYPE_INFO
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, nil, t, s)
}

func (l *Logger) InfoError(err error, v ...any) {
	t := TYPE_INFO
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, err, t, s)
}

func (l *Logger) InfoErrorf(err error, format string, v ...any) {
	t := TYPE_INFO
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, err, t, s)
}

func (l *Logger) Debug(v ...any) {
	t := TYPE_DEBUG
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, nil, t, s)
}

func (l *Logger) Debugf(format string, v ...any) {
	t := TYPE_DEBUG
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, nil, t, s)
}

func (l *Logger) DebugError(err error, v ...any) {
	t := TYPE_DEBUG
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	l.output(1, err, t, s)
}

func (l *Logger) DebugErrorf(err error, format string, v ...any) {
	t := TYPE_DEBUG
	if l.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	l.output(1, err, t, s)
}

func (l *Logger) Print(v ...any) {
	s := fmt.Sprint(v...)
	l.output(1, nil, 0, s)
}

func (l *Logger) Printf(format string, v ...any) {
	s := fmt.Sprintf(format, v...)
	l.output(1, nil, 0, s)
}

func (l *Logger) Println(v ...any) {
	s := fmt.Sprintln(v...)
	l.output(1, nil, 0, s)
}

// output preserves the legacy printf entry points while routing every record
// through slog.Logger.LogAttrs. The handler renders the exact historical
// "[LEVEL] message", "[error]" and "[stack]" sections.
func (l *Logger) output(traceskip int, err error, t LogType, s string) {
	var stack trace.Stack
	if l.isTraceEnabled(t) {
		stack = trace.TraceN(traceskip+1, 32)
	}
	var attrs []slog.Attr
	if err != nil {
		attrs = append(attrs, slog.Any(attrKeyError, err))
	}
	if len(stack) != 0 {
		attrs = append(attrs, slog.Any(attrKeyStack, stack))
	}
	l.slog.LogAttrs(context.Background(), slogLevelOf(t), s, attrs...)
}

func Flags() int {
	return currentStdLog().Flags()
}

func Prefix() string {
	return currentStdLog().Prefix()
}

func SetFlags(flags int) {
	currentStdLog().SetFlags(flags)
}

func SetPrefix(prefix string) {
	currentStdLog().SetPrefix(prefix)
}

func SetLevel(v LogLevel) {
	currentStdLog().SetLevel(v)
}

func SetLevelString(s string) bool {
	return currentStdLog().SetLevelString(s)
}

func SetTrace(v LogLevel) {
	currentStdLog().SetTraceLevel(v)
}

// Slog returns the structured logger for the package-level Logger.
func Slog() *slog.Logger {
	return currentStdLog().Slog()
}

// With returns a structured logger for the package-level Logger.
func With(args ...any) *slog.Logger {
	return currentStdLog().With(args...)
}

// WithGroup returns a grouped structured logger for the package-level Logger.
func WithGroup(name string) *slog.Logger {
	return currentStdLog().WithGroup(name)
}

// LogAttrs emits a structured record through the package-level Logger.
func LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	currentStdLog().LogAttrs(ctx, level, msg, attrs...)
}

func Panic(v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprint(v...)
	currentStdLog().output(1, nil, t, s)
	osExit(1)
}

func Panicf(format string, v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprintf(format, v...)
	currentStdLog().output(1, nil, t, s)
	osExit(1)
}

func PanicError(err error, v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprint(v...)
	currentStdLog().output(1, err, t, s)
	osExit(1)
}

func PanicErrorf(err error, format string, v ...any) {
	t := TYPE_PANIC
	s := fmt.Sprintf(format, v...)
	currentStdLog().output(1, err, t, s)
	osExit(1)
}

func Error(v ...any) {
	t := TYPE_ERROR
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, nil, t, s)
}

func Errorf(format string, v ...any) {
	t := TYPE_ERROR
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, nil, t, s)
}

func ErrorError(err error, v ...any) {
	t := TYPE_ERROR
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, err, t, s)
}

func ErrorErrorf(err error, format string, v ...any) {
	t := TYPE_ERROR
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, err, t, s)
}

func Warn(v ...any) {
	t := TYPE_WARN
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, nil, t, s)
}

func Warnf(format string, v ...any) {
	t := TYPE_WARN
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, nil, t, s)
}

func WarnError(err error, v ...any) {
	t := TYPE_WARN
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, err, t, s)
}

func WarnErrorf(err error, format string, v ...any) {
	t := TYPE_WARN
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, err, t, s)
}

func Info(v ...any) {
	t := TYPE_INFO
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, nil, t, s)
}

func Infof(format string, v ...any) {
	t := TYPE_INFO
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, nil, t, s)
}

func InfoError(err error, v ...any) {
	t := TYPE_INFO
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, err, t, s)
}

func InfoErrorf(err error, format string, v ...any) {
	t := TYPE_INFO
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, err, t, s)
}

func Debug(v ...any) {
	t := TYPE_DEBUG
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, nil, t, s)
}

func Debugf(format string, v ...any) {
	t := TYPE_DEBUG
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, nil, t, s)
}

func DebugError(err error, v ...any) {
	t := TYPE_DEBUG
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprint(v...)
	logger.output(1, err, t, s)
}

func DebugErrorf(err error, format string, v ...any) {
	t := TYPE_DEBUG
	logger := currentStdLog()
	if logger.isDisabled(t) {
		return
	}
	s := fmt.Sprintf(format, v...)
	logger.output(1, err, t, s)
}

func Print(v ...any) {
	s := fmt.Sprint(v...)
	currentStdLog().output(1, nil, 0, s)
}

func Printf(format string, v ...any) {
	s := fmt.Sprintf(format, v...)
	currentStdLog().output(1, nil, 0, s)
}

func Println(v ...any) {
	s := fmt.Sprintln(v...)
	currentStdLog().output(1, nil, 0, s)
}

func slogLevelOf(t LogType) slog.Level {
	switch t {
	case TYPE_PANIC:
		return levelPanic
	case TYPE_ERROR:
		return slog.LevelError
	case TYPE_WARN:
		return slog.LevelWarn
	case TYPE_INFO:
		return slog.LevelInfo
	case TYPE_DEBUG:
		return slog.LevelDebug
	default:
		return levelLog
	}
}

func logTypeOf(level slog.Level) LogType {
	switch {
	case level == levelLog:
		return 0
	case level == levelPanic:
		return TYPE_PANIC
	case level >= slog.LevelError:
		return TYPE_ERROR
	case level >= slog.LevelWarn:
		return TYPE_WARN
	case level >= slog.LevelInfo:
		return TYPE_INFO
	default:
		return TYPE_DEBUG
	}
}

// handler adapts slog records onto the historical Codis text layout. It also
// owns level filtering and automatic stack capture for Error and above.
type handler struct {
	logger *Logger
	attrs  []handlerAttr
	groups []string
}

type handlerAttr struct {
	prefix string
	attr   slog.Attr
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	t := logTypeOf(level)
	if t == TYPE_PANIC || t == 0 {
		return true
	}
	return !h.logger.isDisabled(t)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if !h.Enabled(ctx, r.Level) {
		return nil
	}
	var file string
	var line int
	if h.logger.Flags()&(Lshortfile|Llongfile) != 0 {
		file, line = captureCaller()
	}
	return h.format(r, file, line)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := h.clone()
	prefix := h.groupPrefix()
	for _, a := range attrs {
		h2.attrs = append(h2.attrs, handlerAttr{prefix: prefix, attr: a})
	}
	return h2
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := h.clone()
	h2.groups = append(h2.groups, name)
	return h2
}

func (h *handler) clone() *handler {
	h2 := *h
	h2.attrs = append([]handlerAttr(nil), h.attrs...)
	h2.groups = append([]string(nil), h.groups...)
	return &h2
}

func (h *handler) groupPrefix() string {
	return strings.Join(h.groups, ".")
}

func (h *handler) format(r slog.Record, file string, line int) error {
	l := h.logger
	t := logTypeOf(r.Level)

	var (
		inline   []handlerAttr
		errValue error
		hasError bool
		stack    trace.Stack
		hasStack bool
	)

	collect := func(prefix string, a slog.Attr) {
		if a.Value.Kind() == slog.KindAny {
			switch a.Key {
			case attrKeyError:
				if err, ok := a.Value.Any().(error); ok {
					errValue, hasError = err, true
					return
				}
			case attrKeyStack:
				if s, ok := a.Value.Any().(trace.Stack); ok {
					stack, hasStack = s, len(s) != 0
					return
				}
				if s, ok := a.Value.Any().([]*trace.Record); ok {
					stack, hasStack = trace.Stack(s), len(s) != 0
					return
				}
			}
		}
		inline = append(inline, handlerAttr{prefix: prefix, attr: a})
	}

	for _, ha := range h.attrs {
		collect(ha.prefix, ha.attr)
	}
	prefix := h.groupPrefix()
	r.Attrs(func(a slog.Attr) bool {
		collect(prefix, a)
		return true
	})

	if !hasStack && l.isTraceEnabled(t) {
		stack = captureExternalStack()
		hasStack = len(stack) != 0
	}

	var b bytes.Buffer
	b.Grow(256)
	b.Write(l.appendHeader(nil, r.Time, file, line))
	b.WriteString(t.String())
	b.WriteByte(' ')
	msg := r.Message
	if len(inline) != 0 {
		msg = strings.TrimRight(msg, "\n")
	}
	b.WriteString(msg)
	for _, ha := range inline {
		appendAttr(&b, ha.prefix, ha.attr)
	}
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		b.WriteByte('\n')
	}
	if hasError {
		b.WriteString("[error]: ")
		b.WriteString(errValue.Error())
		b.WriteByte('\n')
		if s := errors.Stack(errValue); s != nil {
			b.WriteString(s.StringWithIndent(1))
		}
	}
	if hasStack {
		b.WriteString("[stack]: \n")
		b.WriteString(stack.StringWithIndent(1))
	}
	return l.write(b.Bytes())
}

func appendAttr(b *bytes.Buffer, prefix string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := joinKey(prefix, a.Key)
		for _, member := range v.Group() {
			appendAttr(b, p, member)
		}
		return
	}
	b.WriteByte(' ')
	b.WriteString(joinKey(prefix, a.Key))
	b.WriteByte('=')
	b.WriteString(v.String())
}

func joinKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	if key == "" {
		return prefix
	}
	return prefix + "." + key
}

func (l *Logger) write(line []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}
	_, err := l.out.Write(line)
	return err
}

func (l *Logger) appendHeader(b []byte, t time.Time, file string, line int) []byte {
	flag := l.Flags()
	prefix := l.Prefix()
	if flag&Lmsgprefix == 0 {
		b = append(b, prefix...)
	}
	if flag&(Ldate|Ltime|Lmicroseconds) != 0 {
		if flag&LUTC != 0 {
			t = t.UTC()
		}
		if flag&Ldate != 0 {
			year, month, day := t.Date()
			b = appendPaddedInt(b, year, 4)
			b = append(b, '/')
			b = appendPaddedInt(b, int(month), 2)
			b = append(b, '/')
			b = appendPaddedInt(b, day, 2)
			b = append(b, ' ')
		}
		if flag&(Ltime|Lmicroseconds) != 0 {
			hour, min, sec := t.Clock()
			b = appendPaddedInt(b, hour, 2)
			b = append(b, ':')
			b = appendPaddedInt(b, min, 2)
			b = append(b, ':')
			b = appendPaddedInt(b, sec, 2)
			if flag&Lmicroseconds != 0 {
				b = append(b, '.')
				b = appendPaddedInt(b, t.Nanosecond()/1e3, 6)
			}
			b = append(b, ' ')
		}
	}
	if flag&(Lshortfile|Llongfile) != 0 {
		if flag&Lshortfile != 0 {
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			file = short
		}
		b = append(b, file...)
		b = append(b, ':')
		b = appendPaddedInt(b, line, -1)
		b = append(b, ": "...)
	}
	if flag&Lmsgprefix != 0 {
		b = append(b, prefix...)
	}
	return b
}

func appendPaddedInt(b []byte, v, width int) []byte {
	if width <= 0 {
		return strconv.AppendInt(b, int64(v), 10)
	}
	var tmp [20]byte
	s := strconv.AppendInt(tmp[:0], int64(v), 10)
	for i := len(s); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

// captureCaller returns the first stack frame outside the logging
// implementation for the "file:line:" header.
func captureCaller() (string, int) {
	var pcs [16]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if !isLogInternalFunction(f.Function, f.File) {
			return f.File, f.Line
		}
		if !more {
			break
		}
	}
	return "???", 0
}

// captureExternalStack returns the external frames used for automatic Error+
// stacks.
func captureExternalStack() trace.Stack {
	var pcs [64]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])

	stack := make(trace.Stack, 0, 32)
	for len(stack) < 32 {
		f, more := frames.Next()
		if !isLogInternalFunction(f.Function, f.File) {
			stack = append(stack, &trace.Record{Name: f.Function, File: f.File, Line: f.Line})
		}
		if !more {
			break
		}
	}
	return stack
}

const logPackageSuffix = "/pkg/utils/log."

func isLogInternalFunction(name, file string) bool {
	if strings.HasPrefix(name, "log/slog.") || strings.HasPrefix(name, "runtime.") {
		return true
	}
	// Match by package suffix rather than the module path so forks keep
	// caller attribution. Tests in this package remain valid external callers.
	return strings.Contains(name, logPackageSuffix) && !strings.HasSuffix(file, "_test.go")
}
