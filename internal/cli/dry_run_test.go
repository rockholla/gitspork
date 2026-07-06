package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_isDryRun(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: false},
		{name: "no flags", args: []string{"docs/foo.md"}, want: false},

		{name: "-n alone", args: []string{"-n", "docs/foo.md"}, want: true},
		{name: "--dry-run long form", args: []string{"--dry-run", "docs/foo.md"}, want: true},
		{name: "-nf bundled", args: []string{"-nf", "docs/foo.md"}, want: true},
		{name: "-rn bundled with recursive", args: []string{"-rn", "docs/foo"}, want: true},
		{name: "-rfn triple-bundle", args: []string{"-rfn", "docs/foo"}, want: true},

		{name: "-r alone (not dry-run)", args: []string{"-r", "docs/foo"}, want: false},
		{name: "-f alone (not dry-run)", args: []string{"-f", "docs/foo"}, want: false},
		{name: "-fq combined without n", args: []string{"-fq", "docs/foo"}, want: false},
		{name: "--force long form", args: []string{"--force", "docs/foo"}, want: false},

		{name: "-- separator hides subsequent -n", args: []string{"--", "-n"}, want: false},
		{name: "flag before -- separator still counts", args: []string{"-n", "--", "docs/foo"}, want: true},

		{name: "path with n in name is not a flag", args: []string{"docs/notes.md"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isDryRun(tc.args))
		})
	}
}
