package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func savePrivateJSON(path string, payload any) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create private data directory: %w", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode private data: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create private temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		_ = temporary.Close()
		return fmt.Errorf("protect private temporary file: %w", err)
	}
	if _, err = temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write private data: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close private data: %w", err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace private data: %w", err)
	}
	return nil
}
