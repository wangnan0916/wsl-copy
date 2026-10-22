package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type configuration struct {
	SourceRoot  string `json:"source_root"`
	WindowsRoot string `json:"windows_root"`
	Replace     bool   `json:"replace"`
	LockFile    string `json:"lock_file"`
	WindowsTar  string `json:"windows_tar"`
	PowerShell  string `json:"powershell"`
}

func loadConfig(path string, explicit bool) (configuration, error) {
	if explicit && path == "" {
		return configuration{}, nil
	}
	if !explicit {
		dir, err := os.UserConfigDir()
		if err != nil {
			return configuration{}, fmt.Errorf("find config directory: %w", err)
		}
		path = filepath.Join(dir, "wsl-copy", "config.json")
	}

	file, err := os.Open(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return configuration{}, nil
		}
		return configuration{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var cfg *configuration
	if err := decoder.Decode(&cfg); err != nil {
		return configuration{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	if cfg == nil {
		return configuration{}, fmt.Errorf("config %q must contain a JSON object", path)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return configuration{}, fmt.Errorf("config %q must contain only one JSON object", path)
	}
	return *cfg, nil
}
