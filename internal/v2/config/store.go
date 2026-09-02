package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxConfigBytes = 1024 * 1024

func DefaultPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("CONFIG_EXECUTABLE_PATH_FAILED: %w", err)
	}
	return filepath.Join(filepath.Dir(executable), "CWapi-data", "config", "cwapi.json"), nil
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	payload, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("CONFIG_READ_FAILED: %w", err)
	}
	if len(payload) > maxConfigBytes {
		return Config{}, errors.New("CONFIG_TOO_LARGE")
	}

	var cfg Config
	if err := decodeStrict(payload, &cfg); err != nil {
		return Config{}, err
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func LoadOrCreate(path string) (Config, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		if migrated, migrateErr := migratePrevious(path); migrateErr == nil {
			return migrated, nil
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	cfg = Default()
	if err := SaveAtomic(path, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func migratePrevious(path string) (Config, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if len(payload) > maxConfigBytes {
		return Config{}, errors.New("CONFIG_TOO_LARGE")
	}
	var cfg Config
	if err := decodeStrict(payload, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Schema != Schema || cfg.Version != previousVersion {
		return Config{}, errors.New("CONFIG_MIGRATION_NOT_APPLICABLE")
	}
	cfg.Version = Version
	cfg.Codex.RemoteGitRewrite = false
	if err := SaveAtomic(path, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SaveAtomic(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("CONFIG_DIRECTORY_CREATE_FAILED: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".cwapi-v2-config-*.tmp")
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

func decodeStrict(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("CONFIG_DECODE_FAILED: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("CONFIG_TRAILING_DATA")
		}
		return fmt.Errorf("CONFIG_TRAILING_DATA: %w", err)
	}
	return nil
}
