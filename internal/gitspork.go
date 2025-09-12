package internal

import (
	"fmt"
	"os"

	"github.com/rockholla/go-lib/marshal"
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
	UpstreamOwned   []string                      `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the upstream gitspork repo"`
	DownstreamOwned []string                      `yaml:"downstream_owned" comment:"file patterns (https://github.com/gobwas/glob) that should be treated as fully-owned by the downstream repo once it's been initially integrated"`
	SharedOwnership GitSporkConfigSharedOwnership `yaml:"shared_ownership" comment:"file patterns (https://github.com/gobwas/glob) that will be owned by both the upstream and downstream repos in some managed way"`
	Migrations      []string                      `yaml:"migrations" comment:"list of YAML file paths in the upstream repo, relative to the upstream repo root or subpath if specified, containing downstream repo migration instructions"`
}

// GitSporkConfigSharedOwnership represents config for what files will have shared ownership
type GitSporkConfigSharedOwnership struct {
	Merged     []string                                `yaml:"merged" comment:"file patterns (https://github.com/gobwas/glob) that should be treated as owned by both the upstream and downstream repos, with the ability for the upstream to own blocks w/in these types of files"`
	Structured GitSporkConfigSharedOwnershipStructured `yaml:"structured" comment:"file patterns (https://github.com/gobwas/glob) that contain structured data to maintain on both the upstream and downstream side, e.g. json/yaml configuration files"`
}

// GitSporkConfigSharedOwnershipStructured represents config for what files will have shared ownership of structured data in yaml or json format
type GitSporkConfigSharedOwnershipStructured struct {
	PreferUpstream   []string `yaml:"prefer_upstream" comment:"file patterns (https://github.com/gobwas/glob) that contain common structure data to merge, prefering the values set in the upstream repo"`
	PreferDownstream []string `yaml:"prefer_downstream" comment:"file patterns (https://github.com/gobwas/glob) that contain common structure data to merge, prefering the values set in the downstream repo"`
}

// GitSporkConfigMigration represents config for a single downstream repo migration
type GitSporkConfigMigration struct {
	PreIntegrate  *GitSporkConfigMigrationInstructions `yaml:"pre_integrate,omitempty"`
	PostIntegrate *GitSporkConfigMigrationInstructions `yaml:"post_integrate,omitempty"`
}

// GitSporkConfigMigrationInstruction provides specific instructions for a migration operation/set of operations
type GitSporkConfigMigrationInstructions struct {
	ID   string `yaml:"-"`
	Exec string `yaml:"exec" comment:"command, or path to a script relative to the upstream repo root or subpath if specified, to execute in the downstream repo as a migration-related operation"`
}

// GitSporkDownstreamState represents state stored in the downstream repo to track integrations, etc.
type GitSporkDownstreamState struct {
	MigrationsComplete []string `json:"migrations_complete" comment:"list of migration IDs that have completed successfully against the downstream repo"`
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

// ParseMigrationConfig will read a migration config YAML file, parse its instruction and return the parsed data
func ParseMigrationConfig(migrationConfigPath string) (*GitSporkConfigMigration, error) {
	migration := &GitSporkConfigMigration{}
	f, err := os.ReadFile(migrationConfigPath)
	if err != nil {
		return migration, fmt.Errorf("error reading gitspork migration config file %s: %v", migrationConfigPath, err)
	}
	err = yaml.Unmarshal(f, migration)
	if err != nil {
		return migration, fmt.Errorf("error parsing gitspork migration config file %s: %v", migrationConfigPath, err)
	}
	return migration, nil
}

// GetGitSporkConfigSchema will render a version of the .gitspork.yml config w/ comments as a schema-like documentation source
func GetGitSporkConfigSchema() (string, string, error) {
	gitSporkExampleConfig := &GitSporkConfig{
		Version:         "v0.1.0",
		UpstreamOwned:   []string{"upstream-owned.txt"},
		DownstreamOwned: []string{"downstream-owned.md"},
		SharedOwnership: GitSporkConfigSharedOwnership{
			Merged: []string{"shared-ownership-merged.txt"},
			Structured: GitSporkConfigSharedOwnershipStructured{
				PreferUpstream:   []string{"shared-ownership-prefer-upstream.json"},
				PreferDownstream: []string{"shared-ownership-prefer-downstream.json"},
			},
		},
		Migrations: []string{".gitspork/migrations/0001/migration.yml"},
	}
	migrationExampleConfig := &GitSporkConfigMigration{
		PreIntegrate: &GitSporkConfigMigrationInstructions{
			Exec: "./.gitspork/migrations/0001/pre-integrate.sh",
		},
		PostIntegrate: &GitSporkConfigMigrationInstructions{
			Exec: "./.gitspork/migrations/0001/post-integrate.sh",
		},
	}
	renderedMain, err := marshal.YAMLWithComments(gitSporkExampleConfig, 2)
	if err != nil {
		return "", "", err
	}
	renderedMigration, err := marshal.YAMLWithComments(migrationExampleConfig, 2)
	if err != nil {
		return "", "", err
	}
	return renderedMain, renderedMigration, nil
}
