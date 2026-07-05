package input

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/term"
)

// readLine reads a single line of interactive input from /dev/tty with
// filesystem-path tab completion. Line editing supports insertion, backspace,
// delete, left/right arrow, home/end (via Ctrl+A / Ctrl+E), and Ctrl+U to
// clear the line. Enter submits. Ctrl+C exits the process cleanly (matching
// input.go's existing SIGINT-handler semantics).
//
// The visible prompt is styled as the same orange arrow the previous bash
// implementation used: `➡️ <prompt> `.
func readLine(prompt string) (string, error) {
	return runLineEditor(prompt, pathCompleter)
}

// completerFunc returns candidate completions for a prefix. Directory matches
// carry a trailing "/" so a follow-up tab keeps descending. An empty prefix
// completes against the current working directory.
type completerFunc func(prefix string) []string

// runLineEditor is the exported-through-readLine entry point split out for
// testing seams: the terminal I/O uses runLineEditor with pathCompleter, but
// pure-logic tests can drive individual state-machine helpers directly.
func runLineEditor(prompt string, completer completerFunc) (string, error) {
	tty, err := term.Open("/dev/tty")
	if err != nil {
		return "", err
	}
	defer tty.Close()

	if err := term.RawMode(tty); err != nil {
		return "", err
	}
	defer func() { _ = tty.Restore() }()

	// Print the styled prompt. Kept identical in look to the previous
	// bash `read -e -p "➡️ <colored>%s <reset>"` shape.
	const orange = "\033[38;2;255;166;0m"
	const reset = "\033[0m"
	fmt.Fprintf(os.Stdout, "➡️  %s%s%s ", orange, prompt, reset)

	buf := &lineBuffer{}
	reader := bufio.NewReader(tty)
	lastKeyWasTab := false

	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(os.Stdout)
				return string(buf.runes), nil
			}
			return "", err
		}

		thisKeyIsTab := b == '\t'

		switch {
		case b == 0x03: // Ctrl+C
			_ = tty.Restore()
			fmt.Fprintln(os.Stdout)
			os.Exit(0)
		case b == '\r' || b == '\n':
			fmt.Fprintln(os.Stdout)
			return strings.TrimSpace(string(buf.runes)), nil
		case b == 0x7f || b == 0x08: // backspace / DEL
			buf.backspace()
			redraw(prompt, buf)
		case b == 0x15: // Ctrl+U — clear line
			buf.reset()
			redraw(prompt, buf)
		case b == 0x01: // Ctrl+A — home
			buf.moveHome()
			redraw(prompt, buf)
		case b == 0x05: // Ctrl+E — end
			buf.moveEnd()
			redraw(prompt, buf)
		case b == '\t':
			// Two consecutive Tabs list all matches when the common
			// prefix has already been inserted.
			handleTab(prompt, buf, completer, lastKeyWasTab)
		case b == 0x1b: // ESC — read the rest of the escape sequence
			handleEscapeSequence(reader, buf)
			redraw(prompt, buf)
		default:
			if b >= 0x20 && b < 0x7f {
				buf.insert(rune(b))
				redraw(prompt, buf)
			}
			// Non-printable bytes are silently ignored.
		}

		lastKeyWasTab = thisKeyIsTab
	}
}

// handleTab implements the tab-completion state machine:
//   - no matches → beep (silent no-op)
//   - one match → insert it (replacing the token before the cursor)
//   - many matches, no common-prefix extension → on the second consecutive
//     Tab, list all matches; otherwise stay quiet
//   - many matches, common-prefix extension → insert the common prefix
func handleTab(prompt string, buf *lineBuffer, completer completerFunc, lastKeyWasTab bool) {
	tokenStart, prefix := extractPathToken(buf.runes, buf.pos)
	matches := completer(prefix)
	switch {
	case len(matches) == 0:
		return
	case len(matches) == 1:
		buf.replaceRange(tokenStart, buf.pos, []rune(matches[0]))
		redraw(prompt, buf)
	default:
		cp := commonPrefix(matches)
		if len(cp) > len(prefix) {
			buf.replaceRange(tokenStart, buf.pos, []rune(cp))
			redraw(prompt, buf)
			return
		}
		if lastKeyWasTab {
			// List candidates on their own line, then reprint the
			// prompt and current buffer on the next line.
			fmt.Fprintln(os.Stdout)
			for _, m := range matches {
				fmt.Fprintln(os.Stdout, m)
			}
			redraw(prompt, buf)
		}
	}
}

// handleEscapeSequence reads the two bytes following an ESC and dispatches
// arrow keys / home / end. Unknown sequences are silently swallowed.
func handleEscapeSequence(r *bufio.Reader, buf *lineBuffer) {
	b1, err := r.ReadByte()
	if err != nil {
		return
	}
	if b1 != '[' && b1 != 'O' {
		return
	}
	b2, err := r.ReadByte()
	if err != nil {
		return
	}
	switch b2 {
	case 'C':
		buf.moveRight()
	case 'D':
		buf.moveLeft()
	case 'H':
		buf.moveHome()
	case 'F':
		buf.moveEnd()
		// 'A' (up) and 'B' (down) are silently ignored — no history support.
	}
}

// redraw clears the current line and reprints the prompt + buffer. When the
// cursor is not at the end of the buffer, it moves left by the difference.
// Assumes the current input fits on a single terminal line — long inputs
// that wrap will display correctly but the cursor-positioning math becomes
// approximate.
func redraw(prompt string, buf *lineBuffer) {
	const orange = "\033[38;2;255;166;0m"
	const reset = "\033[0m"
	// \r moves to column 0; \033[K clears to end of line.
	fmt.Fprintf(os.Stdout, "\r\033[K➡️  %s%s%s %s", orange, prompt, reset, string(buf.runes))
	if buf.pos < len(buf.runes) {
		fmt.Fprintf(os.Stdout, "\033[%dD", len(buf.runes)-buf.pos)
	}
}

// lineBuffer is a pure editing buffer with no terminal knowledge. All state
// mutation goes through methods; tests exercise these directly without a TTY.
type lineBuffer struct {
	runes []rune
	pos   int // 0..len(runes)
}

func (b *lineBuffer) insert(r rune) {
	b.runes = append(b.runes[:b.pos], append([]rune{r}, b.runes[b.pos:]...)...)
	b.pos++
}

func (b *lineBuffer) backspace() {
	if b.pos == 0 {
		return
	}
	b.runes = append(b.runes[:b.pos-1], b.runes[b.pos:]...)
	b.pos--
}

func (b *lineBuffer) moveLeft() {
	if b.pos > 0 {
		b.pos--
	}
}

func (b *lineBuffer) moveRight() {
	if b.pos < len(b.runes) {
		b.pos++
	}
}

func (b *lineBuffer) moveHome() { b.pos = 0 }
func (b *lineBuffer) moveEnd()  { b.pos = len(b.runes) }

func (b *lineBuffer) reset() {
	b.runes = nil
	b.pos = 0
}

// replaceRange substitutes runes[start:end) with replacement, moving the
// cursor to the end of the inserted text.
func (b *lineBuffer) replaceRange(start, end int, replacement []rune) {
	if start < 0 {
		start = 0
	}
	if end > len(b.runes) {
		end = len(b.runes)
	}
	if start > end {
		start = end
	}
	tail := append([]rune{}, b.runes[end:]...)
	b.runes = append(append(b.runes[:start], replacement...), tail...)
	b.pos = start + len(replacement)
}

// extractPathToken returns (start, prefix) for the path-like token ending at
// the cursor. The token starts at the byte after the last whitespace before
// the cursor (or at 0 if there's no whitespace).
func extractPathToken(runes []rune, cursor int) (int, string) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	start := 0
	for i := cursor - 1; i >= 0; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			start = i + 1
			break
		}
	}
	return start, string(runes[start:cursor])
}

// commonPrefix returns the longest string that is a prefix of every input.
// Empty when strs is empty or has no shared prefix.
func commonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// pathCompleter is the default completerFunc for readLine. It matches
// filesystem paths matching `<prefix>*` (glob semantics), appending a trailing
// slash to directory matches so a follow-up tab descends into the directory.
// Empty prefix lists the entries of the current working directory.
func pathCompleter(prefix string) []string {
	if prefix == "" {
		entries, err := os.ReadDir(".")
		if err != nil {
			return nil
		}
		out := make([]string, 0, len(entries))
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			out = append(out, name)
		}
		return out
	}
	matches, err := filepath.Glob(prefix + "*")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && info.IsDir() {
			out = append(out, m+"/")
		} else {
			out = append(out, m)
		}
	}
	return out
}
