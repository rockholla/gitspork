package logutil

import (
	"strings"

	"github.com/fatih/color"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/printer"
)

// ColorizeYAML applies syntax highlighting to a YAML string using the go-yaml
// lexer for token-accurate coloring of keys, string values, and comments.
// When color output is disabled (non-TTY or NO_COLOR), the string is returned
// unchanged.
func ColorizeYAML(src string) string {
	if color.NoColor {
		return src
	}
	p := printer.Printer{
		MapKey: func() *printer.Property {
			return &printer.Property{Prefix: "\x1b[96m", Suffix: "\x1b[0m"}
		},
		String: func() *printer.Property {
			return &printer.Property{Prefix: "\x1b[92m", Suffix: "\x1b[0m"}
		},
		Comment: func() *printer.Property {
			return &printer.Property{Prefix: "\x1b[2m", Suffix: "\x1b[0m"}
		},
	}
	tokens := lexer.Tokenize(src)
	return strings.TrimRight(p.PrintTokens(tokens), "\n")
}
