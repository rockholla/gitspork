package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rockholla/gitspork/internal/types"
)

const (
	gitSporkConfigHeader string = "# For full docs on how to use this config, see https://github.com/rockholla/gitspork/docs\n"
)

// Init will initialize a path for use as a gitspork upstream
func Init(initPath string, logger types.Logger) error {
	var err error

	if logger == nil {
		logger = types.NoopLogger()
	}
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
	initConfig := &GitSporkConfig{}
	if err := WriteGitSporkConfig(filepath.Join(initPath, GitSporkConfigFileName), initConfig, gitSporkConfigHeader); err != nil {
		return fmt.Errorf("error initializing gitspork config: %v", err)
	}

	logger.Log("successfully created %s at %s, see that file for more info on what to set and a link to the docs", GitSporkConfigFileName, initPath)
	return nil
}
