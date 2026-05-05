//go:build functional || functional_docker

package functional

import "testing"

// simpleGitsporkYML is the shared upstream config used across integrate,
// check-drift, and any other scenario that needs a realistic upstream repo.
const simpleGitsporkYML = `upstream_owned:
- upstream-owned/**
- upstream-owned.mk
downstream_owned:
- downstream-owned.md
shared_ownership:
  merged:
  - Makefile
  structured:
    prefer_upstream:
    - config.yaml
    prefer_downstream:
    - info.json
templated:
- template: .gitspork-templates/meta.txt.go.tmpl
  destination: meta.txt
  inputs:
  - name: project_name
    json_data_path: input-data.json
  - name: project_description
    json_data_path: input-data.json
`

const metaTemplate = `Project: {{ index .Inputs "project_name" }}
Description: {{ index .Inputs "project_description" }}
`

// buildSimpleUpstream creates a temp upstream git repo with all ownership types
// (upstream-owned, downstream-owned, shared/merged, shared/structured, templated).
func buildSimpleUpstream(t *testing.T) string {
	t.Helper()
	return NewUpstreamRepo(t, map[string]string{
		"upstream-owned/file.txt":              "upstream content\n",
		"upstream-owned.mk":                    "upstream mk content\n",
		"downstream-owned.md":                  "downstream seed content\n",
		"Makefile":                             "# upstream makefile\n",
		"config.yaml":                          "key: upstream-value\n",
		"info.json":                            `{"version":"1"}`,
		".gitspork-templates/meta.txt.go.tmpl": metaTemplate,
	}, simpleGitsporkYML)
}

// prepDownstreamWithInputData writes the input-data.json file required by the
// templated entry in simpleGitsporkYML. Must be called before integrate and
// again before check-drift (which re-runs integrate internally).
func prepDownstreamWithInputData(t *testing.T, downstreamDir string) {
	t.Helper()
	WriteFiles(t, downstreamDir, map[string]string{
		"input-data.json": `{"project_name":"my-project","project_description":"my description"}`,
	})
}

// integrateArgs returns the standard integrate command args for the simple upstream.
func integrateArgs(upstreamDir, downstreamDir string) []string {
	return []string{
		"integrate",
		"--upstream-repo-url", "file://" + upstreamDir,
		"--upstream-repo-version", "main",
		"--downstream-repo-path", downstreamDir,
	}
}
