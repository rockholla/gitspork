package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseUpstreamOwnedEntry(t *testing.T) {
	tests := []struct {
		name        string
		entry       string
		wantPattern string
		wantDest    string
	}{
		{
			name:        "simple pattern without rename",
			entry:       "file.txt",
			wantPattern: "file.txt",
			wantDest:    "file.txt",
		},
		{
			name:        "pattern with rename syntax",
			entry:       "source.txt=>destination.txt",
			wantPattern: "source.txt",
			wantDest:    "destination.txt",
		},
		{
			name:        "glob pattern without rename",
			entry:       "docs/**/*.md",
			wantPattern: "docs/**/*.md",
			wantDest:    "docs/**/*.md",
		},
		{
			name:        "glob pattern with rename",
			entry:       ".config-upstream.json=>.config.json",
			wantPattern: ".config-upstream.json",
			wantDest:    ".config.json",
		},
		{
			name:        "path with multiple arrows uses only first",
			entry:       "source=>dest=>extra",
			wantPattern: "source",
			wantDest:    "dest=>extra",
		},
		{
			name:        "path with colon is treated as simple pattern",
			entry:       "path:with:colons",
			wantPattern: "path:with:colons",
			wantDest:    "path:with:colons",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPattern, gotDest := parseUpstreamOwnedEntry(tt.entry)
			assert.Equal(t, tt.wantPattern, gotPattern, "pattern mismatch")
			assert.Equal(t, tt.wantDest, gotDest, "destination mismatch")
		})
	}
}
