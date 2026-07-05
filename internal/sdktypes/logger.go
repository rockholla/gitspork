package sdktypes

// Logger is the small interface gitspork uses for narration/progress and
// error messages. It is deliberately narrow so SDK consumers can wire their
// own logging (slog, zap, log/logr) with minimal glue. A nil Logger means
// silent — implementations that accept Logger as a field MUST check for nil
// before calling either method.
type Logger interface {
	Log(msg string, args ...any)
	Error(msg string, args ...any)
}
