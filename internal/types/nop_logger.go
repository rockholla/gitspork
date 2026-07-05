package types

// nopLogger is a Logger implementation whose methods do nothing. It exists so
// entry-point functions can normalize a nil Logger into a non-nil no-op
// without every downstream callsite having to guard.
type nopLogger struct{}

func (nopLogger) Log(string, ...any)   {}
func (nopLogger) Error(string, ...any) {}

// NopLogger returns a Logger that discards all output.
func NopLogger() Logger { return nopLogger{} }
