package internal

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

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

// Diff reads a unified diff from r and writes it to stdout with color highlighting.
// Addition lines (+) are green, removal lines (-) are red, hunk headers (@@) are cyan,
// and file header lines (---, +++) are bold. All other lines are printed as-is.
func (l *Logger) Diff(r io.Reader) error {
	bold := color.New(color.Bold)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			bold.Println(line)
		case strings.HasPrefix(line, "@@"):
			color.Cyan("%s\n", line)
		case strings.HasPrefix(line, "+"):
			color.Green("%s\n", line)
		case strings.HasPrefix(line, "-"):
			color.Red("%s\n", line)
		default:
			fmt.Println(line)
		}
	}
	return scanner.Err()
}

// NewLogger will return a new instance of a logger
func NewLogger() *Logger {
	return &Logger{
		defaultLogger: log.New(os.Stdout, color.CyanString("INFO: "), 0),
		errorLogger:   log.New(os.Stderr, color.RedString("ERROR: "), 0),
	}
}
