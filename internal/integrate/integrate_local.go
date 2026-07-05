package integrate

import (
	"fmt"
	"path/filepath"

	"github.com/rockholla/gitspork/internal/config"
	"github.com/rockholla/gitspork/internal/types"
)

// IntegrateLocal integrates one or more local upstream paths into the downstream.
func IntegrateLocal(opts *types.IntegrateLocalOptions) (*types.IntegrateResult, error) {
	result := &types.IntegrateResult{}

	if opts.Logger == nil {
		opts.Logger = types.NoopLogger()
	}

	if len(opts.UpstreamPaths) == 0 {
		return result, fmt.Errorf("no upstream path specified: set UpstreamPaths on IntegrateLocalOptions")
	}

	for _, upstreamPath := range opts.UpstreamPaths {
		opts.Logger.Log("parsing the gitspork config file at %s or %s",
			filepath.Join(upstreamPath, config.GitSporkConfigFileName),
			filepath.Join(upstreamPath, config.GitSporkConfigFileNameAlt))
		gitSporkConfig, err := getGitSporkConfig(upstreamPath)
		if err != nil {
			return result, err
		}
		if err := integrate(gitSporkConfig, upstreamPath, opts.DownstreamPath, opts.ForceRePrompt, false, opts.Logger); err != nil {
			return result, err
		}
		result.Upstreams = append(result.Upstreams, types.IntegratedUpstream{
			URL: upstreamPath, // local path recorded in URL slot; no CommitHash concept for local
		})
	}
	return result, nil
}
