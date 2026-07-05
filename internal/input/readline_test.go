package input

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLineBuffer_insert(t *testing.T) {
	b := &lineBuffer{}
	for _, r := range "hello" {
		b.insert(r)
	}
	assert.Equal(t, "hello", string(b.runes))
	assert.Equal(t, 5, b.pos)
}

func TestLineBuffer_insertInMiddle(t *testing.T) {
	b := &lineBuffer{runes: []rune("hlo"), pos: 1}
	b.insert('e')
	b.insert('l')
	assert.Equal(t, "hello", string(b.runes))
	assert.Equal(t, 3, b.pos)
}

func TestLineBuffer_backspace(t *testing.T) {
	b := &lineBuffer{runes: []rune("hello"), pos: 5}
	b.backspace()
	assert.Equal(t, "hell", string(b.runes))
	assert.Equal(t, 4, b.pos)
}

func TestLineBuffer_backspaceAtStart(t *testing.T) {
	b := &lineBuffer{runes: []rune("hello"), pos: 0}
	b.backspace()
	assert.Equal(t, "hello", string(b.runes), "backspace at pos 0 is a no-op")
	assert.Equal(t, 0, b.pos)
}

func TestLineBuffer_backspaceInMiddle(t *testing.T) {
	b := &lineBuffer{runes: []rune("hello"), pos: 3}
	b.backspace()
	assert.Equal(t, "helo", string(b.runes))
	assert.Equal(t, 2, b.pos)
}

func TestLineBuffer_moveLeftRight(t *testing.T) {
	b := &lineBuffer{runes: []rune("abc"), pos: 3}
	b.moveLeft()
	b.moveLeft()
	assert.Equal(t, 1, b.pos)
	b.moveRight()
	assert.Equal(t, 2, b.pos)
	// Bounds are respected.
	for i := 0; i < 5; i++ {
		b.moveRight()
	}
	assert.Equal(t, 3, b.pos)
	for i := 0; i < 5; i++ {
		b.moveLeft()
	}
	assert.Equal(t, 0, b.pos)
}

func TestLineBuffer_homeAndEnd(t *testing.T) {
	b := &lineBuffer{runes: []rune("hello"), pos: 3}
	b.moveHome()
	assert.Equal(t, 0, b.pos)
	b.moveEnd()
	assert.Equal(t, 5, b.pos)
}

func TestLineBuffer_reset(t *testing.T) {
	b := &lineBuffer{runes: []rune("hello"), pos: 3}
	b.reset()
	assert.Empty(t, b.runes)
	assert.Equal(t, 0, b.pos)
}

func TestLineBuffer_replaceRange_atEnd(t *testing.T) {
	b := &lineBuffer{runes: []rune("./co"), pos: 4}
	b.replaceRange(2, 4, []rune("configs/"))
	assert.Equal(t, "./configs/", string(b.runes))
	assert.Equal(t, 10, b.pos, "cursor should be at end of inserted text")
}

func TestLineBuffer_replaceRange_middle(t *testing.T) {
	b := &lineBuffer{runes: []rune("say ./co and go"), pos: 8}
	b.replaceRange(4, 8, []rune("./configs/"))
	assert.Equal(t, "say ./configs/ and go", string(b.runes))
	assert.Equal(t, 14, b.pos)
}

func TestLineBuffer_replaceRange_boundsClamped(t *testing.T) {
	b := &lineBuffer{runes: []rune("ab"), pos: 0}
	b.replaceRange(-5, 100, []rune("XY"))
	assert.Equal(t, "XY", string(b.runes))
	assert.Equal(t, 2, b.pos)
}

func TestExtractPathToken_wholeLineIsToken(t *testing.T) {
	start, prefix := extractPathToken([]rune("./configs/db"), 12)
	assert.Equal(t, 0, start)
	assert.Equal(t, "./configs/db", prefix)
}

func TestExtractPathToken_afterSpace(t *testing.T) {
	// Cursor after "some ./co"
	start, prefix := extractPathToken([]rune("some ./co"), 9)
	assert.Equal(t, 5, start)
	assert.Equal(t, "./co", prefix)
}

func TestExtractPathToken_emptyWhenCursorAtStart(t *testing.T) {
	start, prefix := extractPathToken([]rune("hello"), 0)
	assert.Equal(t, 0, start)
	assert.Equal(t, "", prefix)
}

func TestExtractPathToken_cursorAtSpace(t *testing.T) {
	// Cursor just after a space with no token yet
	start, prefix := extractPathToken([]rune("hi "), 3)
	assert.Equal(t, 3, start)
	assert.Equal(t, "", prefix)
}

func TestExtractPathToken_cursorOutOfRange(t *testing.T) {
	// Cursor beyond len is clamped to len.
	start, prefix := extractPathToken([]rune("abc"), 100)
	assert.Equal(t, 0, start)
	assert.Equal(t, "abc", prefix)
	// Negative cursor is clamped to 0.
	start, prefix = extractPathToken([]rune("abc"), -3)
	assert.Equal(t, 0, start)
	assert.Equal(t, "", prefix)
}

func TestCommonPrefix_empty(t *testing.T) {
	assert.Equal(t, "", commonPrefix(nil))
	assert.Equal(t, "", commonPrefix([]string{}))
}

func TestCommonPrefix_singleton(t *testing.T) {
	assert.Equal(t, "abc", commonPrefix([]string{"abc"}))
}

func TestCommonPrefix_shared(t *testing.T) {
	assert.Equal(t, "config", commonPrefix([]string{"configs/", "config.yaml", "configuration"}))
}

func TestCommonPrefix_none(t *testing.T) {
	assert.Equal(t, "", commonPrefix([]string{"apple", "banana"}))
}

func TestCommonPrefix_oneIsPrefixOfOther(t *testing.T) {
	assert.Equal(t, "abc", commonPrefix([]string{"abc", "abcdef"}))
}

func TestPathCompleter_emptyPrefixListsCwd(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("a"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(dir))

	got := pathCompleter("")
	sort.Strings(got)
	assert.Equal(t, []string{"alpha.txt", "sub/"}, got, "directory entries should carry a trailing slash")
}

func TestPathCompleter_prefixGlob(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("c"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("u"), 0644))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(dir))

	got := pathCompleter("conf")
	sort.Strings(got)
	assert.Equal(t, []string{"config.yaml", "configs/"}, got)
}

func TestPathCompleter_noMatchesReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(dir))

	got := pathCompleter("nonexistent-prefix-")
	assert.Empty(t, got)
}
