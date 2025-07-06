package internal

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

const (
	gitSporkConfigHeader string = "# For full docs on how to use this config, see https://github.com/rockholla/gitspork/docs\n"
)

// Init will initialize a path for use as a gitspork upstream
func Init(initPath string, gitSporkVersion string, logger *Logger) error {
	var err error

	logger.Log("initializing gitspork upstream at %s", initPath)
	if initPath == "" {
		initPath, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("unable to get the present working directory: %v", err)
		}
	} else {
		initPath, err = filepath.Abs(initPath)
		if err != nil {
			return fmt.Errorf("error determining init path: %v", err)
		}
	}
	initConfig := &GitSporkConfig{
		Version: gitSporkVersion,
	}
	yamlBytes, err := yaml.Marshal(initConfig)
	if err != nil {
		return fmt.Errorf("error initializing YAML configuration: %v", err)
	}
	yamlBytes = append([]byte(gitSporkConfigHeader), yamlBytes...)
	if err := os.WriteFile(filepath.Join(initPath, gitSporkConfigFileName), yamlBytes, 0644); err != nil {
		return fmt.Errorf("error initializing YAML configuration: %v", err)
	}
	logger.Log("successfully created %s at %s, see that file for more info on what to set and a link to the docs", gitSporkConfigFileName, initPath)

	return nil
}
