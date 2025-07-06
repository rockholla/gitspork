package internal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

const (
	gitSpork                  string = "gitspork"
	gitSSHUsername            string = "git"
	gitSporkConfigFileName    string = ".gitspork.yml"
	gitSporkConfigFileNameAlt string = ".gitspork.yaml"
	gitSporkMarkerSeparator   string = "::"
)

var (
	gitSporkCommentMarker string = fmt.Sprintf("%s%s%s", gitSporkMarkerSeparator, gitSpork, gitSporkMarkerSeparator)
)

// GitSporkFilesIntegrator defines the common implementation requirements for any thing processing a list of files to integrate b/w upstream/downstream
type GitSporkFilesIntegrator interface {
	Integrate(filePaths []string, upstreamRepoClonePath string, downstreamRepoClonePath string, logger *Logger) error
}

// GitSporkConfig represents the config an upstream repo defines in .gitspork.yml
type GitSporkConfig struct {
	Version         string                        `yaml:"version" comment:"version of gitspork relevant to the config"`
	UpstreamOwned   []string                      `yaml:"upstream_owned" comment:"files that should be treated as fully-owned by the upstream gitspork repo"`
	DownstreamOwned []string                      `yaml:"downstream_owned" comment:"files that should be treated as fully-owned by the downstream repo once it's been initially integrated"`
	SharedOwnership GitSporkConfigSharedOwnership `yaml:"shared_ownership" comment:"files that will be owned by both the upstream and downstream repos in some managed way"`
}

// GitSporkConfigSharedOwnership represents config for what files will have shared ownership
type GitSporkConfigSharedOwnership struct {
	Merged     []string                                `yaml:"merged" comment:"files that should be treated as owned by both the upstream and downstream repos, with the ability for the upstream to own blocks w/in these types of files"`
	Structured GitSporkConfigSharedOwnershipStructured `yaml:"structured" comment:"files that contain structured data to maintain on both the upstream and downstream side, e.g. json/yaml configuration files"`
}

// GitSporkConfigSharedOwnershipStructured represents config for what files will have shared ownership of structured data in yaml or json format
type GitSporkConfigSharedOwnershipStructured struct {
	PreferUpstream   []string `yaml:"prefer_upstream" comment:"files that contain common structure data to merge, prefering the values set in the upstream repo"`
	PreferDownstream []string `yaml:"prefer_downstream" comment:"files that contain common structure data to merge, prefering the values set in the downstream repo"`
}

// IntegrateOptions are options for the Integrate method
type IntegrateOptions struct {
	Logger              *Logger
	UpstreamRepoURL     string
	UpstreamRepoVersion string
	UpstreamRepoSubpath string
	UpstreamRepoToken   string
	DownstreamRepoPath  string
}

// ParseGitSporkConfig will parse a .gitspork.yml config file at the provided path
func ParseGitSporkConfig(gitSporkConfigFilePath string) (*GitSporkConfig, error) {
	config := &GitSporkConfig{}
	f, err := os.ReadFile(gitSporkConfigFilePath)
	if err != nil {
		return config, fmt.Errorf("error reading gitspork config file %s: %v", gitSporkConfigFilePath, err)
	}
	err = yaml.Unmarshal(f, config)
	if err != nil {
		return config, fmt.Errorf("error parsing gitspork config file %s: %v", gitSporkConfigFilePath, err)
	}

	return config, nil
}

func copyFile(src, dst string) error {
	// Open the source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()

	// Ensure the destination directory
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("error ensuring destination directory path exists: %v", err)
	}

	// Create the destination file
	destinationFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}
	defer destinationFile.Close()

	// Copy the content
	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %v", err)
	}

	// Flush file metadata to disk
	err = destinationFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync destination file: %v", err)
	}

	return nil
}
