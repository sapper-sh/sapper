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
	"sort"
	"strings"
	"time"
)

// ErrNotInstalled is returned when the restic binary cannot be found.
var ErrNotInstalled = errors.New("restic not found in PATH; install it from https://restic.net or your package manager")

// ErrNoSnapshot is returned when the repository holds no matching snapshot.
var ErrNoSnapshot = errors.New("no snapshot found; run `sapper backup` first")

// ErrNoRepository is returned when there is no restic repository at the configured location.
var ErrNoRepository = errors.New("no restic repository at the configured location yet; `sapper backup` creates it")

// ErrWrongPassword is returned when restic cannot open the repository with the password.
var ErrWrongPassword = errors.New("wrong repository password or no matching key; check the password source (--password-file, --password-stdin or the blueprint's password_env). Without the password the backups cannot be read by anyone")

// ErrLocked is returned when another restic process, or a crashed one, holds a lock on the repository.
var ErrLocked = errors.New("repository is locked by another restic process")

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
		return stdout.Bytes(), classify(args[0], err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// classify turns restic's exit codes (0.17: 10 no repository, 11 locked, 12 wrong password)
// into errors that say what to do.
func classify(sub string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	var exit *exec.ExitError
	code := -1
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	}
	switch {
	case code == 12 || strings.Contains(msg, "wrong password"):
		return fmt.Errorf("restic %s: %w", sub, ErrWrongPassword)
	case code == 11 || strings.Contains(msg, "repository is already locked"):
		return fmt.Errorf("restic %s: %w: %s\nIf no other backup, prune or check is running, the lock is stale (a killed or crashed run). Inspect it with `restic -r <repository> list locks --no-lock`, then remove stale locks with `restic -r <repository> unlock`. Do not use --remove-all while another restic process may be running",
			sub, ErrLocked, msg)
	}
	return fmt.Errorf("restic %s: %w: %s", sub, err, msg)
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
	return false, classify("cat config", err, stderr.String())
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
	SortByTime(snaps)
	return snaps[len(snaps)-1], nil
}

// Restore restores snapshot id into target.
func (c *Client) Restore(ctx context.Context, id, target string) error {
	// --no-cache: restic's local cache holds tree blobs, and a drill served from the cache
	// would not notice a damaged or deleted pack in the repository itself.
	_, err := c.run(ctx, "restore", "--no-cache", id, "--target", target)
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

// Snapshots returns every snapshot carrying tag, oldest first by timestamp.
func (c *Client) Snapshots(ctx context.Context, tag string) ([]Snapshot, error) {
	out, err := c.run(ctx, "snapshots", "--json", "--tag", tag)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 10 {
		return nil, ErrNoRepository
	}
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	if err := json.Unmarshal(out, &snaps); err != nil {
		return nil, fmt.Errorf("restic snapshots: %w", err)
	}
	SortByTime(snaps)
	return snaps, nil
}

// SortByTime orders snapshots oldest first. Times are parsed, not compared as strings,
// because restic prints the local offset of the host that took each snapshot.
func SortByTime(s []Snapshot) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].When().Before(s[j].When()) })
}

// When parses the snapshot time; the zero time if restic printed something else.
func (s Snapshot) When() time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s.Time)
	return t
}

// RepoExists reports whether a repository is present at the configured location.
func (c *Client) RepoExists(ctx context.Context) (bool, error) {
	cmd := c.cmd(ctx, "cat", "config")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 10:
		return false, nil
	}
	return false, classify("cat config", err, stderr.String())
}

// ForgetIDs removes the given snapshots and prunes data no snapshot uses any more.
func (c *Client) ForgetIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"forget", "--prune", "--"}, ids...)
	_, err := c.run(ctx, args...)
	return err
}

// ManifestFile is the name of a manifest copy inside the repository.
const ManifestFile = "sapper-manifest.json"

// SaveBlob stores data in the repository as its own snapshot of a single file, tagged tags.
func (c *Client) SaveBlob(ctx context.Context, data []byte, tags []string) (string, error) {
	args := []string{"backup", "--json", "--stdin", "--stdin-filename", ManifestFile}
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	var stdout, stderr bytes.Buffer
	cmd := c.cmd(ctx, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(data), &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", classify("backup", err, stderr.String())
	}
	return parseSnapshotID(stdout.Bytes())
}

// LoadBlob reads the file SaveBlob stored in snapshot id, up to max bytes.
func (c *Client) LoadBlob(ctx context.Context, id string, max int64) ([]byte, error) {
	var stdout limitedBuffer
	stdout.max = max
	var stderr bytes.Buffer
	cmd := c.cmd(ctx, "dump", "--no-cache", id, "/"+ManifestFile)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if stdout.over {
			return nil, fmt.Errorf("restic dump: manifest copy larger than %d bytes", max)
		}
		return nil, classify("dump", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	max  int64
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len()+len(p)) > b.max {
		b.over = true
		return 0, errors.New("too large")
	}
	return b.Buffer.Write(p)
}

// Locks returns how many lock files the repository holds.
func (c *Client) Locks(ctx context.Context) (int, error) {
	out, err := c.run(ctx, "list", "locks", "--no-lock")
	if err != nil {
		return 0, err
	}
	return len(strings.Fields(string(out))), nil
}
