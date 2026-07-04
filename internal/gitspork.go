package internal

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/rockholla/go-lib/marshal"
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
	UpstreamOwned   []OwnedEntry                  `yaml:"upstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the upstream; an entry may instead be a {from, to} map to rename a file as it syncs to the downstream"`
	DownstreamOwned []OwnedEntry                  `yaml:"downstream_owned" comment:"file patterns (https://github.com/gobwas/glob) fully owned by the downstream once initially integrated; an entry may instead be a {from, to} map to seed a file at a different downstream path"`
	SharedOwnership GitSporkConfigSharedOwnership `yaml:"shared_ownership" comment:"file patterns (https://github.com/gobwas/glob) that will be owned by both the upstream and downstream repos in some managed way"`
	Templated       []GitSporkConfigTemplated     `yaml:"templated" comment:"list of instruction for templated source files in the upstream that should be rendered in some way to a location in the downstream"`
	Migrations      []string                      `yaml:"migrations" comment:"list of YAML file paths in the upstream repo, relative to the upstream repo root or subpath if specified, containing downstream repo migration instructions"`

	// comments holds user-written YAML comments captured on parse, re-injected on write.
	comments yaml.CommentMap `yaml:"-"`
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

// GitSporkDownstreamState represents state stored in the downstream repo to track integrations.
type GitSporkDownstreamState struct {
	MigrationsComplete []string                `json:"migrations_complete"`
	Upstreams          []GitSporkUpstreamState `json:"upstreams,omitempty"`
	// Deprecated: migrated to Upstreams on first load.
	LastUpstreamRepoURL     string `json:"last_upstream_repo_url,omitempty"`
	LastUpstreamRepoSubpath string `json:"last_upstream_repo_subpath,omitempty"`
	LastUpstreamCommitHash  string `json:"last_upstream_commit_hash,omitempty"`
}

// IntegrateResult is the structural return value of Integrate and IntegrateLocal.
// It records what was successfully integrated (in order); on partial failure
// the successful upstreams so far are still present in this result alongside
// the returned error.
type IntegrateResult struct {
	Upstreams []IntegratedUpstream
}

// IntegratedUpstream identifies a single successfully integrated upstream.
type IntegratedUpstream struct {
	URL        string
	Subpath    string
	CommitHash string
}

// DriftReport is the structural return value of CheckDrift. HasDrift is false
// when the downstream matches the recorded integration state; true when
// differences were found. Files enumerates the drifted entries with per-file
// attribution to whichever upstream last wrote each path.
type DriftReport struct {
	HasDrift bool
	Files    []DriftedFile
}

// DriftedFile is a single entry in a DriftReport.
type DriftedFile struct {
	Path          string
	AttributedURL string // upstream URL responsible for this file; empty means unattributed
	Diff          string // unified-diff text for just this file; empty for binary or unattributable
}

// UpstreamSpec is a parsed --upstream flag entry.
type UpstreamSpec struct {
	URL     string
	Version string
	Subpath string
	Token   string
}

// GitSporkUpstreamState records the last integration for a single upstream.
type GitSporkUpstreamState struct {
	URL        string `json:"url"`
	Subpath    string `json:"subpath,omitempty"`
	CommitHash string `json:"commit_hash"`
}

// GitSporkConfigTemplated is a single templated/render template instruction from upstream -> downstream
type GitSporkConfigTemplated struct {
	Template    string                         `yaml:"template" comment:"source path of the Go template file to use in the upstream"`
	Destination string                         `yaml:"destination" comment:"destination path and file name in the dowstream where the template will be rendered"`
	Inputs      []GitSporkConfigTemplatedInput `yaml:"inputs" comment:"list of inputs to provide to the template, and how to determine them"`
	Merged      *GitSporkConfigTemplatedMerged `yaml:"merged,omitempty" comment:"optional instruction for merging with pre-existing file in the destination, if present, post-render"`
}

// GitSporkConfigTemplatedInput
type GitSporkConfigTemplatedInput struct {
	Name          string                                `yaml:"name" comment:"name of the input as defined in the template like 'index .Inputs \"[name]\"'"`
	Prompt        string                                `yaml:"prompt,omitempty" comment:"(optional, one-of required) prompt to present to the user in order to gather the input value"`
	JSONDataPath  string                                `yaml:"json_data_path,omitempty" comment:"(optional, one-of required) JSON data file path (relative to the downstream path) containing the input value at the root property equal to the 'name'. Contract is that downstream is responsible for maintaining this path."`
	PreviousInput *GitSporkConfigTemplatedInputPrevious `yaml:"previous_input,omitempty" comment:"(optional, one-of-required) reference to an input already known from this template or another template defined before this one"`
}

type GitSporkConfigTemplatedInputPrevious struct {
	Template string `yaml:"template" comment:"Name of a previous template defined in the gitspork config from which to pull the value"`
	Name     string `yaml:"name" comment:"Name of the input from that template from which to pull the value"`
}

// GitSporkConfigTemplatedMerged
type GitSporkConfigTemplatedMerged struct {
	Structured string `yaml:"structured" comment:"instruction for a structured merged post-render, either 'prefer-upstream' or 'prefer-downstream'"`
}

// IntegrateOptions are options for the Integrate method
type IntegrateOptions struct {
	Logger                 *Logger
	UpstreamRepoURL        string
	UpstreamRepoVersion    string
	UpstreamRepoCommit     string
	UpstreamRepoSubpath    string
	UpstreamRepoToken      string
	DownstreamRepoPath     string
	ForceRePrompt          bool
	ForDriftCheck          bool
	PrevUpstreamCommitHash string
	Upstreams              []UpstreamSpec
}

// IntegrateLocalOptions are options for the IntegrateLocal method
type IntegrateLocalOptions struct {
	Logger         *Logger
	UpstreamPath   string
	UpstreamPaths  []string
	DownstreamPath string
	ForceRePrompt  bool
}

// CheckDriftOptions are options for the CheckDrift method
type CheckDriftOptions struct {
	Logger             *Logger
	DownstreamRepoPath string
	Upstreams          []UpstreamSpec
}

// ParseGitSporkConfig will parse a .gitspork.yml config file at the provided path
func ParseGitSporkConfig(gitSporkConfigFilePath string) (*GitSporkConfig, error) {
	config := &GitSporkConfig{}
	f, err := os.ReadFile(gitSporkConfigFilePath)
	if err != nil {
		return config, fmt.Errorf("error reading gitspork config file %s: %v", gitSporkConfigFilePath, err)
	}
	cm := yaml.CommentMap{}
	if err = yaml.UnmarshalWithOptions(f, config, yaml.CommentToMap(cm)); err != nil {
		return config, fmt.Errorf("error parsing gitspork config file %s: %v", gitSporkConfigFilePath, err)
	}
	config.comments = cm
	for _, e := range config.UpstreamOwned {
		if err := e.Validate(); err != nil {
			return config, fmt.Errorf("invalid upstream_owned entry in %s: %v", gitSporkConfigFilePath, err)
		}
	}
	for _, e := range config.DownstreamOwned {
		if err := e.Validate(); err != nil {
			return config, fmt.Errorf("invalid downstream_owned entry in %s: %v", gitSporkConfigFilePath, err)
		}
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
		UpstreamOwned: []OwnedEntry{
			{Pattern: "upstream-owned.txt"},
			{From: "upstream-owned-renamed-from.txt", To: "downstream-renamed-to.txt"},
		},
		DownstreamOwned: []OwnedEntry{
			{Pattern: "downstream-owned.md"},
			{From: "downstream-owned-seed-from.md", To: "downstream-owned-seed-to.md"},
		},
		SharedOwnership: GitSporkConfigSharedOwnership{
			Merged: []string{"shared-ownership-merged.txt"},
			Structured: GitSporkConfigSharedOwnershipStructured{
				PreferUpstream:   []string{"shared-ownership-prefer-upstream.json"},
				PreferDownstream: []string{"shared-ownership-prefer-downstream.json"},
			},
		},
		Templated: []GitSporkConfigTemplated{
			{
				Template:    "meta.txt.go.tmpl",
				Destination: "meta.txt",
				Merged: &GitSporkConfigTemplatedMerged{
					Structured: templatedMergeStructuredPreferDownstream,
				},
				Inputs: []GitSporkConfigTemplatedInput{
					{
						Name:   "input_one",
						Prompt: "What is the value of input_one?",
					},
					{
						Name:         "input_two",
						JSONDataPath: "./.json/data.json",
					},
					{
						Name: "input_three",
						PreviousInput: &GitSporkConfigTemplatedInputPrevious{
							Template: "meta.txt.go.tmpl",
							Name:     "input_one",
						},
					},
				},
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
	renderedMain, err := marshal.YAMLWithComments(gitSporkExampleConfig, 0)
	if err != nil {
		return "", "", err
	}
	renderedMain = collapsePlainOwnedEntries(renderedMain)
	renderedMigration, err := marshal.YAMLWithComments(migrationExampleConfig, 0)
	if err != nil {
		return "", "", err
	}
	return renderedMain, renderedMigration, nil
}

// WriteGitSporkConfig writes config to configPath, prepending header if non-empty.
// User-written YAML comments captured during ParseGitSporkConfig are re-injected automatically.
func WriteGitSporkConfig(configPath string, config *GitSporkConfig, header ...string) error {
	var b []byte
	var err error
	if config.comments != nil {
		b, err = yaml.MarshalWithOptions(config, yaml.WithComment(config.comments))
	} else {
		b, err = yaml.Marshal(config)
	}
	if err != nil {
		return fmt.Errorf("error marshalling config: %v", err)
	}
	if len(header) > 0 && header[0] != "" {
		b = append([]byte(header[0]), b...)
	}
	return os.WriteFile(configPath, b, 0644)
}

// FindGitSporkConfig walks up from startDir to find .gitspork.yml (or .gitspork.yaml) and returns its path.
func FindGitSporkConfig(startDir string) (string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if p := filepath.Join(dir, gitSporkConfigFileName); fileExists(p) {
			return p, nil
		}
		if p := filepath.Join(dir, gitSporkConfigFileNameAlt); fileExists(p) {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .gitspork.yml found in %s or any parent directory", startDir)
		}
		dir = parent
	}
}
