// Package statefile replaces private metadata without truncating the previous version.
package statefile

import (
	"os"
	"path/filepath"
)

func Replace(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sapper-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
