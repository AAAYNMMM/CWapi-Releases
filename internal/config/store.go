package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func DefaultPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("CONFIG_EXECUTABLE_PATH_FAILED: %w", err)
	}
	root := filepath.Dir(executable)
	return filepath.Join(root, "CWapi-data", "config", "cwapi.json"), nil
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 4*1024*1024))
	decoder.DisallowUnknownFields()
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("CONFIG_DECODE_FAILED: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, fmt.Errorf("CONFIG_TRAILING_DATA")
		}
		return Config{}, fmt.Errorf("CONFIG_TRAILING_DATA: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	cfg.PermissionMode = EffectivePermissionMode(cfg.PermissionMode)
	return cfg.Clone(), nil
}

func SaveAtomic(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("CONFIG_DIRECTORY_CREATE_FAILED: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".cwapi-config-*.tmp")
	if err != nil {
		return fmt.Errorf("CONFIG_TEMP_CREATE_FAILED: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("CONFIG_TEMP_PERMISSION_FAILED: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("CONFIG_ENCODE_FAILED: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("CONFIG_TEMP_SYNC_FAILED: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("CONFIG_TEMP_CLOSE_FAILED: %w", err)
	}
	if err := replaceFileAtomic(temporaryPath, path); err != nil {
		return fmt.Errorf("CONFIG_ATOMIC_REPLACE_FAILED: %w", err)
	}
	committed = true
	return nil
}
