package internal

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
	"gopkg.in/yaml.v2"
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
)

// GitSporkIntegrator defines the common implementation requirements for any thing processing a list of glob patterns to integrate b/w upstream/downstream
type GitSporkIntegrator interface {
	Integrate(configuredGlobPatterns []string, upstreamRepoPath string, downstreamRepoPath string, logger *Logger) error
}

// Integrate will ensure that the localRepoPath is integrated/re-integrated w/ the upstreamRepoURL version
func Integrate(opts *IntegrateOptions) error {
	var err error

	// setup
	if opts.DownstreamRepoPath == "" {
		opts.DownstreamRepoPath, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("unable to get the present working directory: %v", err)
		}
	} else {
		opts.DownstreamRepoPath, err = filepath.Abs(opts.DownstreamRepoPath)
		if err != nil {
			return fmt.Errorf("unable to determine local downstream repo path: %v", err)
		}
	}

	// clone the upstream git repo to a temporary local location to start
	cloneDir, err := os.MkdirTemp("", gitSpork)
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(cloneDir)
	opts.Logger.Log("cloning gitspork upstream repo %s", opts.UpstreamRepoURL)
	if err := cloneUpstreamForIntegrate(cloneDir, opts); err != nil {
		return err
	}

	// now we can work our way through both the upstream clone and our local downstream source
	// to begin integrating, merging, etc.
	upstreamRootPath := filepath.Join(cloneDir, opts.UpstreamRepoSubpath)
	opts.Logger.Log("parsing the gitspork config file in the upstream repo clone at %s or %s", gitSporkConfigFileName, gitSporkConfigFileNameAlt)
	gitSporkConfigFilePath := filepath.Join(upstreamRootPath, gitSporkConfigFileName)
	if _, err := os.Stat(gitSporkConfigFilePath); os.IsNotExist(err) {
		gitSporkConfigFilePath = filepath.Join(upstreamRootPath, gitSporkConfigFileNameAlt)
		if _, err := os.Stat(gitSporkConfigFilePath); os.IsNotExist(err) {
			return fmt.Errorf("it looks like %s does not include a %s or %s config file", opts.UpstreamRepoURL, gitSporkConfigFileName, gitSporkConfigFileNameAlt)
		}
	}
	gitSporkConfig, err := ParseGitSporkConfig(gitSporkConfigFilePath)
	if err != nil {
		return err
	}

	greenBold := color.New(color.FgHiGreen, color.Bold)

	preIntegrateMigrations := []*GitSporkConfigMigrationInstructions{}
	postIntegrateMigrations := []*GitSporkConfigMigrationInstructions{}
	queueMigrationIfNotCompleted := func(instructions *GitSporkConfigMigrationInstructions, queue []*GitSporkConfigMigrationInstructions) ([]*GitSporkConfigMigrationInstructions, error) {
		migrationCompleted, err := migrationCompletedInDownstream(instructions.ID, opts.DownstreamRepoPath)
		if err != nil {
			return queue, fmt.Errorf("error determining if migration %s was already run in downstream: %v", instructions.ID, err)
		}
		if !migrationCompleted {
			queue = append(queue, instructions)
		}
		return queue, nil
	}
	for _, migrationConfigPath := range gitSporkConfig.Migrations {
		migrationConfig, err := ParseMigrationConfig(filepath.Join(upstreamRootPath, migrationConfigPath))
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
		opts.Logger.Log(greenBold.Sprintf("running pre-integrate migration defined in upstream against the downstream: %s", preIntegrateMigration.ID))
		if err := runMigration(preIntegrateMigration, upstreamRootPath, opts.DownstreamRepoPath); err != nil {
			return fmt.Errorf("error running pre-integrate migration against the downstream: %v", err)
		}
		if err := recordCompleteMigration(preIntegrateMigration.ID, opts.DownstreamRepoPath); err != nil {
			return fmt.Errorf("error recording successful migration result: %v", err)
		}
	}

	fmt.Println("")
	opts.Logger.Log(greenBold.Sprint("integrating configured upstream-owned resources from upstream to downstream"))
	if err := (&IntegratorUpstreamOwned{}).Integrate(gitSporkConfig.UpstreamOwned, upstreamRootPath, opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating upstream-owned: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log(greenBold.Sprint("integrating configured downstream-owned resources from upstream to downstream"))
	if err := (&IntegratorDownstreamOwned{}).Integrate(gitSporkConfig.DownstreamOwned, upstreamRootPath, opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating downstream-owned: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log(greenBold.Sprint("integrating configured shared-ownership generic resources to merge b/w upstream and downstream"))
	if err := (&IntegratorSharedOwnershipMerged{}).Integrate(gitSporkConfig.SharedOwnership.Merged, upstreamRootPath, opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.merged: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log(greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering upstream data"))
	if err := (&IntegratorSharedOwnershipStructuredPreferUpstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferUpstream, upstreamRootPath, opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_upstream: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log(greenBold.Sprint("integrating configured shared-ownership structured resources to merge, prefering downstream data"))
	if err := (&IntegratorSharedOwnershipStructuredPreferDownstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferDownstream, upstreamRootPath, opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_downstream: %v", err)
	}

	for _, postIntegrateMigration := range postIntegrateMigrations {
		fmt.Println("")
		opts.Logger.Log(greenBold.Sprintf("running post-integrate migration defined in upstream against the downstream: %s", postIntegrateMigration.ID))
		if err := runMigration(postIntegrateMigration, upstreamRootPath, opts.DownstreamRepoPath); err != nil {
			return fmt.Errorf("error running post-integrate migration against the downstream: %v", err)
		}
		if err := recordCompleteMigration(postIntegrateMigration.ID, opts.DownstreamRepoPath); err != nil {
			return fmt.Errorf("error recording successful migration result: %v", err)
		}
	}

	fmt.Println("")

	return nil
}

func cloneUpstreamForIntegrate(cloneDir string, opts *IntegrateOptions) error {
	var err error
	var authMethod transport.AuthMethod
	isHTTPsUpstreamURL, _ := regexp.MatchString("^https://.*$", opts.UpstreamRepoURL)
	if isHTTPsUpstreamURL && opts.UpstreamRepoToken != "" {
		authMethod = &http.BasicAuth{
			Username: gitSpork,
			Password: opts.UpstreamRepoToken,
		}
	} else if !isHTTPsUpstreamURL {
		authMethod, err = ssh.NewSSHAgentAuth(gitSSHUsername)
		if err != nil {
			return fmt.Errorf("error setting up SSH auth method for git: %v", err)
		}
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
			return fmt.Errorf("error processing upstream gitsport repo version to use: %v", err)
		}
		if isTag {
			refName = fmt.Sprintf("refs/%s", opts.UpstreamRepoVersion)
		}
		cloneOptions.ReferenceName = plumbing.ReferenceName(refName)
	}
	_, err = git.PlainClone(cloneDir, cloneOptions)
	if err != nil {
		return fmt.Errorf("error cloning upstream gitspork repo: %v", err)
	}
	return nil
}

func getIntegrateFiles(inDir string, configuredGlobPatterns []string) ([]string, error) {
	allFiles := []string{}
	makeFilesRelativePath := func(filesList []string) ([]string, error) {
		result := []string{}
		re, err := regexp.Compile(fmt.Sprintf("^%s%s", inDir, string(filepath.Separator)))
		if err != nil {
			return result, err
		}
		for _, filesListItem := range filesList {
			result = append(result, re.ReplaceAllString(filesListItem, ""))
		}
		return result, nil
	}
	for _, configuredGlobPattern := range configuredGlobPatterns {
		files, err := filepath.Glob(filepath.Join(inDir, configuredGlobPattern))
		if err != nil {
			return allFiles, err
		}
		filesRelativePath, err := makeFilesRelativePath(files)
		if err != nil {
			return allFiles, err
		}
		allFiles = append(allFiles, filesRelativePath...)
	}
	return allFiles, nil
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

func loadDownstreamState(downstreamRepoPath string) (*GitSporkDownstreamState, error) {
	gitSporkMetaDir, err := ensureDownstreamMetaDir(downstreamRepoPath)
	if err != nil {
		return nil, err
	}
	state := &GitSporkDownstreamState{}
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
	return state, nil
}

func saveDownstreamState(downstreamRepoPath string, state *GitSporkDownstreamState) error {
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
	state, err := loadDownstreamState(downstreamRepoPath)
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

func runMigration(migrationInstructions *GitSporkConfigMigrationInstructions, upstreamRepoRootPath string, downstreamRepoPath string) error {
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
	state, err := loadDownstreamState(downstreamRepoPath)
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
	return saveDownstreamState(downstreamRepoPath, state)
}
