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
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/gobwas/glob"
	"github.com/goccy/go-yaml"
	"github.com/rockholla/gitspork/internal/config"
	"github.com/rockholla/gitspork/internal/types"
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
)

// Integrator is implemented by the ownership integrators that process a
// homogeneous list of items into the downstream: T is OwnedEntry for the
// upstream_owned/downstream_owned lists and string (glob pattern) for the
// shared_ownership lists. Each integrator file carries a compile-time
// `var _ Integrator[T] = (*Type)(nil)` assertion, so a newly added integrator
// that diverges from this contract fails to build.
type Integrator[T any] interface {
	Integrate(items []T, upstreamPath string, downstreamPath string, logger types.Logger) error
}

// TemplatedIntegrator is kept separate from Integrator[T] because rendering
// templates additionally needs the forceRePrompt flag to drive input
// collection, so its Integrate signature cannot match the generic contract.
type TemplatedIntegrator interface {
	Integrate(instructions []config.GitSporkConfigTemplated, upstreamPath string, downstreamPath string, forceRePrompt bool, logger types.Logger) error
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
func UpsertUpstreamState(state *types.GitSporkDownstreamState, entry types.GitSporkUpstreamState) {
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
func Integrate(opts *types.IntegrateOptions) (*types.IntegrateResult, error) {
	var err error
	result := &types.IntegrateResult{}

	if opts.Logger == nil {
		opts.Logger = types.NoopLogger()
	}

	if opts.DownstreamRepoPath == "" {
		opts.DownstreamRepoPath, err = os.Getwd()
		if err != nil {
			return result, fmt.Errorf("unable to get the present working directory: %v", err)
		}
	} else {
		opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return result, fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
	}

	// Normalize: synthesize Upstreams from single-upstream fields for backward compat.
	if len(opts.Upstreams) == 0 && opts.UpstreamRepoURL != "" {
		opts.Upstreams = []types.UpstreamSpec{{
			URL:     opts.UpstreamRepoURL,
			Version: opts.UpstreamRepoVersion,
			Subpath: opts.UpstreamRepoSubpath,
			Token:   opts.UpstreamRepoToken,
		}}
	}
	if len(opts.Upstreams) == 0 {
		return result, fmt.Errorf("no upstream specified: provide --upstream or --upstream-repo-url")
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

func integrateOne(opts *types.IntegrateOptions, upstream types.UpstreamSpec) (types.IntegratedUpstream, error) {
	prevHash := ""
	if !opts.ForDriftCheck {
		existingState, err := LoadDownstreamState(opts.DownstreamRepoPath)
		if err != nil {
			return types.IntegratedUpstream{}, fmt.Errorf("error loading downstream state for delta check: %v", err)
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
		return types.IntegratedUpstream{}, fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(cloneDir)

	singleOpts := &types.IntegrateOptions{
		Logger:                 opts.Logger,
		UpstreamRepoURL:        upstream.URL,
		UpstreamRepoVersion:    upstream.Version,
		UpstreamRepoSubpath:    upstream.Subpath,
		UpstreamRepoToken:      upstream.Token,
		UpstreamRepoCommit:     opts.UpstreamRepoCommit,
		DownstreamRepoPath:     opts.DownstreamRepoPath,
		ForceRePrompt:          opts.ForceRePrompt,
		ForDriftCheck:          opts.ForDriftCheck,
		PrevUpstreamCommitHash: prevHash,
	}

	originalUpstreamURL := upstream.URL
	opts.Logger.Log("cloning gitspork upstream repo %s", upstream.URL)
	commitHash, err := cloneUpstreamForIntegrate(cloneDir, singleOpts)
	if err != nil {
		return types.IntegratedUpstream{}, err
	}

	upstreamRootPath := filepath.Join(cloneDir, upstream.Subpath)
	opts.Logger.Log("parsing the gitspork config file in the upstream repo clone at %s or %s", config.GitSporkConfigFileName, config.GitSporkConfigFileNameAlt)
	gitSporkConfig, err := getGitSporkConfig(upstreamRootPath)
	if err != nil {
		return types.IntegratedUpstream{}, err
	}

	if !opts.ForDriftCheck && prevHash != "" {
		upstreamRepo, err := git.PlainOpen(cloneDir)
		if err != nil {
			return types.IntegratedUpstream{}, fmt.Errorf("error opening upstream clone for delta computation: %v", err)
		}
		delta, err := computeUpstreamDelta(upstreamRepo, prevHash, commitHash, gitSporkConfig, upstream.Subpath)
		if err != nil {
			return types.IntegratedUpstream{}, fmt.Errorf("error computing upstream delta: %v", err)
		}
		if err := applyUpstreamDelta(delta, opts.DownstreamRepoPath, opts.Logger); err != nil {
			return types.IntegratedUpstream{}, fmt.Errorf("error applying upstream delta to downstream: %v", err)
		}
	}

	if err := integrate(gitSporkConfig, upstreamRootPath, opts.DownstreamRepoPath, opts.ForceRePrompt, opts.ForDriftCheck, opts.Logger); err != nil {
		return types.IntegratedUpstream{}, err
	}

	if !opts.ForDriftCheck {
		state, err := LoadDownstreamState(opts.DownstreamRepoPath)
		if err != nil {
			return types.IntegratedUpstream{}, fmt.Errorf("error loading downstream state to save upstream metadata: %v", err)
		}
		UpsertUpstreamState(state, types.GitSporkUpstreamState{
			URL:        originalUpstreamURL,
			Subpath:    upstream.Subpath,
			CommitHash: commitHash,
		})
		if err := SaveDownstreamState(opts.DownstreamRepoPath, state); err != nil {
			return types.IntegratedUpstream{}, fmt.Errorf("error saving upstream metadata to downstream state: %v", err)
		}
	}

	return types.IntegratedUpstream{
		URL:        originalUpstreamURL,
		Subpath:    upstream.Subpath,
		CommitHash: commitHash,
	}, nil
}

func integrate(gitSporkConfig *config.GitSporkConfig, upstreamPath string, downstreamPath string, forceRePrompt bool, forDriftCheck bool, logger types.Logger) error {
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
		fmt.Println("")
		logger.Log("%s", greenBold.Sprintf("running pre-integrate migration defined in upstream against the downstream: %s", preIntegrateMigration.ID))
		if err := runMigration(preIntegrateMigration, upstreamPath, downstreamPath); err != nil {
			return fmt.Errorf("error running pre-integrate migration against the downstream: %v", err)
		}
		if !forDriftCheck {
			if err := recordCompleteMigration(preIntegrateMigration.ID, downstreamPath); err != nil {
				return fmt.Errorf("error recording successful migration result: %v", err)
			}
		}
	}

	fmt.Println("")
	logger.Log("%s", greenBold.Sprint("integrating configured upstream-owned resources from upstream to downstream"))
	if err := (&IntegratorUpstreamOwned{}).Integrate(gitSporkConfig.UpstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating upstream-owned: %v", err)
	}

	fmt.Println("")
	logger.Log("%s", greenBold.Sprint("integrating configured downstream-owned resources from upstream to downstream"))
	if err := (&IntegratorDownstreamOwned{}).Integrate(gitSporkConfig.DownstreamOwned, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating downstream-owned: %v", err)
	}

	fmt.Println("")
	logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership generic resources to merge b/w upstream and downstream"))
	if err := (&IntegratorSharedOwnershipMerged{}).Integrate(gitSporkConfig.SharedOwnership.Merged, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.merged: %v", err)
	}

	fmt.Println("")
	logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering upstream data"))
	if err := (&IntegratorSharedOwnershipStructuredPreferUpstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferUpstream, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_upstream: %v", err)
	}

	fmt.Println("")
	logger.Log("%s", greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering downstream data"))
	if err := (&IntegratorSharedOwnershipStructuredPreferDownstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferDownstream, upstreamPath, downstreamPath, logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_downstream: %v", err)
	}

	fmt.Println("")
	logger.Log("%s", greenBold.Sprint("integrating configured templated resources from upstream to downstream"))
	if err := (&IntegratorTemplated{}).Integrate(gitSporkConfig.Templated, upstreamPath, downstreamPath, forceRePrompt, logger); err != nil {
		return fmt.Errorf("error integrating templated: %v", err)
	}

	for _, postIntegrateMigration := range postIntegrateMigrations {
		fmt.Println("")
		logger.Log("%s", greenBold.Sprintf("running post-integrate migration defined in upstream against the downstream: %s", postIntegrateMigration.ID))
		if err := runMigration(postIntegrateMigration, upstreamPath, downstreamPath); err != nil {
			return fmt.Errorf("error running post-integrate migration against the downstream: %v", err)
		}
		if !forDriftCheck {
			if err := recordCompleteMigration(postIntegrateMigration.ID, downstreamPath); err != nil {
				return fmt.Errorf("error recording successful migration result: %v", err)
			}
		}
	}

	fmt.Println("")
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

func cloneUpstreamForIntegrate(cloneDir string, opts *types.IntegrateOptions) (string, error) {
	opts.UpstreamRepoURL = resolveUpstreamURL(opts.UpstreamRepoURL, opts.UpstreamRepoToken)
	var err error
	var authMethod transport.AuthMethod
	isHTTPsUpstreamURL, _ := regexp.MatchString("^https://.*$", opts.UpstreamRepoURL)
	isSSHUpstreamURL, _ := regexp.MatchString("^git@", opts.UpstreamRepoURL)
	if isHTTPsUpstreamURL && opts.UpstreamRepoToken != "" {
		authMethod = &http.BasicAuth{
			Username: config.GitSpork,
			Password: opts.UpstreamRepoToken,
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
		URL:          opts.UpstreamRepoURL,
		SingleBranch: true,
		Progress:     os.Stdout,
	}
	if authMethod != nil {
		cloneOptions.Auth = authMethod
	}
	if opts.UpstreamRepoVersion != "" {
		refName := fmt.Sprintf("refs/heads/%s", opts.UpstreamRepoVersion)
		isTag, err := regexp.MatchString("^tags\\/", opts.UpstreamRepoVersion)
		if err != nil {
			return "", fmt.Errorf("error processing upstream gitspork repo version to use: %v", err)
		}
		if isTag {
			refName = fmt.Sprintf("refs/%s", opts.UpstreamRepoVersion)
		}
		cloneOptions.ReferenceName = plumbing.ReferenceName(refName)
	}
	if opts.UpstreamRepoCommit != "" || opts.PrevUpstreamCommitHash != "" {
		// need full history to checkout a specific commit
		cloneOptions.SingleBranch = false
	}
	repo, err := git.PlainClone(cloneDir, cloneOptions)
	if err != nil {
		return "", fmt.Errorf("error cloning upstream gitspork repo: %v", err)
	}
	if opts.UpstreamRepoCommit != "" {
		worktree, err := repo.Worktree()
		if err != nil {
			return "", fmt.Errorf("error getting worktree for commit checkout: %v", err)
		}
		if err := worktree.Checkout(&git.CheckoutOptions{
			Hash: plumbing.NewHash(opts.UpstreamRepoCommit),
		}); err != nil {
			return "", fmt.Errorf("error checking out commit %s: %v", opts.UpstreamRepoCommit, err)
		}
		return opts.UpstreamRepoCommit, nil
	}
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("error resolving HEAD commit from upstream clone: %v", err)
	}
	return ref.Hash().String(), nil
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

func getStructuredData(upstreamPath string, downstreamPath string) (*map[string]any, *map[string]any, string, error) {
	var err error
	upstreamData := &map[string]any{}
	downstreamData := &map[string]any{}

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
		return upstreamData, downstreamData, "", fmt.Errorf("upstream file %s is not a supported structured data file, supported: %v", upstreamPath, append(structuredDataYAMLExtensions, structuredDataJSONExtensions...))
	}

	upstreamPathBytes, err := os.ReadFile(upstreamPath)
	if err != nil {
		return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error reading file %s", upstreamPath)
	}
	if _, err := os.Stat(downstreamPath); os.IsNotExist(err) {
		// the downstream doesn't exist yet, we can just copy the file
		if err := syncFile(upstreamPath, downstreamPath); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error copying structured data file %s to downstream: %v", upstreamPath, err)
		}
	}
	downstreamPathBytes, err := os.ReadFile(downstreamPath)
	if err != nil {
		return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error reading file %s", upstreamPath)
	}
	switch structuredDataType {
	case structuredDataTypeYAML:
		if err := yaml.Unmarshal(upstreamPathBytes, upstreamData); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error parsing upstream file %s: %v", upstreamPath, err)
		}
		if err := yaml.Unmarshal(downstreamPathBytes, downstreamData); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error parsing downstream file %s: %v", upstreamPath, err)
		}
	case structuredDataTypeJSON:
		if err := json.Unmarshal(upstreamPathBytes, upstreamData); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error parsing upstream file %s: %v", upstreamPath, err)
		}
		if err := json.Unmarshal(downstreamPathBytes, downstreamData); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error parsing downstream file %s: %v", upstreamPath, err)
		}
	}

	return upstreamData, downstreamData, structuredDataType, err
}

func writeStructuredData(structuredData *map[string]interface{}, structuredDataType string, toPath string) error {
	var b []byte
	var err error
	switch structuredDataType {
	case structuredDataTypeYAML:
		b, err = yaml.Marshal(structuredData)
		if err != nil {
			return err
		}
	case structuredDataTypeJSON:
		b, err = json.MarshalIndent(structuredData, "", "  ")
		if err != nil {
			return err
		}
	}
	if err := os.WriteFile(toPath, b, 0644); err != nil {
		return err
	}
	return nil
}

func ensureDownstreamMetaDir(downstreamRepoPath string) (string, error) {
	gitSporkMetaDir := filepath.Join(downstreamRepoPath, gitSporkMetaDirName)
	if pathInfo, err := os.Stat(gitSporkMetaDir); os.IsNotExist(err) || !pathInfo.IsDir() {
		os.RemoveAll(gitSporkMetaDir) // remove it if it exists, since it is not a dir
		if err := os.Mkdir(gitSporkMetaDir, 0755); err != nil {
			return gitSporkMetaDir, err
		}
	}
	return gitSporkMetaDir, nil
}

// LoadDownstreamState reads the persisted downstream state from
// .gitspork/downstream-state.json under downstreamRepoPath, creating the
// metadata directory if it does not already exist. Deprecated single-upstream
// fields on disk are migrated in-memory into the Upstreams slice.
func LoadDownstreamState(downstreamRepoPath string) (*types.GitSporkDownstreamState, error) {
	gitSporkMetaDir, err := ensureDownstreamMetaDir(downstreamRepoPath)
	if err != nil {
		return nil, err
	}
	state := &types.GitSporkDownstreamState{}
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
		state.Upstreams = []types.GitSporkUpstreamState{{
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
func SaveDownstreamState(downstreamRepoPath string, state *types.GitSporkDownstreamState) error {
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

func runMigration(migrationInstructions *config.GitSporkConfigMigrationInstructions, upstreamRepoRootPath string, downstreamRepoPath string) error {
	if migrationInstructions.Exec != "" {
		execParts := strings.Split(migrationInstructions.Exec, " ")
		if _, err := os.Stat(filepath.Join(upstreamRepoRootPath, execParts[0])); err == nil {
			// this is a case where the exec is calling a script that exists in the upstream, so call from that absolute path
			execParts[0] = filepath.Join(upstreamRepoRootPath, execParts[0])
		}
		cmd := exec.Command(execParts[0], execParts[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
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
