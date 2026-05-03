package internal

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func Test_globNonWildcardPrefix(t *testing.T) {
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/**"))
	assert.Equal(t, "docs/cloud-native", globNonWildcardPrefix("docs/cloud-native/*.md"))
	assert.Equal(t, "", globNonWildcardPrefix("**/cloud-native/*.md"))
	assert.Equal(t, "exact/path.md", globNonWildcardPrefix("exact/path.md"))
}
