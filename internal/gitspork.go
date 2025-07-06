package internal

import (
	"fmt"
	"os"

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

// GitSporkConfig represents the config an upstream repo defines in .gitspork.yml
type GitSporkConfig struct {
	Version         string                        `yaml:"version" comment:"version of gitspork relevant to the config"`
	UpstreamOwned   []string                      `yaml:"upstream_owned" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that should be treated as fully-owned by the upstream gitspork repo"`
	DownstreamOwned []string                      `yaml:"downstream_owned" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that should be treated as fully-owned by the downstream repo once it's been initially integrated"`
	SharedOwnership GitSporkConfigSharedOwnership `yaml:"shared_ownership" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that will be owned by both the upstream and downstream repos in some managed way"`
}

// GitSporkConfigSharedOwnership represents config for what files will have shared ownership
type GitSporkConfigSharedOwnership struct {
	Merged     []string                                `yaml:"merged" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that should be treated as owned by both the upstream and downstream repos, with the ability for the upstream to own blocks w/in these types of files"`
	Structured GitSporkConfigSharedOwnershipStructured `yaml:"structured" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that contain structured data to maintain on both the upstream and downstream side, e.g. json/yaml configuration files"`
}

// GitSporkConfigSharedOwnershipStructured represents config for what files will have shared ownership of structured data in yaml or json format
type GitSporkConfigSharedOwnershipStructured struct {
	PreferUpstream   []string `yaml:"prefer_upstream" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that contain common structure data to merge, prefering the values set in the upstream repo"`
	PreferDownstream []string `yaml:"prefer_downstream" comment:"file patterns (https://pkg.go.dev/path/filepath#Match) that contain common structure data to merge, prefering the values set in the downstream repo"`
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
