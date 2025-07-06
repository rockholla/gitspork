package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/plumbing/transport/http"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"gopkg.in/yaml.v2"
)

const (
	structuredDataTypeYAML = "yaml"
	structuredDataTypeJSON = "json"
)

var (
	structuredDataYAMLExtensions []string = []string{".yaml", ".yml"}
	structuredDataJSONExtensions []string = []string{".json"}
)

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
	opts.Logger.Log("parsing the gitspork config file in the upstream repo clone at %s or %s", gitSporkConfigFileName, gitSporkConfigFileNameAlt)
	gitSporkConfigFilePath := filepath.Join(cloneDir, opts.UpstreamRepoSubpath, gitSporkConfigFileName)
	if _, err := os.Stat(gitSporkConfigFilePath); os.IsNotExist(err) {
		gitSporkConfigFilePath = filepath.Join(cloneDir, opts.UpstreamRepoSubpath, gitSporkConfigFileNameAlt)
		if _, err := os.Stat(gitSporkConfigFilePath); os.IsNotExist(err) {
			return fmt.Errorf("it looks like %s does not include a %s or %s config file", opts.UpstreamRepoURL, gitSporkConfigFileName, gitSporkConfigFileNameAlt)
		}
	}
	gitSporkConfig, err := ParseGitSporkConfig(gitSporkConfigFilePath)
	if err != nil {
		return err
	}

	fmt.Println("")
	opts.Logger.Log("integrating configured upstream-owned resources from upstream to downstream")
	if err := (&IntegratorUpstreamOwned{}).Integrate(gitSporkConfig.UpstreamOwned, filepath.Join(cloneDir, opts.UpstreamRepoSubpath), opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating upstream-owned: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log("integrating configured downstream-owned resources from upstream to downstream")
	if err := (&IntegratorDownstreamOwned{}).Integrate(gitSporkConfig.DownstreamOwned, filepath.Join(cloneDir, opts.UpstreamRepoSubpath), opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating downstream-owned: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log("integrating configured shared-ownership generic resources to merge b/w upstream and downstream")
	if err := (&IntegratorSharedOwnershipMerged{}).Integrate(gitSporkConfig.SharedOwnership.Merged, filepath.Join(cloneDir, opts.UpstreamRepoSubpath), opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.merged: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log("integrating configured shared-ownership structured resources to merge, prefering upstream data")
	if err := (&IntegratorSharedOwnershipStructuredPreferUpstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferUpstream, filepath.Join(cloneDir, opts.UpstreamRepoSubpath), opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_upstream: %v", err)
	}

	fmt.Println("")
	opts.Logger.Log("integrating configured shared-ownership structured resources to merge, prefering downstream data")
	if err := (&IntegratorSharedOwnershipStructuredPreferDownstream{}).Integrate(gitSporkConfig.SharedOwnership.Structured.PreferDownstream, filepath.Join(cloneDir, opts.UpstreamRepoSubpath), opts.DownstreamRepoPath, opts.Logger); err != nil {
		return fmt.Errorf("error integrating shared-ownership.structured.prefer_downstream: %v", err)
	}

	return nil
}

func cloneUpstreamForIntegrate(cloneDir string, opts *IntegrateOptions) error {
	var err error
	var authMethod transport.AuthMethod
	if opts.UpstreamRepoToken != "" {
		authMethod = &http.BasicAuth{
			Username: gitSpork,
			Password: opts.UpstreamRepoToken,
		}
	} else {
		authMethod, err = ssh.NewSSHAgentAuth(gitSSHUsername)
		if err != nil {
			return fmt.Errorf("error setting up SSH auth method for git: %v", err)
		}
	}
	cloneOptions := &git.CloneOptions{
		URL:          opts.UpstreamRepoURL,
		Auth:         authMethod,
		SingleBranch: true,
		Progress:     os.Stdout,
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

func getStructuredData(upstreamPath string, downstreamPath string) (*map[string]interface{}, *map[string]interface{}, string, error) {
	var err error
	upstreamData := &map[string]interface{}{}
	downstreamData := &map[string]interface{}{}

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
		if err := copyFile(upstreamPath, downstreamPath); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error copying structured data file %s to downstream: %v", upstreamPath, err)
		}
	}
	downstreamPathBytes, err := os.ReadFile(downstreamPath)
	if err != nil {
		return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error reading file %s", upstreamPath)
	}
	if structuredDataType == structuredDataTypeYAML {
		if err := yaml.Unmarshal(upstreamPathBytes, upstreamData); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error parsing upstream file %s: %v", upstreamPath, err)
		}
		if err := yaml.Unmarshal(downstreamPathBytes, downstreamData); err != nil {
			return upstreamData, downstreamData, structuredDataType, fmt.Errorf("error parsing downstream file %s: %v", upstreamPath, err)
		}
	} else if structuredDataType == structuredDataTypeJSON {
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
	if structuredDataType == structuredDataTypeYAML {
		b, err = yaml.Marshal(structuredData)
		if err != nil {
			return err
		}
	} else if structuredDataType == structuredDataTypeJSON {
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
