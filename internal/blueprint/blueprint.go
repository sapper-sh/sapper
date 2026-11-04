// Package blueprint parses and validates fortress.blueprint files.
package blueprint

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/sapper-unbuilt/sapper/internal/catalog"
)

// FileName is the default blueprint file name.
const FileName = "fortress.blueprint"

// Blueprint describes a fortress.
type Blueprint struct {
	Version  int            `yaml:"version"`
	Fortress string         `yaml:"fortress"`
	DataRoot string         `yaml:"data_root"`
	Apps     map[string]App `yaml:"apps"`
	Backup   Backup         `yaml:"backup"`
	Drill    Drill          `yaml:"drill"`

	// Dir is the directory containing the blueprint file; relative paths resolve against it.
	Dir string `yaml:"-"`
}

// App is one app instance. Volumes maps catalogue mount names to host paths.
type App struct {
	Port    int               `yaml:"port"`
	Volumes map[string]string `yaml:"volumes"`
}

// Backup configures the restic repository.
type Backup struct {
	Repository  string `yaml:"repository"`
	PasswordEnv string `yaml:"password_env"`
	KeepLast    int    `yaml:"keep_last"`
}

// Drill configures breach drills.
type Drill struct {
	Schedule string `yaml:"schedule"`
}

var (
	nameRE    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	envNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	cronField = regexp.MustCompile(`^[0-9*/,\-]+$`)
)

// Load reads and validates the blueprint at path.
func Load(path string) (*Blueprint, error) {
	raw, err := readLimited(path, MaxSize)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	bp, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	bp.Dir = filepath.Dir(abs)
	if err := bp.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return bp, nil
}

// MaxSize is the largest blueprint sapper reads. Real blueprints are a few hundred bytes;
// the cap stops a hostile or corrupt file from exhausting memory in the YAML decoder.
const MaxSize = 1 << 20

func readLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, max)
	}
	return raw, nil
}

// Parse decodes YAML strictly: unknown keys are errors.
func Parse(raw []byte) (*Blueprint, error) {
	if len(raw) > MaxSize {
		return nil, fmt.Errorf("parse: blueprint larger than %d bytes", MaxSize)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var bp Blueprint
	if err := dec.Decode(&bp); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &bp, nil
}

// Validate checks the blueprint and returns all problems joined.
func (bp *Blueprint) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if bp.Version != 1 {
		add("version: must be 1, got %d", bp.Version)
	}
	if !nameRE.MatchString(bp.Fortress) {
		add("fortress: %q must be lowercase letters, digits or '-', starting with a letter", bp.Fortress)
	}
	if bp.DataRoot == "" {
		add("data_root: required")
	} else if err := checkPathText(bp.DataRoot); err != nil {
		add("data_root: %v", err)
	}
	if len(bp.Apps) == 0 {
		add("apps: at least one app is required")
	}
	ports := map[int]string{}
	hostPaths := map[string]string{}
	for _, name := range bp.AppNames() {
		app := bp.Apps[name]
		def, ok := catalog.Lookup(name)
		if !ok {
			add("apps.%s: not in catalogue (available: %s)", name, strings.Join(catalog.Names(), ", "))
			continue
		}
		if app.Port < 1 || app.Port > 65535 {
			add("apps.%s.port: %d out of range 1-65535", name, app.Port)
		} else if other, dup := ports[app.Port]; dup {
			add("apps.%s.port: %d already used by %s", name, app.Port, other)
		} else {
			ports[app.Port] = name
		}
		for _, m := range def.Mounts {
			if _, ok := app.Volumes[m.Name]; !ok {
				add("apps.%s.volumes.%s: required", name, m.Name)
			}
		}
		for _, vol := range sortedKeys(app.Volumes) {
			if !def.HasMount(vol) {
				add("apps.%s.volumes.%s: unknown mount for %s", name, vol, name)
				continue
			}
			if app.Volumes[vol] == "" {
				add("apps.%s.volumes.%s: empty path", name, vol)
				continue
			}
			if err := checkPathText(app.Volumes[vol]); err != nil {
				add("apps.%s.volumes.%s: %v", name, vol, err)
				continue
			}
			hp := bp.HostPath(app.Volumes[vol])
			if err := checkHostPath(hp); err != nil {
				add("apps.%s.volumes.%s: %v", name, vol, err)
				continue
			}
			key := name + "." + vol
			for p, owner := range hostPaths {
				if overlaps(p, hp) {
					add("apps.%s.volumes.%s: path %s overlaps %s", name, vol, hp, owner)
				}
			}
			hostPaths[hp] = key
		}
	}
	if bp.Backup.Repository == "" {
		add("backup.repository: required")
	} else if hasControl(bp.Backup.Repository) {
		add("backup.repository: must not contain control characters such as newlines")
	} else if err := checkRepoArgs(bp.Backup.Repository); err != nil {
		add("backup.repository: %v", err)
	}
	if !envNameRE.MatchString(bp.Backup.PasswordEnv) {
		add("backup.password_env: %q must be an environment variable name", bp.Backup.PasswordEnv)
	}
	if bp.Backup.KeepLast < 0 {
		add("backup.keep_last: must not be negative")
	}
	if bp.Backup.Repository != "" && !isRemoteRepo(bp.Backup.Repository) {
		repo := bp.RepoLocation()
		for p, owner := range hostPaths {
			if overlaps(p, repo) {
				add("backup.repository: %s overlaps data volume %s", repo, owner)
			}
		}
	}
	if bp.Drill.Schedule != "" {
		if err := validateCron(bp.Drill.Schedule); err != nil {
			add("drill.schedule: %v", err)
		}
	}
	return errors.Join(errs...)
}

// AppNames returns app names sorted.
func (bp *Blueprint) AppNames() []string { return sortedKeys(bp.Apps) }

// HostPath resolves p against DataRoot (then Dir) unless it is absolute.
func (bp *Blueprint) HostPath(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	root := bp.DataRoot
	if !filepath.IsAbs(root) {
		root = filepath.Join(bp.Dir, root)
	}
	return filepath.Join(root, p)
}

// RepoLocation returns the restic repository string with local paths made absolute.
func (bp *Blueprint) RepoLocation() string {
	if isRemoteRepo(bp.Backup.Repository) {
		return bp.Backup.Repository
	}
	if filepath.IsAbs(bp.Backup.Repository) {
		return filepath.Clean(bp.Backup.Repository)
	}
	return filepath.Join(bp.Dir, bp.Backup.Repository)
}

// RedactedRepo returns RepoLocation with any password in URL userinfo replaced by "***",
// for display in logs and dry-run output (e.g. rest:https://user:secret@host/ ).
func (bp *Blueprint) RedactedRepo() string { return RedactRepo(bp.RepoLocation()) }

// RedactRepo hides the password part of URL userinfo in a restic repository string.
func RedactRepo(repo string) string {
	i := strings.Index(repo, "://")
	if i < 0 {
		return repo
	}
	start := i + 3
	end := len(repo)
	if j := strings.IndexAny(repo[start:], "/?#"); j >= 0 {
		end = start + j
	}
	auth := repo[start:end]
	at := strings.LastIndex(auth, "@")
	if at < 0 {
		return repo
	}
	user, _, hasPass := strings.Cut(auth[:at], ":")
	if !hasPass {
		return repo
	}
	return repo[:start] + user + ":***" + auth[at:] + repo[end:]
}

// VolumePaths returns every declared host path, sorted.
func (bp *Blueprint) VolumePaths() []string {
	var out []string
	for _, name := range bp.AppNames() {
		for _, v := range sortedKeys(bp.Apps[name].Volumes) {
			out = append(out, bp.HostPath(bp.Apps[name].Volumes[v]))
		}
	}
	sort.Strings(out)
	return out
}

func isRemoteRepo(r string) bool {
	i := strings.Index(r, ":")
	return i > 1 && !strings.HasPrefix(r, "/")
}

func overlaps(a, b string) bool {
	return a == b || within(a, b) || within(b, a)
}

// within reports whether child is strictly inside parent.
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			return true
		}
	}
	return !utf8.ValidString(s)
}

// checkPathText rejects characters that would change the meaning of a path once it is
// written into compose short syntax ("src:dst[:mode]"), restic.env or backup-paths.txt.
func checkPathText(p string) error {
	if hasControl(p) {
		return errors.New("must not contain control characters such as newlines")
	}
	rest := p[len(filepath.VolumeName(p)):]
	if strings.ContainsAny(rest, ":,") {
		return errors.New("must not contain ':' or ','; they are separators in compose volume syntax")
	}
	if strings.HasPrefix(p, "-") {
		return errors.New("must not start with '-'")
	}
	return nil
}

// checkRepoArgs rejects sftp and rclone repositories whose host, user or remote would reach
// ssh or rclone as a command-line option. restic passes the sftp host as ssh's first
// argument, so "sftp:-oProxyCommand=..." runs an arbitrary command on backup.
func checkRepoArgs(repo string) error {
	if strings.HasPrefix(repo, "-") {
		return errors.New("must not start with '-'")
	}
	var rest string
	switch {
	case strings.HasPrefix(repo, "sftp://"):
		rest = strings.TrimPrefix(repo, "sftp://")
	case strings.HasPrefix(repo, "sftp:"):
		rest = strings.TrimPrefix(repo, "sftp:")
	case strings.HasPrefix(repo, "rclone:"):
		rest = strings.TrimPrefix(repo, "rclone:")
	default:
		return nil
	}
	if strings.ContainsAny(rest, " \t") {
		return errors.New("sftp/rclone repository must not contain whitespace")
	}
	host := rest
	if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	user, h, hasUser := strings.Cut(host, "@")
	if strings.HasPrefix(rest, "-") || strings.HasPrefix(user, "-") || (hasUser && strings.HasPrefix(h, "-")) {
		return errors.New("sftp/rclone host, user or remote must not start with '-'; it would be passed to ssh or rclone as an option")
	}
	return nil
}

// sensitiveHostPaths are host locations an app must never be given: mounting them hands a
// web app root-equivalent control of the host (docker socket) or exposes system secrets.
var sensitiveHostPaths = []string{
	"/var/run/docker.sock", "/run/docker.sock", "/run/podman", "/var/run/podman",
	"/run/containerd", "/var/run/containerd",
	"/etc", "/root", "/boot", "/proc", "/sys", "/dev", "/usr", "/bin", "/sbin", "/lib", "/lib64",
	"/var/lib/docker", "/var/lib/containers",
}

// checkHostPath rejects host paths that would expose the host to a container.
func checkHostPath(hp string) error {
	if filepath.Dir(hp) == hp {
		return fmt.Errorf("%s is a filesystem root; mounting it would expose the whole host", hp)
	}
	slash := filepath.ToSlash(hp[len(filepath.VolumeName(hp)):])
	for _, s := range sensitiveHostPaths {
		if slash == s || strings.HasPrefix(slash, s+"/") {
			return fmt.Errorf("%s is inside sensitive host path %s; mounting it would expose the host", hp, s)
		}
	}
	if strings.HasSuffix(slash, ".sock") {
		return fmt.Errorf("%s looks like a socket; sapper only mounts data directories", hp)
	}
	return nil
}

func validateCron(s string) error {
	f := strings.Fields(s)
	if len(f) != 5 {
		return fmt.Errorf("%q must have 5 fields (minute hour day month weekday)", s)
	}
	for _, x := range f {
		if !cronField.MatchString(x) {
			return fmt.Errorf("%q: field %q has invalid characters", s, x)
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
