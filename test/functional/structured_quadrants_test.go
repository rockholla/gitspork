//go:build functional || functional_docker

package functional

import (
	"encoding/json"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shared functional-tier upstream (simpleGitsporkYML) covers the
// prefer_upstream+YAML and prefer_downstream+JSON pair. The other two cells
// of the 2x2 — prefer_upstream+JSON and prefer_downstream+YAML — went
// unexercised at the compiled-binary tier. These tests fill in the gap so
// all four structured-merge cells have functional coverage.

const missingQuadrantsGitsporkYML = `shared_ownership:
  structured:
    prefer_upstream:
    - upstream-wins.json
    prefer_downstream:
    - downstream-wins.yaml
`

// TestIntegrate_structured_missingQuadrants_preferUpstreamJSON_and_preferDownstreamYAML
// exercises the two structured-merge cells that simpleGitsporkYML doesn't.
func TestIntegrate_structured_missingQuadrants_preferUpstreamJSON_and_preferDownstreamYAML(t *testing.T) {
	upstreamDir := NewUpstreamRepo(t, map[string]string{
		"upstream-wins.json": `{
  "shared": "from-upstream",
  "upstream_only": "u-value"
}`,
		"downstream-wins.yaml": "shared: from-upstream\nupstream_only: u-value\n",
	}, missingQuadrantsGitsporkYML)
	downstreamDir := NewDownstreamRepo(t)
	runner := resolveRunner(t, upstreamDir, downstreamDir)

	// Pre-seed the downstream with divergent content so the merge has
	// something to resolve.
	WriteFiles(t, downstreamDir, map[string]string{
		"upstream-wins.json":   `{"shared":"from-downstream","downstream_only":"d-value"}`,
		"downstream-wins.yaml": "shared: from-downstream\ndownstream_only: d-value\n",
	})

	args := []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}
	out, code := runner.Run(t, args, downstreamDir)
	require.Equal(t, 0, code, "integrate failed:\n%s", out)

	t.Run("prefer_upstream_JSON: upstream wins on collision, downstream-only survives", func(t *testing.T) {
		got := ReadFile(t, downstreamDir, "upstream-wins.json")
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &m))
		assert.Equal(t, "from-upstream", m["shared"],
			"upstream must win on shared_key collision under prefer_upstream+JSON")
		assert.Equal(t, "u-value", m["upstream_only"],
			"upstream-only JSON keys must land in downstream")
		assert.Equal(t, "d-value", m["downstream_only"],
			"downstream-only JSON keys must survive the merge — the whole point of shared-ownership")
	})

	t.Run("prefer_downstream_YAML: downstream wins on collision, upstream-only survives", func(t *testing.T) {
		got := ReadFile(t, downstreamDir, "downstream-wins.yaml")
		m := map[string]any{}
		require.NoError(t, yaml.Unmarshal([]byte(got), &m))
		assert.Equal(t, "from-downstream", m["shared"],
			"downstream must win on shared_key collision under prefer_downstream+YAML")
		assert.Equal(t, "d-value", m["downstream_only"],
			"downstream-only YAML keys survive (trivial: they were already there)")
		assert.Equal(t, "u-value", m["upstream_only"],
			"upstream-only YAML keys must still land in downstream — merge still lands upstream additions")
	})
}
