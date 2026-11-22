package configfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gosync", "default.yaml"), nil
}

func StartupArgs(args []string) ([]string, error) {
	if len(args) != 0 {
		return args, nil
	}
	config, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(config); err != nil {
		if os.IsNotExist(err) {
			legacy := strings.TrimSuffix(config, ".yaml") + ".json"
			if _, legacyErr := os.Stat(legacy); legacyErr == nil {
				return []string{"--config", legacy}, nil
			} else if !os.IsNotExist(legacyErr) {
				return nil, legacyErr
			}
			return nil, fmt.Errorf("no saved configuration at %s; choose a folder first: gosync init --sync-folder /path/to/shared", config)
		}
		return nil, err
	}
	return []string{"--config", config}, nil
}
