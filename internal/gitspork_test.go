package internal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntegrate(t *testing.T) {
	_, thisTestFile, _, _ := runtime.Caller(0)

	t.Run("simple", func(t *testing.T) {
		simpleDownstreamPath, err := filepath.Abs(filepath.Join(filepath.Dir(thisTestFile), "..", "docs", "examples", "simple", "downstream"))
		assert.Nil(t, err)
		err = os.Mkdir(simpleDownstreamPath, 0755)
		defer os.RemoveAll(simpleDownstreamPath)
		assert.Nil(t, err)
		err = Integrate(&IntegrateOptions{
			Logger:              NewLogger(),
			UpstreamRepoURL:     "git@github.com:rockholla/gitspork.git",
			UpstreamRepoVersion: "work",
			UpstreamRepoSubpath: "docs/examples/simple/upstream",
			DownstreamRepoPath:  simpleDownstreamPath,
		})
		assert.Nil(t, err)
	})
}
