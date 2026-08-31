package integrate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegrateLocal integrates one or more local upstream paths (UpstreamPaths)
// and/or in-memory filesystems (UpstreamFSes) into the downstream. Paths are
// processed first, then FSes. Each FS is materialized to a temporary directory
// internally and cleaned up on return — no temporary files are visible to the
// caller.
func IntegrateLocal(opts *sdktypes.IntegrateLocalOptions) (*sdktypes.IntegrateResult, error) {
	result := &sdktypes.IntegrateResult{}

	if opts.Logger == nil {
		opts.Logger = sdktypes.NoopLogger()
	}

	if len(opts.UpstreamPaths) == 0 && len(opts.UpstreamFSes) == 0 {
		return result, fmt.Errorf("no upstream specified: set UpstreamPaths or UpstreamFSes on IntegrateLocalOptions")
	}

	// Materialize each fs.FS to a temp dir so the path-based integration
	// pipeline can process it unchanged. All temp dirs are removed on return.
	upstreamPaths := opts.UpstreamPaths
	var tempDirs []string
	defer func() {
		for _, dir := range tempDirs {
			_ = os.RemoveAll(dir)
		}
	}()
	for _, fsys := range opts.UpstreamFSes {
		dir, err := materializeFS(fsys)
		if err != nil {
			return result, fmt.Errorf("materializing upstream FS: %w", err)
		}
		tempDirs = append(tempDirs, dir)
		upstreamPaths = append(upstreamPaths, dir)
	}

	for _, upstreamPath := range upstreamPaths {
		if err := EnsureNotSelfIntegration(opts.DownstreamPath, "", upstreamPath); err != nil {
			return result, err
		}
		opts.Logger.Log("parsing the gitspork config file at %s or %s",
			filepath.Join(upstreamPath, config.GitSporkConfigFileName),
			filepath.Join(upstreamPath, config.GitSporkConfigFileNameAlt))
		gitSporkConfig, err := getGitSporkConfig(upstreamPath)
		if err != nil {
			return result, err
		}
		if err := integrate(gitSporkConfig, upstreamPath, opts.DownstreamPath, opts.ForceRePrompt, false, opts.Logger, opts.SeedInputs); err != nil {
			return result, err
		}
		result.Upstreams = append(result.Upstreams, sdktypes.IntegratedUpstream{
			URL: upstreamPath, // local path (or materialized temp dir) in URL slot; no CommitHash for local
		})
	}
	return result, nil
}

// materializeFS writes the full content of fsys into a new temporary directory
// and returns its path. The caller owns the directory and must remove it when
// done; materializeFS removes it on its own errors.
func materializeFS(fsys fs.FS) (string, error) {
	dir, err := os.MkdirTemp("", "gitspork-upstream-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("creating parent dir for %s: %w", dest, err)
		}
		return os.WriteFile(dest, data, info.Mode().Perm())
	}); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("writing FS to temp dir: %w", err)
	}
	return dir, nil
}
