package internal

import (
	"fmt"
	"path/filepath"
)

// IntegrateLocal integrates one or more local upstream paths into the downstream.
func IntegrateLocal(opts *IntegrateLocalOptions) error {
	// Normalize: single UpstreamPath -> UpstreamPaths slice.
	if len(opts.UpstreamPaths) == 0 && opts.UpstreamPath != "" {
		opts.UpstreamPaths = []string{opts.UpstreamPath}
	}
	if len(opts.UpstreamPaths) == 0 {
		return fmt.Errorf("no upstream path specified: provide --upstream-path")
	}

	for _, upstreamPath := range opts.UpstreamPaths {
		opts.Logger.Log("parsing the gitspork config file at %s or %s",
			filepath.Join(upstreamPath, gitSporkConfigFileName),
			filepath.Join(upstreamPath, gitSporkConfigFileNameAlt))
		gitSporkConfig, err := getGitSporkConfig(upstreamPath)
		if err != nil {
			return err
		}
		if err := integrate(gitSporkConfig, upstreamPath, opts.DownstreamPath, opts.ForceRePrompt, false, opts.Logger); err != nil {
			return err
		}
	}
	return nil
}
