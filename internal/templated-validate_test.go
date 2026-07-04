package internal

// Verifies fail-fast validation of templated config: the structured-merge
// preference enum and the exactly-one-of-source invariant on inputs.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuredMergePreference_Validate(t *testing.T) {
	require.NoError(t, StructuredMergePreference("").Validate())
	require.NoError(t, StructuredMergePreferUpstream.Validate())
	require.NoError(t, StructuredMergePreferDownstream.Validate())
	require.Error(t, StructuredMergePreference("prefer-sideways").Validate())
}

func TestGitSporkConfigTemplatedInput_Validate(t *testing.T) {
	// valid: exactly one source
	require.NoError(t, GitSporkConfigTemplatedInput{Name: "a", Prompt: "?"}.Validate())
	require.NoError(t, GitSporkConfigTemplatedInput{Name: "a", JSONDataPath: "d.json"}.Validate())
	require.NoError(t, GitSporkConfigTemplatedInput{Name: "a", PreviousInput: &GitSporkConfigTemplatedInputPrevious{Template: "t", Name: "n"}}.Validate())

	// invalid: no source
	require.Error(t, GitSporkConfigTemplatedInput{Name: "a"}.Validate())
	// invalid: two sources
	require.Error(t, GitSporkConfigTemplatedInput{Name: "a", Prompt: "?", JSONDataPath: "d.json"}.Validate())
}

func TestGitSporkConfigTemplated_Validate(t *testing.T) {
	// valid: good merge preference + one-of input
	require.NoError(t, GitSporkConfigTemplated{
		Merged: &GitSporkConfigTemplatedMerged{Structured: StructuredMergePreferUpstream},
		Inputs: []GitSporkConfigTemplatedInput{{Name: "a", Prompt: "?"}},
	}.Validate())

	// invalid: bad merge preference
	require.Error(t, GitSporkConfigTemplated{
		Merged: &GitSporkConfigTemplatedMerged{Structured: StructuredMergePreference("nope")},
	}.Validate())

	// invalid: input with no source
	require.Error(t, GitSporkConfigTemplated{
		Inputs: []GitSporkConfigTemplatedInput{{Name: "a"}},
	}.Validate())
}
