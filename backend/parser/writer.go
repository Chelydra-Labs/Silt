package parser

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes content to a temporary file, flushes it to disk,
// and atomically renames it to the target path.
func WriteFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"

	// Create or truncate the temp file
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	// Defer cleanup of the temp file in case we return early with an error
	defer func() {
		if _, err := os.Stat(tmpPath); err == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	// Write content
	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}

	// Flush to storage hardware
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}

	// Close the file before renaming
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Atomically rename
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	return nil
}
