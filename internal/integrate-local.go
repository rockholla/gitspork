package internal

import "path/filepath"

// Integrate will ensure that the localRepoPath is integrated/re-integrated w/ the upstreamRepoURL version
func IntegrateLocal(opts *IntegrateLocalOptions) error {
	opts.Logger.Log("parsing the gitspork config file at %s or %s", filepath.Join(opts.UpstreamPath, gitSporkConfigFileName), filepath.Join(opts.UpstreamPath, gitSporkConfigFileNameAlt))
	gitSporkConfig, err := getGitSporkConfig(opts.UpstreamPath)
	if err != nil {
		return err
	}

	return integrate(gitSporkConfig, opts.UpstreamPath, opts.DownstreamPath, opts.ForceRePrompt, opts.Logger)
}
