package logutil

import "strings"

// ANSI escape codes for unified-diff colorization. Emitted directly (not via
// fatih/color) so ColorizeUnifiedDiff produces the same output regardless of
// the process's TTY state — SDK consumers may render the result in contexts
// where TTY detection at generation time doesn't reflect the eventual sink.
const (
	ansiBold  = "\x1b[1m"
	ansiCyan  = "\x1b[36m"
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"
)

// ColorizeUnifiedDiff applies ANSI color codes to a unified-diff string
// based on per-line prefix: `diff --git` and `+++`/`---` headers bold, `@@`
// hunks cyan, `+` additions green, `-` removals red. Always emits color
// codes; callers that want to suppress color (non-TTY output, NO_COLOR env)
// should render the raw diff instead.
func ColorizeUnifiedDiff(diff string) string {
	var out strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"),
			strings.HasPrefix(line, "--- "),
			strings.HasPrefix(line, "+++ "):
			out.WriteString(ansiBold)
			out.WriteString(line)
			out.WriteString(ansiReset)
		case strings.HasPrefix(line, "@@"):
			out.WriteString(ansiCyan)
			out.WriteString(line)
			out.WriteString(ansiReset)
		case strings.HasPrefix(line, "+"):
			out.WriteString(ansiGreen)
			out.WriteString(line)
			out.WriteString(ansiReset)
		case strings.HasPrefix(line, "-"):
			out.WriteString(ansiRed)
			out.WriteString(line)
			out.WriteString(ansiReset)
		default:
			out.WriteString(line)
		}
		out.WriteString("\n")
	}
	return out.String()
}
