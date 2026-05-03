package internal

import (
	gogit "github.com/go-git/go-git/v6"
)

type upstreamRename struct {
	OldPath string
	NewPath string
}

type upstreamDelta struct {
	Deletions []string
	Renames   []upstreamRename
}

func computeUpstreamDelta(repo *gogit.Repository, prevHash, newHash string, config *GitSporkConfig, upstreamSubpath string) (*upstreamDelta, error) {
	delta := &upstreamDelta{}
	if prevHash == "" {
		return delta, nil
	}
	return delta, nil
}

func applyUpstreamDelta(delta *upstreamDelta, downstreamPath string, logger *Logger) error {
	return nil
}
