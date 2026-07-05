package logutil

import (
	"strings"

	"github.com/rockholla/gitspork/internal/types"
)

// LoggerWriter is an io.Writer that forwards each line written to it to a
// types.Logger. Trailing newlines are trimmed since Logger implementations
// handle line boundaries themselves. If L is nil, Write accepts bytes and
// discards them silently — matching the types.Logger "nil is silent"
// contract for callers that pass this writer through third-party APIs
// (like go-git's clone Progress).
type LoggerWriter struct {
	L types.Logger
}

// Write splits input on newlines and calls L.Log for each non-empty line.
func (w *LoggerWriter) Write(p []byte) (int, error) {
	if w == nil || w.L == nil {
		return len(p), nil
	}
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	for _, line := range strings.Split(text, "\n") {
		w.L.Log("%s", line)
	}
	return len(p), nil
}
