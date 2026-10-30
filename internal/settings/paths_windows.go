package settings

import (
	"os"
	"path/filepath"
)

func configDir() (string, error) {
	dir, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Shixin"), nil
}

func rimeUserDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Rime"), nil
}
