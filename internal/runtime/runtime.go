// Package runtime runs docker or podman compose for a rendered fortress.
package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	goruntime "runtime"
	"strings"
)

// Supported runtimes.
var Supported = []string{"docker", "podman"}

// ErrNotLinux is returned when hosting is attempted on a non-Linux OS.
var ErrNotLinux = errors.New("hosting a fortress is supported on Linux only; run this on the Linux host. validate, backup, breach and status work on this OS")

// Runtime is a container engine CLI.
type Runtime struct {
	Name string
	Log  io.Writer
	goos string
}

// New validates name and returns a runtime for the current OS.
func New(name string, log io.Writer) (*Runtime, error) {
	for _, s := range Supported {
		if s == name {
			return &Runtime{Name: name, Log: log, goos: goruntime.GOOS}, nil
		}
	}
	return nil, fmt.Errorf("unknown runtime %q (supported: %s)", name, strings.Join(Supported, ", "))
}

// ComposeArgs returns the arguments for bringing the fortress up.
func ComposeArgs(file, project string) []string {
	return []string{"compose", "-f", file, "-p", project, "up", "-d"}
}

// Up runs `<runtime> compose ... up -d`.
func (r *Runtime) Up(ctx context.Context, file, project string) error {
	if r.goos != "linux" {
		return ErrNotLinux
	}
	bin, err := exec.LookPath(r.Name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH; install it or choose another with --runtime (%s)", r.Name, strings.Join(Supported, ", "))
	}
	args := ComposeArgs(file, project)
	if r.Log != nil {
		fmt.Fprintf(r.Log, "$ %s %s\n", r.Name, strings.Join(args, " "))
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s compose up: %w: %s%s", r.Name, err, strings.TrimSpace(stderr.String()), Hint(r.Name, stderr.String()))
	}
	return nil
}

// Hint turns common engine failures into an actionable suggestion.
func Hint(name, stderr string) string {
	s := strings.ToLower(stderr)
	switch {
	case strings.Contains(s, "permission denied") && strings.Contains(s, "docker.sock"):
		return "\nhint: your user cannot reach the docker socket; run `sudo usermod -aG docker $USER` and log in again, or use --runtime podman"
	case strings.Contains(s, "cannot connect to the docker daemon"):
		return "\nhint: the docker daemon is not running; start it with `sudo systemctl start docker`"
	case strings.Contains(s, "unknown command") && strings.Contains(s, "compose"),
		strings.Contains(s, "is not a docker command"):
		return fmt.Sprintf("\nhint: %s compose is not installed; install the compose plugin (docker-compose-plugin or podman-compose)", name)
	}
	return ""
}
