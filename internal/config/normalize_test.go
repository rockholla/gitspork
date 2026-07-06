package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_NormalizeUpstreamPath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// Empty / root
		{in: "", want: ""},
		{in: ".", want: ""},
		{in: "/", want: ""},

		// Already clean
		{in: "docs", want: "docs"},
		{in: "docs/foo.md", want: "docs/foo.md"},
		{in: "docs/foo/bar.md", want: "docs/foo/bar.md"},

		// Trailing slash (tab-completion)
		{in: "docs/", want: "docs"},
		{in: "docs/foo/", want: "docs/foo"},

		// Leading slash
		{in: "/docs", want: "docs"},
		{in: "/docs/foo", want: "docs/foo"},

		// Both
		{in: "/docs/", want: "docs"},

		// Doubled slashes
		{in: "docs//foo", want: "docs/foo"},
		{in: "docs///foo", want: "docs/foo"},

		// Explicit-cwd prefix
		{in: "./docs/foo", want: "docs/foo"},
		{in: "./docs", want: "docs"},

		// Interior "." and ".."
		{in: "docs/./foo", want: "docs/foo"},
		{in: "docs/bar/../foo", want: "docs/foo"},
		{in: "docs/foo/..", want: "docs"},

		// Combinations
		{in: "./docs/foo/", want: "docs/foo"},
		{in: "/docs//./foo/", want: "docs/foo"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeUpstreamPath(tc.in))
		})
	}
}
