package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	initPath, err := os.MkdirTemp("", "gitspork-tests")
	defer os.RemoveAll(initPath)
	assert.Nil(t, err)
	err = Init(initPath, "tests", NewLogger())
	assert.Nil(t, err)
	initedConfigBytes, err := os.ReadFile(filepath.Join(initPath, gitSporkConfigFileName))
	assert.Nil(t, err)
	assert.Contains(t, string(initedConfigBytes), gitSporkConfigHeader)
}
