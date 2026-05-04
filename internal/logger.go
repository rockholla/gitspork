package internal

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
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

var (
	yamlCommentRe = regexp.MustCompile(`(#.*)$`)
	yamlKeyRe     = regexp.MustCompile(`^(\s*-?\s*)([a-zA-Z_][a-zA-Z0-9_]*\s*:)`)
	yamlValueRe   = regexp.MustCompile(`:\s*(".*")`)

	colorYAMLKey     = color.New(color.FgCyan)
	colorYAMLValue   = color.New(color.FgGreen)
	colorYAMLComment = color.New(color.Faint)
)

// ColorizeYAMLSchema applies syntax highlighting to a YAML schema string (keys, values, comments).
// When color output is disabled (non-TTY or NO_COLOR), the string is returned unchanged.
func ColorizeYAMLSchema(schema string) string {
	if color.NoColor {
		return schema
	}
	var sb strings.Builder
	for _, line := range strings.Split(schema, "\n") {
		// extract and strip comment first so it isn't re-colored by key/value rules
		comment := ""
		if loc := yamlCommentRe.FindStringIndex(line); loc != nil {
			comment = colorYAMLComment.Sprint(line[loc[0]:])
			line = line[:loc[0]]
		}
		// color inline value (quoted string after colon)
		line = yamlValueRe.ReplaceAllStringFunc(line, func(m string) string {
			sub := yamlValueRe.FindStringSubmatch(m)
			return strings.Replace(m, sub[1], colorYAMLValue.Sprint(sub[1]), 1)
		})
		// color key
		line = yamlKeyRe.ReplaceAllStringFunc(line, func(m string) string {
			sub := yamlKeyRe.FindStringSubmatch(m)
			return sub[1] + colorYAMLKey.Sprint(sub[2])
		})
		sb.WriteString(line + comment + "\n")
	}
	// trim the trailing newline added by the loop
	return strings.TrimSuffix(sb.String(), "\n")
}

// NewLogger will return a new instance of a logger
func NewLogger() *Logger {
	return &Logger{
		defaultLogger: log.New(os.Stdout, color.CyanString("INFO: "), 0),
		errorLogger:   log.New(os.Stderr, color.RedString("ERROR: "), 0),
	}
}
