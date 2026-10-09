package integrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

func Test_upstreamConfigFrom(t *testing.T) {
	cfg := upstreamConfigFrom(&config.GitSporkConfig{
		UpstreamOwned: []config.OwnedEntry{
			{Pattern: "{,**/}cloud-native/**"},
			{From: "templates/Makefile.tmpl", To: "Makefile"},
		},
		DownstreamOwned: []config.OwnedEntry{{Pattern: "iac/terraform/.gitignore"}},
		UpstreamOnly:    []string{"docs/**"},
		SharedOwnership: config.GitSporkConfigSharedOwnership{
			Merged: []string{".gitignore"},
			Structured: config.GitSporkConfigSharedOwnershipStructured{
				PreferUpstream:   []string{"a.json"},
				PreferDownstream: []string{"b.yaml"},
			},
		},
		Templated: []config.GitSporkConfigTemplated{
			{Template: "t1", Destination: "out/one.txt"},
			{Template: "t2", Destination: "out/two.txt", DownstreamOwned: true},
		},
	})

	assert.Equal(t, []string{"{,**/}cloud-native/**", "Makefile"}, cfg.UpstreamOwned, "a rename is listed by its downstream destination")
	assert.Equal(t, []string{"iac/terraform/.gitignore"}, cfg.DownstreamOwned)
	assert.Equal(t, []string{"docs/**"}, cfg.UpstreamOnly)
	assert.Equal(t, sdktypes.SharedOwnership{
		Merged:                     []string{".gitignore"},
		StructuredPreferUpstream:   []string{"a.json"},
		StructuredPreferDownstream: []string{"b.yaml"},
	}, cfg.SharedOwnership)
	assert.Equal(t, []sdktypes.TemplatedDestination{
		{Destination: "out/one.txt"},
		{Destination: "out/two.txt", DownstreamOwned: true},
	}, cfg.Templated)
}

func Test_upstreamConfigFrom_emptyListsAreNotNil(t *testing.T) {
	cfg := upstreamConfigFrom(&config.GitSporkConfig{})
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.UpstreamOwned)
	assert.NotNil(t, cfg.SharedOwnership.Merged)
	assert.NotNil(t, cfg.Templated)
}

func Test_upstreamConfigFrom_doesNotAliasTheParsedConfig(t *testing.T) {
	parsed := &config.GitSporkConfig{UpstreamOnly: []string{"docs/**"}}
	cfg := upstreamConfigFrom(parsed)
	cfg.UpstreamOnly[0] = "changed"
	assert.Equal(t, "docs/**", parsed.UpstreamOnly[0])
}
