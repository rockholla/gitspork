// Package logutil provides gitspork's concrete Logger implementation used by
// the CLI binary. It satisfies internal/sdktypes.Logger. SDK consumers can pass
// this or any other sdktypes.Logger implementation (or nil for silent).
package logutil

import (
	"log"
	"os"

	"github.com/fatih/color"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// Logger writes log/error messages to stdout/stderr with ANSI color when
// stdout is a TTY. Its Log and Error methods satisfy sdktypes.Logger; Fatal
// stays concrete because it terminates the process and isn't needed by SDK
// consumers.
type Logger struct {
	defaultLogger *log.Logger
	errorLogger   *log.Logger
}

// Compile-time assertion that Logger satisfies sdktypes.Logger.
var _ sdktypes.Logger = (*Logger)(nil)

// New returns a Logger configured for the CLI.
func New() *Logger {
	return &Logger{
		defaultLogger: log.New(os.Stdout, color.CyanString("INFO: "), 0),
		errorLogger:   log.New(os.Stderr, color.RedString("ERROR: "), 0),
	}
}

// Log writes an informational message to stdout.
func (l *Logger) Log(msg string, v ...any) {
	l.defaultLogger.Printf(msg, v...)
}

// Error writes an error message to stderr.
func (l *Logger) Error(msg string, v ...any) {
	l.errorLogger.Printf(msg, v...)
}

// Fatal writes an error message to stderr and exits with code 1.
func (l *Logger) Fatal(msg string, v ...any) {
	l.errorLogger.Fatalf(msg, v...)
}
