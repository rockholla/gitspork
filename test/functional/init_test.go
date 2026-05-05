//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()
	runner := resolveRunner(t, dir, "")

	out, code := runner.Run(t, []string{"init", "--path", dir}, dir)
	require.Equal(t, 0, code, "init exited non-zero:\n%s", out)

	content := ReadFile(t, dir, ".gitspork.yml")
	assert.Contains(t, content, "# For full docs")
	assert.Contains(t, content, "upstream_owned")
}
