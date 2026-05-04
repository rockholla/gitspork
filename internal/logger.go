package internal

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/printer"
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

// ColorizeYAMLSchema applies syntax highlighting to a YAML schema string using the
// go-yaml lexer for token-accurate coloring of keys, string values, and comments.
// When color output is disabled (non-TTY or NO_COLOR), the string is returned unchanged.
func ColorizeYAMLSchema(schema string) string {
	if color.NoColor {
		return schema
	}
	p := printer.Printer{
		MapKey: func() *printer.Property {
			return &printer.Property{Prefix: "\x1b[96m", Suffix: "\x1b[0m"}
		},
		String: func() *printer.Property {
			return &printer.Property{Prefix: "\x1b[92m", Suffix: "\x1b[0m"}
		},
		Comment: func() *printer.Property {
			return &printer.Property{Prefix: "\x1b[2m", Suffix: "\x1b[0m"}
		},
	}
	tokens := lexer.Tokenize(schema)
	return strings.TrimRight(p.PrintTokens(tokens), "\n")
}

// NewLogger will return a new instance of a logger
func NewLogger() *Logger {
	return &Logger{
		defaultLogger: log.New(os.Stdout, color.CyanString("INFO: "), 0),
		errorLogger:   log.New(os.Stderr, color.RedString("ERROR: "), 0),
	}
}
