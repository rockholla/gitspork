package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGitSporkConfig_choicesRejectsDefaultOutsideChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitspork.yml")
	require.NoError(t, os.WriteFile(path, []byte(`templated:
- template: t.tmpl
  destination: t.txt
  inputs:
  - name: scheduledJob
    prompt: "Enable the scheduled job?"
    choices: ["true", "false"]
    prompt_default:
      value: "yes"
`), 0644))
	_, err := ParseGitSporkConfig(path)
	assert.ErrorContains(t, err, `prompt_default.value "yes" is not one of choices`)
}
