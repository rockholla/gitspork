package internal

import (
	"log"
	"os"

	"github.com/fatih/color"
)

// Logger is an instance of a logger
type Logger struct {
	defaultLogger *log.Logger
	errorLogger   *log.Logger
}

// Log will log a normal/info log message
func (l *Logger) Log(msg string, v ...any) {
	l.defaultLogger.Printf(msg, v...)
}

// Error will log an error message
func (l *Logger) Error(msg string, v ...any) {
	l.errorLogger.Printf(msg, v...)
}

// Fatal will log an error message and use underlying log lib to exit w/ an error
func (l *Logger) Fatal(msg string, v ...any) {
	l.errorLogger.Fatalf(msg, v...)
}

// NewLogger will return a new instance of a logger
func NewLogger() *Logger {
	return &Logger{
		defaultLogger: log.New(os.Stdout, color.CyanString("INFO: "), 0),
		errorLogger:   log.New(os.Stderr, color.RedString("ERROR: "), 0),
	}
}
