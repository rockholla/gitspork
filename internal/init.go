package internal

import (
	"fmt"
	"os"
	"path/filepath"
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
	if err := WriteGitSporkConfig(filepath.Join(initPath, gitSporkConfigFileName), initConfig, gitSporkConfigHeader); err != nil {
		return fmt.Errorf("error initializing gitspork config: %v", err)
	}

	logger.Log("successfully created %s at %s, see that file for more info on what to set and a link to the docs", gitSporkConfigFileName, initPath)
	return nil
}
