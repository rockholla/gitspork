package types

// noopLogger is a Logger implementation whose methods do nothing. It exists so
// entry-point functions can normalize a nil Logger into a non-nil no-op
// without every downstream callsite having to guard.
type noopLogger struct{}

func (noopLogger) Log(string, ...any)   {}
func (noopLogger) Error(string, ...any) {}

// NoopLogger returns a Logger that discards all output.
func NoopLogger() Logger { return noopLogger{} }
