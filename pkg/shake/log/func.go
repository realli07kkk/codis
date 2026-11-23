package log

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-stack/stack"
)

func Debugf(format string, args ...interface{}) {
	logger.Debug().Msgf(format, args...)
}

func Infof(format string, args ...interface{}) {
	logger.Info().Msgf(format, args...)
}

func Warnf(format string, args ...interface{}) {
	logger.Warn().Msgf(format, args...)
}

// Panicf logs the message with a stack trace and exits the process. Upstream
// RedisShake relies on this as its fatal-error path; codis-shake runs one
// migration per process and reports the exit code to codis-dashboard, so the
// behaviour is kept unchanged. The stack frame prefixes are adjusted to the
// vendored import path and the codis-shake entry point.
func Panicf(format string, args ...interface{}) {
	frames := stack.Trace()
	errMsg := fmt.Sprintf(format, args...)
	for _, frame := range frames {
		frameStr := fmt.Sprintf("%+v", frame)
		if strings.HasPrefix(frameStr, "github.com/CodisLabs/codis/cmd/shake/main.go") {
			frameStr = "codis/cmd/" + frameStr
		}
		if strings.HasPrefix(frameStr, "github.com/CodisLabs/codis/pkg/shake/log/func") {
			continue
		}
		errMsg += fmt.Sprintf("\n\t\t\t%v -> %n()", frameStr, frame)
	}
	logger.Error().Msg(errMsg)
	os.Exit(1)
}
