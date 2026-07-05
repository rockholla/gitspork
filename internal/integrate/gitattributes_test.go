package integrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureGitsporkAttributes_createsFileWhenNoneExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, ensureGitsporkAttributes(dir))

	got, err := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	require.NoError(t, err)
	assert.Contains(t, string(got), gitsporkAttrMarker)
	assert.Contains(t, string(got), gitsporkAttrPattern)
}

func TestEnsureGitsporkAttributes_noopWhenAlreadyCorrect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	require.NoError(t, ensureGitsporkAttributes(dir))
	firstInfo, err := os.Stat(path)
	require.NoError(t, err)
	firstMtime := firstInfo.ModTime()

	// wait a hair then re-run — mtime should not change if it's a true no-op
	require.NoError(t, ensureGitsporkAttributes(dir))
	secondInfo, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, firstMtime, secondInfo.ModTime(), "second run should not have rewritten the file")
}

func TestEnsureGitsporkAttributes_appendsWhenFileHasUnrelatedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	original := "*.md linguist-language=Markdown\n*.go text\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, ensureGitsporkAttributes(dir))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "*.md linguist-language=Markdown", "user's rules must survive")
	assert.Contains(t, string(got), "*.go text", "user's rules must survive")
	assert.Contains(t, string(got), gitsporkAttrPattern)
	assert.Contains(t, string(got), gitsporkAttrMarker)
}

func TestEnsureGitsporkAttributes_replacesStaleGitsporkLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	// simulate a previous gitspork version having written a different attribute set
	original := "# gitspork-managed: cache files under .gitspork/ are auto-generated\n" +
		".gitspork/**/*.json some=old flags\n" +
		"*.md linguist-language=Markdown\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, ensureGitsporkAttributes(dir))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "some=old flags", "stale attributes must be removed")
	assert.Contains(t, string(got), gitsporkAttrFlags, "current attributes must be present")
	assert.Contains(t, string(got), "*.md linguist-language=Markdown", "user's rules must survive")
	// only one gitspork block
	occurrences := 0
	for _, line := range splitLines(string(got)) {
		if len(line) > len(gitsporkAttrPattern) && line[:len(gitsporkAttrPattern)] == gitsporkAttrPattern {
			occurrences++
		}
	}
	assert.Equal(t, 1, occurrences, "must collapse to exactly one gitspork pattern line")
}

func TestEnsureGitsporkAttributes_handlesMissingTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	// no trailing newline on the existing file
	require.NoError(t, os.WriteFile(path, []byte("*.md text"), 0644))

	require.NoError(t, ensureGitsporkAttributes(dir))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "*.md text", "existing content preserved")
	assert.Contains(t, string(got), gitsporkAttrPattern)
	// existing content and gitspork block should be on separate lines
	assert.NotContains(t, string(got), "*.md text# gitspork", "must not concatenate onto the same line")
}

func TestEnsureGitsporkAttributes_collapsesDuplicateGitsporkLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	// simulate a broken prior state with the pattern line present twice
	original := gitsporkAttrPattern + " some=old\n" + gitsporkAttrPattern + " " + gitsporkAttrFlags + "\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, ensureGitsporkAttributes(dir))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	occurrences := 0
	for _, line := range splitLines(string(got)) {
		if len(line) > len(gitsporkAttrPattern) && line[:len(gitsporkAttrPattern)] == gitsporkAttrPattern {
			occurrences++
		}
	}
	assert.Equal(t, 1, occurrences, "must collapse to exactly one gitspork pattern line")
}

// splitLines splits by newline and drops the trailing empty entry produced by a trailing newline.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
