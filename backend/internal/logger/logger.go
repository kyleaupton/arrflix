package logger

import (
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type Logger = zerolog.Logger

// devTimeFormatOnce guards the one-time write to the process-global
// zerolog.TimeFieldFormat. Without it, concurrent New(true) callers (the test
// suites build many loggers in parallel) race on the same assignment.
var devTimeFormatOnce sync.Once

func New(dev bool) *Logger {
	if dev {
		cw := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05.000", // hh:mm:ss.mmm
		}

		l := zerolog.New(cw).
			With().
			Timestamp().
			Caller().
			Logger()

		// Prefer a readable time in dev
		devTimeFormatOnce.Do(func() {
			zerolog.TimeFieldFormat = time.RFC3339Nano
		})
		return &l
	}

	l := zerolog.New(os.Stdout).With().Timestamp().Logger()
	return &l
}
