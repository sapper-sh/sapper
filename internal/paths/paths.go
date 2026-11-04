// Package paths locates per-user state directories following each platform's conventions.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// EnvStateDir overrides the state base directory.
const EnvStateDir = "SAPPER_STATE_DIR"

// StateBase returns the per-user directory for sapper state:
//
//	Linux and other Unix: $XDG_STATE_HOME/sapper, default ~/.local/state/sapper
//	macOS:                ~/Library/Application Support/sapper
//	Windows:              %LOCALAPPDATA%\sapper
func StateBase() (string, error) {
	if d := os.Getenv(EnvStateDir); d != "" {
		return filepath.Abs(d)
	}
	return stateBase(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func stateBase(goos string, getenv func(string) string, home func() (string, error)) (string, error) {
	switch goos {
	case "windows":
		if d := getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "sapper"), nil
		}
		return "", errors.New("%LOCALAPPDATA% is not set")
	case "darwin":
		h, err := home()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, "Library", "Application Support", "sapper"), nil
	default:
		if d := getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
			return filepath.Join(d, "sapper"), nil
		}
		h, err := home()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, ".local", "state", "sapper"), nil
	}
}

// FortressState returns the state directory for a fortress whose blueprint lives in dir.
// The directory name combines the fortress name with a hash of dir so two checkouts never collide.
func FortressState(name, dir string) (string, error) {
	base, err := StateBase()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	return filepath.Join(base, "fortresses", name+"-"+hex.EncodeToString(sum[:])[:12]), nil
}
