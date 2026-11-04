// Package restic runs the restic binary.
package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// ErrNotInstalled is returned when the restic binary cannot be found.
var ErrNotInstalled = errors.New("restic not found in PATH; install it from https://restic.net or your package manager")

// ErrNoSnapshot is returned when the repository holds no matching snapshot.
var ErrNoSnapshot = errors.New("no snapshot found; run `sapper backup` first")

// ErrNoRepository is returned when there is no restic repository at the configured location.
var ErrNoRepository = errors.New("no restic repository at the configured location yet; `sapper backup` creates it")

// Client runs restic against one repository.
type Client struct {
	Bin        string
	Repository string
	Password   string
	// DropEnv names extra variables (such as the blueprint's password_env) that are not
	// passed to restic, so the password reaches the child only once, as RESTIC_PASSWORD.
	DropEnv []string
	// Log, when set, receives every command line before it runs. The password is never included.
	Log io.Writer
}

// New locates restic and returns a client. password must be non-empty.
func New(repository, password string) (*Client, error) {
	bin, err := exec.LookPath("restic")
	if err != nil {
		return nil, ErrNotInstalled
	}
	if password == "" {
		return nil, errors.New("repository password is empty")
	}
	return &Client{Bin: bin, Repository: repository, Password: password}, nil
}

func (c *Client) cmd(ctx context.Context, args ...string) *exec.Cmd {
	if c.Log != nil {
		fmt.Fprintf(c.Log, "$ restic %s\n", strings.Join(args, " "))
	}
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Env = append(filteredEnv(c.DropEnv...), "RESTIC_REPOSITORY="+c.Repository, "RESTIC_PASSWORD="+c.Password)
	return cmd
}

// filteredEnv drops inherited restic variables so only the blueprint decides the target.
func filteredEnv(drop ...string) []string {
	var out []string
outer:
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "RESTIC_REPOSITORY", "RESTIC_REPOSITORY_FILE", "RESTIC_PASSWORD", "RESTIC_PASSWORD_FILE", "RESTIC_PASSWORD_COMMAND":
			continue
		}
		for _, d := range drop {
			if k == d {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := c.cmd(ctx, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("restic %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// EnsureRepo initialises the repository if it does not exist yet.
func (c *Client) EnsureRepo(ctx context.Context) (created bool, err error) {
	cmd := c.cmd(ctx, "cat", "config")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return false, nil
	}
	var exit *exec.ExitError
	// restic exits 10 when the repository does not exist (0.17+).
	if errors.As(err, &exit) && exit.ExitCode() == 10 {
		_, err := c.run(ctx, "init")
		return err == nil, err
	}
	return false, fmt.Errorf("restic cat config: %w: %s", err, strings.TrimSpace(stderr.String()))
}

// Backup backs up paths with tags and returns the new snapshot ID.
func (c *Client) Backup(ctx context.Context, paths, tags []string) (string, error) {
	args := []string{"backup", "--json"}
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	// "--" ends option parsing so a path can never be read as a restic flag.
	args = append(args, "--")
	args = append(args, paths...)
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	return parseSnapshotID(out)
}

func parseSnapshotID(out []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var msg struct {
			Type       string `json:"message_type"`
			SnapshotID string `json:"snapshot_id"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) == nil && msg.Type == "summary" && msg.SnapshotID != "" {
			return msg.SnapshotID, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", errors.New("restic backup: no summary with snapshot_id in output")
}

// Snapshot is the subset of restic snapshot metadata sapper uses.
type Snapshot struct {
	ID    string   `json:"id"`
	Time  string   `json:"time"`
	Paths []string `json:"paths"`
	Tags  []string `json:"tags"`
}

// Latest returns the newest snapshot carrying tag.
func (c *Client) Latest(ctx context.Context, tag string) (Snapshot, error) {
	out, err := c.run(ctx, "snapshots", "--json", "--tag", tag, "--latest", "1")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 10 {
		return Snapshot{}, ErrNoRepository
	}
	if err != nil {
		return Snapshot{}, err
	}
	return parseLatest(out)
}

func parseLatest(out []byte) (Snapshot, error) {
	var snaps []Snapshot
	if err := json.Unmarshal(out, &snaps); err != nil {
		return Snapshot{}, fmt.Errorf("restic snapshots: %w", err)
	}
	if len(snaps) == 0 {
		return Snapshot{}, ErrNoSnapshot
	}
	latest := snaps[0]
	for _, s := range snaps[1:] {
		if s.Time > latest.Time {
			latest = s
		}
	}
	return latest, nil
}

// Restore restores snapshot id into target.
func (c *Client) Restore(ctx context.Context, id, target string) error {
	_, err := c.run(ctx, "restore", id, "--target", target)
	return err
}

// Forget applies keep-last retention to snapshots with tag and prunes.
func (c *Client) Forget(ctx context.Context, tag string, keepLast int) error {
	if keepLast <= 0 {
		return nil
	}
	_, err := c.run(ctx, "forget", "--tag", tag, "--keep-last", fmt.Sprint(keepLast), "--prune")
	return err
}
