package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_isRmRecursive(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: false},
		{name: "no flags, just path", args: []string{"docs/foo"}, want: false},

		{name: "-r alone", args: []string{"-r", "docs/foo"}, want: true},
		{name: "-rf combined", args: []string{"-rf", "docs/foo"}, want: true},
		{name: "-fr combined (reversed order)", args: []string{"-fr", "docs/foo"}, want: true},
		{name: "-rn combined with dry-run", args: []string{"-rn", "docs/foo"}, want: true},
		{name: "-rfn triple-bundle", args: []string{"-rfn", "docs/foo"}, want: true},

		{name: "-f alone (not recursive)", args: []string{"-f", "docs/foo"}, want: false},
		{name: "-n alone (not recursive)", args: []string{"-n", "docs/foo"}, want: false},
		{name: "-fq combined without r", args: []string{"-fq", "docs/foo"}, want: false},

		{name: "-- separator hides subsequent -r", args: []string{"--", "-r"}, want: false},
		{name: "flag before -- separator still counts", args: []string{"-r", "--", "docs/foo"}, want: true},

		{name: "path with r in name is not a flag", args: []string{"docs/references/thing.md"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isRmRecursive(tc.args))
		})
	}
}
