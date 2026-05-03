package internal

import (
	"testing"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_computeUpstreamDelta(t *testing.T) {
	t.Run("returns empty delta when prevHash is empty", func(t *testing.T) {
		repo, err := gogit.Init(memory.NewStorage(), nil)
		require.NoError(t, err)
		delta, err := computeUpstreamDelta(repo, "", "abc123", &GitSporkConfig{}, "")
		require.NoError(t, err)
		assert.Empty(t, delta.Deletions)
		assert.Empty(t, delta.Renames)
	})
}
