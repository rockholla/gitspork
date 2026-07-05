package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rockholla/gitspork/v2/internal/logutil"
	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	initPath, err := os.MkdirTemp("", "gitspork-tests")
	defer os.RemoveAll(initPath)
	assert.Nil(t, err)
	err = Init(initPath, logutil.New())
	assert.Nil(t, err)
	initedConfigBytes, err := os.ReadFile(filepath.Join(initPath, GitSporkConfigFileName))
	assert.Nil(t, err)
	assert.Contains(t, string(initedConfigBytes), gitSporkConfigHeader)
}
