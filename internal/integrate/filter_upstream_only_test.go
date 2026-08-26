package integrate

import (
	"fmt"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/sdktypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingLogger struct {
	logs []string
}

func (l *recordingLogger) Log(msg string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf(msg, args...))
}

func (l *recordingLogger) Error(msg string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf(msg, args...))
}

func Test_filterUpstreamOnly_noPatterns(t *testing.T) {
	files := []string{"a/b.txt", "c/d.txt"}
	got, err := filterUpstreamOnly(files, nil, sdktypes.NoopLogger())
	require.NoError(t, err)
	assert.Equal(t, files, got)
}

func Test_filterUpstreamOnly_excludesSomeFiles(t *testing.T) {
	files := []string{"cli/foo.txt", "lib/bar.txt", "cli/.cloud-native-template/baz.txt"}
	rl := &recordingLogger{}
	got, err := filterUpstreamOnly(files, []string{"cli/**"}, rl)
	require.NoError(t, err)
	assert.Equal(t, []string{"lib/bar.txt"}, got)
	assert.Len(t, rl.logs, 2, "expected a warning for each excluded file")
	assert.Contains(t, rl.logs[0], "cli/foo.txt")
	assert.Contains(t, rl.logs[1], "cli/.cloud-native-template/baz.txt")
}

func Test_filterUpstreamOnly_invalidPattern(t *testing.T) {
	_, err := filterUpstreamOnly([]string{"a.txt"}, []string{"["}, sdktypes.NoopLogger())
	assert.Error(t, err)
}

func Test_filterUpstreamOnly_patternMatchesNothing(t *testing.T) {
	files := []string{"a/b.txt", "c/d.txt"}
	got, err := filterUpstreamOnly(files, []string{"nomatch/**"}, sdktypes.NoopLogger())
	require.NoError(t, err)
	assert.Equal(t, files, got)
}
