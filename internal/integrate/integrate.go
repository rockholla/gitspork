package integrate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/gobwas/glob"
	"github.com/rockholla/gitspork/v2/internal/config"
	"github.com/rockholla/gitspork/v2/internal/logutil"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

const (
	structuredDataTypeYAML   string = "yaml"
	structuredDataTypeJSON   string = "json"
	preIntegrateMigrationID  string = "pre_integrate"
	postIntegrateMigrationID string = "post_integrate"
	gitSporkMetaDirName      string = ".gitspork"
	downstreamStateFileName  string = "downstream-state.json"
)

var (
	structuredDataYAMLExtensions []string = []string{".yaml", ".yml"}
	structuredDataJSONExtensions []string = []string{".json"}
	reSSHURL                               = regexp.MustCompile(`^git@([^:]+):(.+)$`)
	reHTTPProto                            = regexp.MustCompile(`^https?://`)
	// commitHashRe matches short (7-char) through full (40-char) git commit hashes.
	commitHashRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
)

// internalRequest carries wiring needed by integrateOneInternal that is not
// part of the public IntegrateOptions surface. It exists to keep the SDK's
// IntegrateOptions minimal while still allowing drift-check to signal special
// behavior.
type internalRequest struct {
	Logger                 sdktypes.Logger
	DownstreamRepoPath     string
	ForceRePrompt          bool
	forDriftCheck          bool   // true = skip state write, skip delta
	upstreamCommit         string // when forDriftCheck: the pinned commit
	prevUpstreamCommitHash string // set by integrateOne between calls
}

// Integrator is implemented by the ownership integrators that process a
// homogeneous list of items into the downstream: T is OwnedEntry for the
// upstream_owned/downstream_owned lists and string (glob pattern) for the
// shared_ownership lists. Each integrator file carries a compile-time
// `var _ Integrator[T] = (*Type)(nil)` assertion, so a newly added integrator
// that diverges from this contract fails to build.
type Integrator[T any] interface {
	Integrate(items []T, upstreamPath string, downstreamPath string, logger sdktypes.Logger) error
}

// TemplatedIntegrator is kept separate from Integrator[T] because rendering
// templates additionally needs the forceRePrompt flag to drive input
// collection, so its Integrate signature cannot match the generic contract.
type TemplatedIntegrator interface {
	Integrate(instructions []config.GitSporkConfigTemplated, upstreamPath string, downstreamPath string, forceRePrompt bool, logger sdktypes.Logger) error
}

// NormalizeUpstreamURL returns a canonical key for an upstream URL+subpath pair
// so that SSH and HTTPS forms of the same repo compare equal. Used to look up
// stored state entries regardless of how the upstream URL was originally spelled.
func NormalizeUpstreamURL(rawURL string, subpath string) string {
	u := rawURL
	// SSH git@host:org/repo -> host/org/repo
	if reSSHURL.MatchString(u) {
		u = reSSHURL.ReplaceAllString(u, "$1/$2")
	}
	// strip https:// or http:// prefix
	u = reHTTPProto.ReplaceAllString(u, "")
	// strip trailing .git
	u = strings.TrimSuffix(u, ".git")
	if subpath != "" {
		u = u + "::" + subpath
	}
	return strings.ToLower(u)
}

// UpsertUpstreamState inserts entry into state.Upstreams, replacing any existing
// entry whose URL+subpath normalises to the same key while preserving slice order.
func UpsertUpstreamState(state *sdktypes.DownstreamState, entry sdktypes.UpstreamState) {
	key := NormalizeUpstreamURL(entry.URL, entry.Subpath)
	for i, existing := range state.Upstreams {
		if NormalizeUpstreamURL(existing.URL, existing.Subpath) == key {
			state.Upstreams[i] = entry
			return
		}
	}
	state.Upstreams = append(state.Upstreams, entry)
}

// Integrate will ensure that the downstream at opts.DownstreamRepoPath is
// integrated with each upstream in opts.Upstreams, in order.
func Integrate(opts *sdktypes.IntegrateOptions) (*sdktypes.IntegrateResult, error) {
	result := &sdktypes.IntegrateResult{}

	if opts.Logger == nil {
		opts.Logger = sdktypes.NoopLogger()
	}

	if opts.DownstreamRepoPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return result, fmt.Errorf("unable to get the present working directory: %v", err)
		}
		opts.DownstreamRepoPath = wd
	} else {
		abs, err := filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return result, fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
		opts.DownstreamRepoPath = abs
	}

	if len(opts.Upstreams) == 0 {
		return result, fmt.Errorf("no upstream specified: set Upstreams on IntegrateOptions")
	}

	for _, upstream := range opts.Upstreams {
		integrated, err := integrateOne(opts, upstream)
		if err != nil {
			return result, err
		}
		result.Upstreams = append(result.Upstreams, integrated)
	}
	return result, nil
}

// integrateOne is the public-facing helper called from Integrate. It adapts
// the public *sdktypes.IntegrateOptions into an internalRequest.
func integrateOne(opts *sdktypes.IntegrateOptions, upstream sdktypes.UpstreamSpec) (sdktypes.IntegratedUpstream, error) {
	req := &internalRequest{
		Logger:             opts.Logger,
		DownstreamRepoPath: opts.DownstreamRepoPath,
		ForceRePrompt:      opts.ForceRePrompt,
		// forDriftCheck / upstreamCommit / prevUpstreamCommitHash stay zero-value:
		// public Integrate never runs drift-check semantics.
	}
	return integrateOneInternal(req, upstream)
}

// integrateOneInternal is the shared body. It receives the internalRequest
// carrying the drift-check flag and pinned commit hash, and is called from
// both integrateOne (public path) and IntegrateForDriftCheck.
func integrateOneInternal(req *internalRequest, upstream sdktypes.UpstreamSpec) (sdktypes.IntegratedUpstream, error) {
	prevHash := ""
	if !req.forDriftCheck {
		existingState, err := LoadDownstreamState(req.DownstreamRepoPath)
		if err != nil {
			return sdktypes.IntegratedUpstream{}, fmt.Errorf("error loading downstream state for delta check: %v", err)
		}
		key := NormalizeUpstreamURL(upstream.URL, upstream.Subpath)
		for _, u := range existingState.Upstreams {
			if NormalizeUpstreamURL(u.URL, u.Subpath) == key {
				prevHash = u.CommitHash
				break
			}
		}
	}

	cloneDir, err := os.MkdirTemp("", config.GitSpork)
	if err != nil {
		return sdktypes.IntegratedUpstream{}, fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(cloneDir)

	nestedReq := &internalRequest{
		Logger:                 req.Logger,
		DownstreamRepoPath:     req.DownstreamRepoPath,
		ForceRePrompt:          req.ForceRePrompt,
		forDriftCheck:          req.forDriftCheck,
		upstreamCommit:         req.upstreamCommit,
		prevUpstreamCommitHash: prevHash,
	}

	originalUpstreamURL := upstream.URL
	req.Logger.Log("cloning gitspork upstream repo %s", upstream.URL)
	commitHash, err := cloneUpstreamForIntegrate(cloneDir, nestedReq, upstream)
	if err != nil {
		return sdktypes.IntegratedUpstream{}, err
	}

	upstreamRootPath := filepath.Join(cloneDir, upstream.Subpath)
	req.Logger.Log("parsing the gitspork config file in the upstream repo clone at %s or %s", config.GitSporkConfigFileName, config.GitSporkConfigFileNameAlt)
	gitSporkConfig, err := getGitSporkConfig(upstreamRootPath)
	if err != nil {
		return sdktypes.IntegratedUpstream{}, err
	}

	if !req.forDriftCheck && prevHash != "" {
		upstreamRepo, err := git.PlainOpen(cloneDir)
		if err != nil {
			return sdktypes.IntegratedUpstream{}, fmt.Errorf("error opening upstream clone for delta computation: %v", err)
		}
		delta, err := computeUpstreamDelta(upstreamRepo, prevHash, commitHash, gitSporkConfig, upstream.Subpath)
		if err != nil {
			return sdktypes.IntegratedUpstream{}, fmt.Errorf("error computing upstream delta: %v", err)
		}
		if err := applyUpstreamDelta(delta, req.DownstreamRepoPath, req.Logger); err != nil {
			return sdktypes.IntegratedUpstream{}, fmt.Errorf("error applying upstream delta to downstream: %v", err)
		}
	}

	if err := integrate(gitSporkConfig, upstreamRootPath, req.DownstreamRepoPath, req.ForceRePrompt, req.forDriftCheck, req.Logger); err != nil {
		return sdktypes.IntegratedUpstream{}, err
	}

	if !req.forDriftCheck {
		state, err := LoadDownstreamState(req.DownstreamRepoPath)
		if err != nil {
			return sdktypes.IntegratedUpstream{}, fmt.Errorf("error loading downstream state to save upstream metadata: %v", err)
		}
		UpsertUpstreamState(state, sdktypes.UpstreamState{
			URL:        originalUpstreamURL,
			Subpath:    upstream.Subpath,
			CommitHash: commitHash,
		})
		if err := SaveDownstreamState(req.DownstreamRepoPath, state); err != nil {
			return sdktypes.IntegratedUpstream{}, fmt.Errorf("error saving upstream metadata to downstream state: %v", err)
		}
	}

	return sdktypes.IntegratedUpstream{
		URL:        originalUpstreamURL,
		Subpath:    upstream.Subpath,
		CommitHash: commitHash,
	}, nil
}

func integrate(gitSporkConfig *config.GitSporkConfig, upstreamPath string, downstreamPath string, forceRePrompt bool, forDriftCheck bool, logger sdktypes.Logger) error {
	greenBold := color.New(color.FgHiGreen, color.Bold)

	preIntegrateMigrations := []*config.GitSporkConfigMigrationInstructions{}
	postIntegrateMigrations := []*config.GitSporkConfigMigrationInstructions{}
	queueMigrationIfNotCompleted := func(instructions *config.GitSporkConfigMigrationInstructions, queue []*config.GitSporkConfigMigrationInstructions) ([]*config.GitSporkConfigMigrationInstructions, error) {
		migrationCompleted, err := migrationCompletedInDownstream(instructions.ID, downstreamPath)
		if err != nil {
			return queue, fmt.Errorf("error determining if migration %s was already run in downstream: %v", instructions.ID, err)
		}
		if !migrationCompleted {
			queue = append(queue, instructions)
		}
		return queue, nil
	}
	for _, migrationConfigPath := range gitSporkConfig.Migrations {
		migrationConfig, err := config.ParseMigrationConfig(filepath.Join(upstreamPath, migrationConfigPath))
		if err != nil {
			return fmt.Errorf("error parsing migration config: %v", err)
		}
		if migrationConfig.PreIntegrate != nil {
			migrationConfig.PreIntegrate.ID = fmt.Sprintf("%s:%s", migrationConfigPath, preIntegrateMigrationID)
			preIntegrateMigrations, err = queueMigrationIfNotCompleted(migrationConfig.PreIntegrate, preIntegrateMigrations)
			if err != nil {
				return fmt.Errorf("error queuing post-integrate migrations: %v", err)
			}
		}
		if migrationConfig.PostIntegrate != nil {
			migrationConfig.PostIntegrate.ID = fmt.Sprintf("%s:%s", migrationConfigPath, postIntegrateMigrationID)
			postIntegrateMigrations, err = queueMigrationIfNotCompleted(migrationConfig.PostIntegrate, postIntegrateMigrations)
			if err != nil {
				return fmt.Errorf("error queuing post-integrate migrations: %v", err)
			}
		}
	}

	for _, preIntegrateMigration := range preIntegrateMigrations {
		logger.Log("%s", greenBold.Sprintf("running pre-integrate migration defined in upstream against the downstream: %s", preIntegrateMigration.ID))
		if err := runMigration(preIntegrateMigration, upstreamPath, downstreamPath, logger); err != nil {
			return fmt.Errorf("error running pre-integrate migration against the downstream: %v", err)
		}
		if !forDriftCheck {
			if err := recordCompleteMigration(preIntegrateMigration.ID, downstreamPath); err != nil {
				return fmt.Errorf("error recording successful migration result: %v", err)
			}
		}
	}

	logger.Log("%s", greenBold.Sprint("integrating configured upstream-owned resources from upstream to downstream"))
	if err := (&IntegratorUpstreamOwned{}).Integrate(gitSporkConfig.UpstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating upstream-owned: %v", err)
	}

	logger.Log("%s", greenBold.Sprint("integrating configured downstream-owned resources from upstream to downstream"))
	if err := (&IntegratorDownstreamOwned{}).Integrate(gitSporkConfig.DownstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating downstream-owned: %v", err)
	}

	logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership generic resources to merge b/w upstream and downstream"))
	if err := (&IntegratorSharedOwnershipMerged{}).Integrate(gitSporkConfig.SharedOwnership.Merged, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.merged: %v", err)
	}

	logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering upstream data"))
	if err := (&IntegratorSharedOwnershipStructuredPreferUpstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferUpstream, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_upstream: %v", err)
	}

	logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering downstream data"))
	if err := (&IntegratorSharedOwnershipStructuredPreferDownstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferDownstream, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_downstream: %v", err)
	}

	logger.Log("%s", greenBold.Sprint("integrating configured templated resources from upstream to downstream"))
	if err := (&IntegratorTemplated{}).Integrate(gitSporkConfig.Templated, upstreamPath, downstreamPath, forceRePrompt, logger); err != nil {
		return fmt.Errorf("error integrating templated: %v", err)
	}

	for _, postIntegrateMigration := range postIntegrateMigrations {
		logger.Log("%s", greenBold.Sprintf("running post-integrate migration defined in upstream against the downstream: %s", postIntegrateMigration.ID))
		if err := runMigration(postIntegrateMigration, upstreamPath, downstreamPath, logger); err != nil {
			return fmt.Errorf("error running post-integrate migration against the downstream: %v", err)
		}
		if !forDriftCheck {
			if err := recordCompleteMigration(postIntegrateMigration.ID, downstreamPath); err != nil {
				return fmt.Errorf("error recording successful migration result: %v", err)
			}
		}
	}

	return nil
}

// applySSHKnownHosts sets the host key callback on agentAuth from SSH_KNOWN_HOSTS.
// No-ops when SSH_KNOWN_HOSTS is not set (go-git uses its own discovery).
// Returns an error when the env var is set but no listed files exist, preventing
// a nil-pointer panic inside go-git's NewKnownHostsCallback.
func applySSHKnownHosts(agentAuth *ssh.PublicKeysCallback) error {
	val := os.Getenv("SSH_KNOWN_HOSTS")
	if val == "" {
		return nil
	}
	var validFiles []string
	for _, f := range filepath.SplitList(val) {
		if _, err := os.Stat(f); err == nil {
			validFiles = append(validFiles, f)
		}
	}
	if len(validFiles) == 0 {
		return nil
	}
	cb, err := ssh.NewKnownHostsCallback(validFiles...)
	if err != nil {
		return fmt.Errorf("error loading SSH known hosts: %v", err)
	}
	agentAuth.HostKeyCallback = cb
	return nil
}

func resolveUpstreamURL(url string, token string) string {
	isHTTPS, _ := regexp.MatchString(`^https://`, url)
	isSSH, _ := regexp.MatchString(`^git@`, url)
	if token == "" && isHTTPS {
		re := regexp.MustCompile(`^https://([^/]+)/(.+)$`)
		return re.ReplaceAllString(url, "git@$1:$2")
	}
	if token != "" && isSSH {
		re := regexp.MustCompile(`^git@([^:]+):(.+)$`)
		return re.ReplaceAllString(url, "https://$1/$2")
	}
	return url
}

func cloneUpstreamForIntegrate(cloneDir string, req *internalRequest, upstream sdktypes.UpstreamSpec) (string, error) {
	upstreamURL := resolveUpstreamURL(upstream.URL, upstream.Token)
	var err error
	var authMethod transport.AuthMethod
	isHTTPsUpstreamURL, _ := regexp.MatchString("^https://.*$", upstreamURL)
	isSSHUpstreamURL, _ := regexp.MatchString("^git@", upstreamURL)
	if isHTTPsUpstreamURL && upstream.Token != "" {
		authMethod = &http.BasicAuth{
			Username: config.GitSpork,
			Password: upstream.Token,
		}
	} else if isSSHUpstreamURL {
		agentAuth, err := ssh.NewSSHAgentAuth(config.GitSSHUsername)
		if err != nil {
			return "", fmt.Errorf("error setting up SSH auth method for git: %v", err)
		}
		if err := applySSHKnownHosts(agentAuth); err != nil {
			return "", err
		}
		authMethod = agentAuth
	}
	cloneOptions := &git.CloneOptions{
		URL:      upstreamURL,
		Progress: &logutil.LoggerWriter{L: req.Logger},
	}
	if authMethod != nil {
		cloneOptions.Auth = authMethod
	}

	// Interpret upstream.Version:
	//   1. Empty              → clone the default branch (SingleBranch).
	//   2. Hex hash 7-40 chars → full-history clone; resolve + checkout below.
	//   3. "tags/..." prefix   → refs/<v> (explicit tag, backward compat).
	//   4. Otherwise           → probe remote; prefer tag, fall back to branch.
	versionIsCommitHash := false
	switch {
	case upstream.Version == "":
		cloneOptions.SingleBranch = true
	case commitHashRe.MatchString(upstream.Version):
		versionIsCommitHash = true
		cloneOptions.SingleBranch = false
	case strings.HasPrefix(upstream.Version, "tags/"):
		cloneOptions.ReferenceName = plumbing.ReferenceName("refs/" + upstream.Version)
		cloneOptions.SingleBranch = true
	default:
		resolvedRef, err := resolveUpstreamVersionRef(upstreamURL, authMethod, upstream.Version)
		if err != nil {
			return "", err
		}
		cloneOptions.ReferenceName = resolvedRef
		cloneOptions.SingleBranch = true
	}

	if req.upstreamCommit != "" || req.prevUpstreamCommitHash != "" {
		// need full history to checkout a specific commit
		cloneOptions.SingleBranch = false
	}
	repo, err := git.PlainClone(cloneDir, cloneOptions)
	if err != nil {
		return "", fmt.Errorf("error cloning upstream gitspork repo: %v", err)
	}
	if req.upstreamCommit != "" {
		worktree, err := repo.Worktree()
		if err != nil {
			return "", fmt.Errorf("error getting worktree for commit checkout: %v", err)
		}
		if err := worktree.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash(req.upstreamCommit),
		}); err != nil {
			return "", fmt.Errorf("error checking out commit %s: %v", req.upstreamCommit, err)
		}
		return req.upstreamCommit, nil
	}
	if versionIsCommitHash {
		resolvedHash, err := repo.ResolveRevision(plumbing.Revision(upstream.Version))
		if err != nil {
			return "", fmt.Errorf("could not resolve upstream version %q as commit: %v", upstream.Version, err)
		}
		worktree, err := repo.Worktree()
		if err != nil {
			return "", fmt.Errorf("error getting worktree for commit checkout: %v", err)
		}
		if err := worktree.Checkout(&git.CheckoutOptions{Hash: *resolvedHash}); err != nil {
			return "", fmt.Errorf("error checking out commit %s: %v", upstream.Version, err)
		}
		return resolvedHash.String(), nil
	}
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("error resolving HEAD commit from upstream clone: %v", err)
	}
	return ref.Hash().String(), nil
}

// resolveUpstreamVersionRef probes the remote to disambiguate a bare Version
// value between a tag and a branch. Tags win over branches when both exist,
// matching `git checkout`'s precedence for ambiguous refs.
func resolveUpstreamVersionRef(url string, auth transport.AuthMethod, version string) (plumbing.ReferenceName, error) {
	rem := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	refs, err := rem.List(&git.ListOptions{Auth: auth})
	if err != nil {
		return "", fmt.Errorf("could not list remote refs to resolve upstream version %q: %v", version, err)
	}
	tagRef := plumbing.ReferenceName("refs/tags/" + version)
	branchRef := plumbing.ReferenceName("refs/heads/" + version)
	var haveTag, haveBranch bool
	for _, r := range refs {
		switch r.Name() {
		case tagRef:
			haveTag = true
		case branchRef:
			haveBranch = true
		}
	}
	if haveTag {
		return tagRef, nil
	}
	if haveBranch {
		return branchRef, nil
	}
	return "", fmt.Errorf("upstream version %q not found as branch or tag on remote", version)
}

func getIntegrateFiles(inDir string, configuredGlobPatterns []string) ([]string, error) {
	allFiles := []string{}
	makeFileRelativePath := func(filePath string) (string, error) {
		re, err := regexp.Compile(fmt.Sprintf("^%s%s", inDir, string(filepath.Separator)))
		if err != nil {
			return "", err
		}
		return re.ReplaceAllString(filePath, ""), nil
	}
	err := filepath.Walk(inDir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		for _, configuredGlobPattern := range configuredGlobPatterns {
			g, inErr := glob.Compile(configuredGlobPattern)
			if inErr != nil {
				return inErr
			}
			path, inErr = makeFileRelativePath(path)
			if inErr != nil {
				return inErr
			}
			if g.Match(path) {
				allFiles = append(allFiles, path)
				return nil
			}
		}
		return nil
	})
	return allFiles, err
}

func getGitSporkConfig(atPath string) (*config.GitSporkConfig, error) {
	cfg := &config.GitSporkConfig{}
	gitSporkConfigFilePath := filepath.Join(atPath, config.GitSporkConfigFileName)
	if _, err := os.Stat(gitSporkConfigFilePath); os.IsNotExist(err) {
		gitSporkConfigFilePath = filepath.Join(atPath, config.GitSporkConfigFileNameAlt)
		if _, err := os.Stat(gitSporkConfigFilePath); os.IsNotExist(err) {
			return cfg, fmt.Errorf("it looks like %s does not include a %s or %s config file", atPath, config.GitSporkConfigFileName, config.GitSporkConfigFileNameAlt)
		}
	}
	return config.ParseGitSporkConfig(gitSporkConfigFilePath)
}

func syncFile(src, dst string) error {
	// Open the source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file at %s: %v", src, err)
	}
	defer sourceFile.Close()
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file at %s: %v", src, err)
	}

	// Ensure the destination directory
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("error ensuring destination directory path exists %s: %v", filepath.Dir(dst), err)
	}

	// Create the destination file
	destinationFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %v", dst, err)
	}
	defer destinationFile.Close()

	// Copy the content
	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file from %s to %s: %v", src, dst, err)
	}

	// Flush file metadata to disk
	err = destinationFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync destination file %s: %v", dst, err)
	}
	// ensure source and dest file perms match
	if err := os.Chmod(dst, sourceFileStat.Mode().Perm()); err != nil {
		return fmt.Errorf("failed ensuring destination file %s perms set: %v", dst, err)
	}

	return nil
}

func getStructuredData(upstreamPath string, downstreamPath string) (*node, *node, string, error) {
	structuredDataType := ""
	for _, yamlExt := range structuredDataYAMLExtensions {
		if filepath.Ext(upstreamPath) == yamlExt {
			structuredDataType = structuredDataTypeYAML
		}
	}
	for _, jsonExt := range structuredDataJSONExtensions {
		if filepath.Ext(upstreamPath) == jsonExt {
			structuredDataType = structuredDataTypeJSON
		}
	}
	if structuredDataType == "" {
		return nil, nil, "", fmt.Errorf("upstream file %s is not a supported structured data file, supported: %v", upstreamPath, append(structuredDataYAMLExtensions, structuredDataJSONExtensions...))
	}

	upstreamBytes, err := os.ReadFile(upstreamPath)
	if err != nil {
		return nil, nil, structuredDataType, fmt.Errorf("error reading file %s", upstreamPath)
	}
	if _, err := os.Stat(downstreamPath); os.IsNotExist(err) {
		if err := syncFile(upstreamPath, downstreamPath); err != nil {
			return nil, nil, structuredDataType, fmt.Errorf("error copying structured data file %s to downstream: %v", upstreamPath, err)
		}
	}
	downstreamBytes, err := os.ReadFile(downstreamPath)
	if err != nil {
		return nil, nil, structuredDataType, fmt.Errorf("error reading file %s", downstreamPath)
	}

	parse := parseYAML
	if structuredDataType == structuredDataTypeJSON {
		parse = parseJSON
	}
	upstreamNode, err := parse(upstreamBytes)
	if err != nil {
		return nil, nil, structuredDataType, fmt.Errorf("error parsing upstream file %s: %v", upstreamPath, err)
	}
	downstreamNode, err := parse(downstreamBytes)
	if err != nil {
		return nil, nil, structuredDataType, fmt.Errorf("error parsing downstream file %s: %v", downstreamPath, err)
	}
	return upstreamNode, downstreamNode, structuredDataType, nil
}

func writeStructuredData(data *node, structuredDataType string, toPath string) error {
	var b []byte
	var err error
	switch structuredDataType {
	case structuredDataTypeYAML:
		b, err = writeYAML(data)
	case structuredDataTypeJSON:
		b, err = writeJSON(data)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(toPath, b, 0644)
}

func ensureDownstreamMetaDir(downstreamRepoPath string) (string, error) {
	gitSporkMetaDir := filepath.Join(downstreamRepoPath, gitSporkMetaDirName)
	pathInfo, err := os.Stat(gitSporkMetaDir)
	switch {
	case err == nil && pathInfo.IsDir():
		return gitSporkMetaDir, nil
	case err == nil:
		// exists but is not a directory (stray file/symlink); replace it
		if rmErr := os.RemoveAll(gitSporkMetaDir); rmErr != nil {
			return gitSporkMetaDir, rmErr
		}
	case !os.IsNotExist(err):
		// permission-denied, EIO, symlink-loop, etc. — surface, don't nil-deref
		return gitSporkMetaDir, err
	}
	if err := os.Mkdir(gitSporkMetaDir, 0755); err != nil {
		return gitSporkMetaDir, err
	}
	return gitSporkMetaDir, nil
}

// LoadDownstreamState reads the persisted downstream state from
// .gitspork/downstream-state.json under downstreamRepoPath, creating the
// metadata directory if it does not already exist. Deprecated single-upstream
// fields on disk are migrated in-memory into the Upstreams slice.
func LoadDownstreamState(downstreamRepoPath string) (*sdktypes.DownstreamState, error) {
	gitSporkMetaDir, err := ensureDownstreamMetaDir(downstreamRepoPath)
	if err != nil {
		return nil, err
	}
	state := &sdktypes.DownstreamState{}
	stateFilePath := filepath.Join(gitSporkMetaDir, downstreamStateFileName)
	if _, err := os.Stat(stateFilePath); os.IsNotExist(err) {
		return state, nil
	}
	f, err := os.ReadFile(stateFilePath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(f, state)
	if err != nil {
		return state, err
	}
	// Migrate deprecated single-upstream fields to Upstreams slice.
	if len(state.Upstreams) == 0 && state.LastUpstreamCommitHash != "" {
		state.Upstreams = []sdktypes.UpstreamState{{
			URL:        state.LastUpstreamRepoURL,
			Subpath:    state.LastUpstreamRepoSubpath,
			CommitHash: state.LastUpstreamCommitHash,
		}}
		state.LastUpstreamRepoURL = ""
		state.LastUpstreamRepoSubpath = ""
		state.LastUpstreamCommitHash = ""
	}
	return state, nil
}

// SaveDownstreamState persists state to .gitspork/downstream-state.json under
// downstreamRepoPath, ensuring the metadata directory exists first.
func SaveDownstreamState(downstreamRepoPath string, state *sdktypes.DownstreamState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	gitSporkMetaDir, err := ensureDownstreamMetaDir(downstreamRepoPath)
	if err != nil {
		return err
	}
	stateFilePath := filepath.Join(gitSporkMetaDir, downstreamStateFileName)
	if err := os.WriteFile(stateFilePath, b, 0644); err != nil {
		return err
	}
	return nil
}

func migrationCompletedInDownstream(migrationID string, downstreamRepoPath string) (bool, error) {
	state, err := LoadDownstreamState(downstreamRepoPath)
	if err != nil {
		return false, err
	}
	for _, migrationComplete := range state.MigrationsComplete {
		if migrationComplete == migrationID {
			return true, nil
		}
	}
	return false, nil
}

func runMigration(migrationInstructions *config.GitSporkConfigMigrationInstructions, upstreamRepoRootPath string, downstreamRepoPath string, logger sdktypes.Logger) error {
	if migrationInstructions.Exec != "" {
		execParts := strings.Split(migrationInstructions.Exec, " ")
		if _, err := os.Stat(filepath.Join(upstreamRepoRootPath, execParts[0])); err == nil {
			// this is a case where the exec is calling a script that exists in the upstream, so call from that absolute path
			execParts[0] = filepath.Join(upstreamRepoRootPath, execParts[0])
		}
		cmd := exec.Command(execParts[0], execParts[1:]...)
		cmd.Stdout = &logutil.LoggerWriter{L: logger}
		cmd.Stderr = &logutil.LoggerWriter{L: logger}
		cmd.Dir = downstreamRepoPath
		return cmd.Run()
	}
	return nil
}

func recordCompleteMigration(migrationID string, downstreamRepoPath string) error {
	state, err := LoadDownstreamState(downstreamRepoPath)
	if err != nil {
		return err
	}
	migrationCompleted, err := migrationCompletedInDownstream(migrationID, downstreamRepoPath)
	if err != nil {
		return err
	}
	if !migrationCompleted {
		state.MigrationsComplete = append(state.MigrationsComplete, migrationID)
	}
	return SaveDownstreamState(downstreamRepoPath, state)
}
