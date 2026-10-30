package settings

import (
	"os"
	"path/filepath"
)

func configDir() (string, error) {
	dir, err := os.UserConfigDir() // $XDG_CONFIG_HOME
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shixin"), nil
}

func rimeUserDir() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "shixin", "rime"), nil
}
