// Package ops implements backup and breach drills for a fortress.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sapper-unbuilt/sapper/internal/blueprint"
	"github.com/sapper-unbuilt/sapper/internal/manifest"
	"github.com/sapper-unbuilt/sapper/internal/paths"
	"github.com/sapper-unbuilt/sapper/internal/restic"
)

// Tag marks snapshots created by sapper.
const Tag = "sapper"

// Restic is the subset of the restic client ops needs.
type Restic interface {
	EnsureRepo(ctx context.Context) (bool, error)
	Backup(ctx context.Context, paths, tags []string) (string, error)
	Latest(ctx context.Context, tag string) (restic.Snapshot, error)
	Restore(ctx context.Context, id, target string) error
	Forget(ctx context.Context, tag string, keepLast int) error
}

// StateDir returns the per-user state directory for bp.
func StateDir(bp *blueprint.Blueprint) (string, error) {
	return paths.FortressState(bp.Fortress, bp.Dir)
}

func statePath(bp *blueprint.Blueprint, elem ...string) (string, error) {
	d, err := StateDir(bp)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{d}, elem...)...), nil
}

// BackupRecord is stored after a successful backup.
type BackupRecord struct {
	Snapshot string    `json:"snapshot"`
	Time     time.Time `json:"time"`
	Entries  int       `json:"entries"`
	Created  bool      `json:"repo_created"`
}

// Backup snapshots every volume and stores a checksum manifest keyed by snapshot ID.
// It fails if the volumes change while the backup runs, because the manifest would not match.
func Backup(ctx context.Context, bp *blueprint.Blueprint, r Restic) (BackupRecord, error) {
	paths := bp.VolumePaths()
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return BackupRecord{}, fmt.Errorf("volume %s: %w", p, err)
		}
		if !st.IsDir() {
			return BackupRecord{}, fmt.Errorf("volume %s: not a directory", p)
		}
	}
	before, err := manifest.Build(paths)
	if err != nil {
		return BackupRecord{}, err
	}
	created, err := r.EnsureRepo(ctx)
	if err != nil {
		return BackupRecord{}, err
	}
	id, err := r.Backup(ctx, paths, []string{Tag, "fortress:" + bp.Fortress})
	if err != nil {
		return BackupRecord{}, err
	}
	after, err := manifest.Build(paths)
	if err != nil {
		return BackupRecord{}, err
	}
	if !manifest.Equal(before, after) {
		return BackupRecord{}, fmt.Errorf("volumes changed during backup of snapshot %s; stop the apps (docker compose stop) and run backup again", short(id))
	}
	mp, err := statePath(bp, "manifests", id+".json")
	if err != nil {
		return BackupRecord{}, err
	}
	if err := manifest.Save(mp, before); err != nil {
		return BackupRecord{}, err
	}
	if err := r.Forget(ctx, Tag, bp.Backup.KeepLast); err != nil {
		return BackupRecord{}, fmt.Errorf("snapshot %s saved, retention failed: %w", short(id), err)
	}
	rec := BackupRecord{Snapshot: id, Time: time.Now().UTC(), Entries: len(before.Entries), Created: created}
	return rec, writeState(bp, "last-backup.json", rec)
}

// DrillOptions tune a breach drill.
type DrillOptions struct {
	// TempRoot is where the throwaway restore directory is created; empty means os.TempDir().
	TempRoot string
	// Keep leaves the restored copy on disk for inspection.
	Keep bool
	// BeforeVerify runs after restore and before verification. Tests use it to inject damage.
	BeforeVerify func(restoreDir string) error
}

// DrillReport is the outcome of a breach drill.
type DrillReport struct {
	Fortress   string             `json:"fortress"`
	Snapshot   string             `json:"snapshot"`
	SnapshotAt string             `json:"snapshot_time"`
	Time       time.Time          `json:"time"`
	Duration   time.Duration      `json:"duration_ns"`
	Checked    int                `json:"checked"`
	Bytes      int64              `json:"bytes"`
	Problems   []manifest.Problem `json:"problems"`
	RestoreDir string             `json:"restore_dir,omitempty"`
}

// Survived reports whether the restore matched the manifest exactly.
func (d DrillReport) Survived() bool { return len(d.Problems) == 0 }

// ErrBreached is returned when a drill finds problems.
var ErrBreached = errors.New("breach drill failed")

// Breach restores the latest snapshot into a fresh temporary directory and verifies it.
// Live volumes are never written: the restore target is checked to lie outside every volume and the repository.
func Breach(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt DrillOptions) (DrillReport, error) {
	start := time.Now()
	rep := DrillReport{Fortress: bp.Fortress, Time: start.UTC()}
	snap, err := r.Latest(ctx, Tag)
	if err != nil {
		return rep, err
	}
	rep.Snapshot, rep.SnapshotAt = snap.ID, snap.Time
	mp, err := statePath(bp, "manifests", snap.ID+".json")
	if err != nil {
		return rep, err
	}
	m, err := manifest.Load(mp)
	if err != nil {
		return rep, fmt.Errorf("manifest for snapshot %s: %w", short(snap.ID), err)
	}
	dir, err := os.MkdirTemp(opt.TempRoot, "sapper-drill-*")
	if err != nil {
		return rep, err
	}
	if !opt.Keep {
		defer os.RemoveAll(dir)
	} else {
		rep.RestoreDir = dir
	}
	if err := checkIsolated(dir, bp); err != nil {
		return rep, err
	}
	if err := r.Restore(ctx, snap.ID, dir); err != nil {
		return rep, err
	}
	if opt.BeforeVerify != nil {
		if err := opt.BeforeVerify(dir); err != nil {
			return rep, err
		}
	}
	res, err := manifest.Verify(m, dir)
	if err != nil {
		return rep, err
	}
	rep.Checked, rep.Bytes, rep.Problems = res.Checked, res.Bytes, res.Problems
	rep.Duration = time.Since(start)
	if err := writeState(bp, "last-drill.json", rep); err != nil {
		return rep, err
	}
	if !rep.Survived() {
		return rep, ErrBreached
	}
	return rep, nil
}

// checkIsolated refuses restore targets that overlap live data or the repository.
func checkIsolated(dir string, bp *blueprint.Blueprint) error {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	sd, err := StateDir(bp)
	if err != nil {
		return err
	}
	guarded := append(bp.VolumePaths(), sd, filepath.Join(bp.Dir, ".sapper"))
	if loc := bp.RepoLocation(); filepath.IsAbs(loc) {
		guarded = append(guarded, loc)
	}
	for _, g := range guarded {
		if real, err := filepath.EvalSymlinks(g); err == nil {
			g = real
		}
		if overlaps(dir, g) {
			return fmt.Errorf("refusing drill: restore directory %s overlaps %s", dir, g)
		}
	}
	return nil
}

func overlaps(a, b string) bool {
	return a == b || inside(a, b) || inside(b, a)
}

func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// LastBackup reads the last backup record, if any.
func LastBackup(bp *blueprint.Blueprint) (BackupRecord, bool) {
	var r BackupRecord
	return r, readState(bp, "last-backup.json", &r) == nil
}

// LastDrill reads the last drill report, if any.
func LastDrill(bp *blueprint.Blueprint) (DrillReport, bool) {
	var r DrillReport
	return r, readState(bp, "last-drill.json", &r) == nil
}

func writeState(bp *blueprint.Blueprint, name string, v any) error {
	p, err := statePath(bp, name)
	if err != nil {
		return err
	}
	return writeJSON(p, v)
}

func readState(bp *blueprint.Blueprint, name string, v any) error {
	p, err := statePath(bp, name)
	if err != nil {
		return err
	}
	return readJSON(p, v)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
