package cli

import (
	"errors"
	"fmt"
	"strings"
)

// Exit codes. Documented in README.md; keep in sync.
const (
	ExitOK         = 0 // success; for breach, the drill survived
	ExitError      = 1 // operation failed
	ExitUsage      = 2 // bad flags, arguments or blueprint
	ExitBreached   = 3 // breach drill ran and found damaged or missing data
	ExitUnverified = 4 // breach drill could not run to a verdict (no snapshot, no manifest, restore failed)
	ExitMissingDep = 5 // restic, docker or podman not installed
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func withCode(code int, err error) error {
	if err == nil {
		return nil
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return err
	}
	return &exitError{code, err}
}

func usagef(format string, a ...any) error {
	return &exitError{ExitUsage, fmt.Errorf(format, a...)}
}

func codeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	msg := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand", "accepts ", "requires at least", "if any flags in the group"} {
		if strings.HasPrefix(msg, p) {
			return ExitUsage
		}
	}
	return ExitError
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
