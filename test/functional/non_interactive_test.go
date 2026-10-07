//go:build functional || functional_docker

package functional

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const promptUpstreamYML = `templated:
- template: greeting.txt.tmpl
  destination: greeting.txt
  inputs:
  - name: name
    prompt: "Who to greet?"
    prompt_default:
      value: world
`

// TestNonInteractive covers --non-interactive on both commands: prompts take their
// default without being shown, and the flag is rejected alongside --force-re-prompt.
func TestNonInteractive(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(upstream, downstream string) []string
	}{
		{"integrate", func(u, d string) []string {
			return []string{"integrate", "--upstream-repo-url", "file://" + u, "--upstream-repo-version", "main", "--downstream-repo-path", d}
		}},
		{"integrate-local", func(u, d string) []string {
			return []string{"integrate-local", "--upstream-path", u, "--downstream-path", d}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstreamDir := NewUpstreamRepo(t, map[string]string{"greeting.txt.tmpl": `Hello, {{ index .Inputs "name" }}!`}, promptUpstreamYML)
			downstreamDir := NewDownstreamRepo(t)
			runner := resolveRunner(t, upstreamDir, downstreamDir)

			out, code := runner.Run(t, append(tc.args(upstreamDir, downstreamDir), "--non-interactive"), downstreamDir)
			require.Equal(t, 0, code, "%s --non-interactive failed:\n%s", tc.name, out)
			assert.NotContains(t, out, "Who to greet?", "the prompt must not be shown")
			AssertFileContains(t, downstreamDir, "greeting.txt", "Hello, world!")

			out, code = runner.Run(t, append(tc.args(upstreamDir, downstreamDir), "--non-interactive", "--force-re-prompt"), downstreamDir)
			assert.NotEqual(t, 0, code)
			assert.Contains(t, out, "cannot be combined")
		})
	}
}
