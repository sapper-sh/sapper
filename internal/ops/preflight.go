package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sapper-unbuilt/sapper/internal/blueprint"
	"github.com/sapper-unbuilt/sapper/internal/manifest"
	"github.com/sapper-unbuilt/sapper/internal/report"
)

// PreflightOptions tune Preflight.
type PreflightOptions struct {
	// TempRoot is where drills restore; empty means os.TempDir().
	TempRoot string
	// FreeBytes replaces the free-space probe in tests.
	FreeBytes func(dir string) (uint64, error)
	// Writable replaces the "can this user delete files here" probe in tests.
	Writable func(dir string) bool
}

// Check is one pre-flight result. A failed Fatal check means backup or breach would fail;
// a failed non-fatal check is a risk worth fixing.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Fatal  bool   `json:"fatal"`
	Detail string `json:"detail"`
}

// PreflightResult lists every check in a fixed order.
type PreflightResult struct {
	Checks []Check `json:"checks"`
}

// OK reports whether every fatal check passed.
func (p PreflightResult) OK() bool {
	for _, c := range p.Checks {
		if c.Fatal && !c.OK {
			return false
		}
	}
	return true
}

// Preflight checks what a backup or drill needs before running one: volumes present and
// not empty, the repository reachable and the password right, no locks, enough space for
// a drill restore, and whether this host could delete the backups.
func Preflight(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt PreflightOptions) PreflightResult {
	var res PreflightResult
	add := func(name string, ok, fatal bool, format string, a ...any) {
		res.Checks = append(res.Checks, Check{name, ok, fatal, fmt.Sprintf(format, a...)})
	}
	for _, p := range bp.VolumePaths() {
		entries, err := os.ReadDir(p)
		switch {
		case err != nil:
			add("volumes", false, true, "%s: %v", p, err)
		case len(entries) == 0:
			add("volumes", false, true, "%s is empty; if it is a mount point, mount the disk before backing up", p)
		}
	}
	if len(res.Checks) == 0 {
		add("volumes", true, true, "%d volumes present and not empty", len(bp.VolumePaths()))
	}

	_, hadBackup := LastBackup(bp)
	reachable := false
	if rc, ok := r.(repoChecker); ok {
		exists, err := rc.RepoExists(ctx)
		switch {
		case errors.Is(err, ErrWrongPassword):
			add("repository", true, true, "%s is reachable", bp.RedactedRepo())
			add("password", false, true, "%s", err.Error())
		case err != nil:
			add("repository", false, true, "%s: %s", bp.RedactedRepo(), firstLine(report.CleanLines(err.Error())))
		case !exists && hadBackup:
			add("repository", false, true, "%s has no repository, but this host backed up there before. It may have been deleted", bp.RedactedRepo())
		case !exists:
			add("repository", true, true, "%s has no repository yet; the first backup creates it", bp.RedactedRepo())
		default:
			reachable = true
			add("repository", true, true, "%s is reachable", bp.RedactedRepo())
			add("password", true, true, "the password opens the repository")
		}
	}
	if ll, ok := r.(lockLister); ok && reachable {
		n, err := ll.Locks(ctx)
		switch {
		case err != nil:
			add("locks", false, false, "could not list locks: %s", firstLine(err.Error()))
		case n > 0:
			add("locks", false, false, "%d lock(s) held. If no restic process is running, they are stale: inspect with `restic -r <repository> list locks --no-lock`, clear with `restic -r <repository> unlock`", n)
		default:
			add("locks", true, false, "no locks held")
		}
	}

	free := opt.FreeBytes
	if free == nil {
		free = freeBytes
	}
	tmp := opt.TempRoot
	if tmp == "" {
		tmp = os.TempDir()
	}
	var need int64
	if last, ok := LastBackup(bp); ok && validID(last.Snapshot) {
		if mp, err := statePath(bp, "manifests", last.Snapshot+".json"); err == nil {
			if m, err := manifest.Load(mp); err == nil {
				for _, e := range m.Entries {
					need += e.Size
				}
			}
		}
	}
	if have, err := free(tmp); err == nil {
		if need > 0 && have < uint64(need+need/10) {
			add("space", false, true, "a drill restore needs about %s in %s, %s is free. Pass --temp-dir on a larger disk", report.Bytes(need), tmp, report.Bytes(int64(min(have, 1<<62))))
		} else {
			add("space", true, true, "%s free in %s", report.Bytes(int64(min(have, 1<<62))), tmp)
		}
	}

	res.Checks = append(res.Checks, appendOnly(bp, opt.Writable))
	return res
}

// appendOnly says whether ransomware running as this user could delete the backups.
func appendOnly(bp *blueprint.Blueprint, writable func(string) bool) Check {
	c := func(ok bool, format string, a ...any) Check {
		return Check{Name: "append-only", OK: ok, Detail: fmt.Sprintf(format, a...)}
	}
	loc := bp.RepoLocation()
	const advice = "Keep one copy this host cannot delete: rest-server --append-only, S3/B2 object lock, or a pull from another machine"
	switch {
	case strings.HasPrefix(loc, "rest:"):
		return c(true, "%s cannot be checked from here. Make sure rest-server runs with --append-only and that pruning happens on the server", "REST server")
	case strings.HasPrefix(loc, "sftp:"):
		return c(false, "this host's ssh key can delete files on the sftp server. %s", advice)
	case strings.Contains(loc, ":") && !filepath.IsAbs(loc):
		return c(true, "%s cannot be checked from here. Turn on object lock or bucket versioning so deleted snapshots can be recovered", "cloud storage")
	}
	if writable == nil {
		writable = canWrite
	}
	if writable(filepath.Join(loc, "snapshots")) || writable(loc) {
		return c(false, "this user can delete %s, so anything that runs as this user (ransomware included) can delete every backup. %s", loc, advice)
	}
	return c(true, "this user cannot write %s", loc)
}
