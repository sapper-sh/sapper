// Package cli implements the sapper command line.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sapper-unbuilt/sapper/internal/art"
	"github.com/sapper-unbuilt/sapper/internal/blueprint"
	"github.com/sapper-unbuilt/sapper/internal/catalog"
	"github.com/sapper-unbuilt/sapper/internal/compose"
	"github.com/sapper-unbuilt/sapper/internal/ops"
	"github.com/sapper-unbuilt/sapper/internal/restic"
	"github.com/sapper-unbuilt/sapper/internal/runtime"
	"github.com/sapper-unbuilt/sapper/internal/termx"
)

// Version is set at build time with -ldflags "-X github.com/sapper-unbuilt/sapper/internal/cli.Version=...".
var Version = "dev"

type app struct {
	stdin          io.Reader
	stdout, stderr io.Writer

	blueprintPath string
	jsonOut       bool
	quiet         bool
	verbose       bool
	noColor       bool
	plain         bool

	style termx.Style
}

// Main runs sapper with args and returns the process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "sapper: "+err.Error())
	}
	return codeOf(err)
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "sapper",
		Short: "Self-hosted fortress builder with verified backups and breach drills",
		Long: `sapper turns a Linux host into a self-hosted fortress described by a blueprint,
backs its data up with restic, and runs breach drills that restore the latest
backup into a throwaway directory and verify every file against checksums
captured at backup time. Drills never write to live data.`,
		Example: `  sapper init
  sapper build --dry-run
  export SAPPER_RESTIC_PASSWORD=...   # or --password-file
  sapper backup
  sapper breach
  sapper status`,
		SilenceUsage:      true,
		SilenceErrors:     true,
		Version:           Version,
		PersistentPreRunE: a.setup,
	}
	root.SuggestionsMinimumDistance = 2
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usagef("%v", err) })
	pf := root.PersistentFlags()
	pf.StringVarP(&a.blueprintPath, "blueprint", "f", envOr("SAPPER_BLUEPRINT", blueprint.FileName), "blueprint file (env SAPPER_BLUEPRINT)")
	pf.BoolVar(&a.jsonOut, "json", false, "write machine-readable JSON to stdout")
	pf.BoolVarP(&a.quiet, "quiet", "q", false, "print only results and errors")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "log every external command to stderr")
	pf.BoolVar(&a.noColor, "no-color", false, "disable colour (NO_COLOR is also honoured)")
	pf.BoolVar(&a.plain, "plain", false, "no colour, no art, no animation")

	root.RunE = func(cmd *cobra.Command, _ []string) error {
		noAnim, _ := cmd.Flags().GetBool("no-anim")
		a.bannerSized(art.Full, noAnim)
		return cmd.Help()
	}
	root.Flags().Bool("no-anim", false, "skip the boot animation")
	root.AddCommand(a.initCmd(), a.validateCmd(), a.buildCmd(), a.backupCmd(), a.breachCmd(), a.statusCmd())
	return root
}

func (a *app) setup(*cobra.Command, []string) error {
	if f, ok := a.stdout.(*os.File); ok {
		a.style = termx.Detect(f, a.noColor || a.plain || a.jsonOut)
	}
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// logf writes progress to stderr unless --quiet or --json.
func (a *app) logf(format string, args ...any) {
	if a.quiet || a.jsonOut {
		return
	}
	fmt.Fprintf(a.stderr, format+"\n", args...)
}

func (a *app) commandLog() io.Writer {
	if a.verbose {
		return a.stderr
	}
	return nil
}

func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *app) load() (*blueprint.Blueprint, error) {
	bp, err := blueprint.Load(a.blueprintPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, usagef("%s not found; run `sapper init` or pass --blueprint", a.blueprintPath)
	}
	if err != nil {
		return nil, withCode(ExitUsage, err)
	}
	return bp, nil
}

func (a *app) initCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Write an example fortress.blueprint",
		Example: `  sapper init
  sapper init --blueprint /srv/fortress/fortress.blueprint
  sapper init --yes    # overwrite an existing blueprint`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := os.Stat(a.blueprintPath); err == nil && !yes {
				return usagef("%s already exists; pass --yes to overwrite it", a.blueprintPath)
			}
			if err := os.MkdirAll(filepath.Dir(a.blueprintPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(a.blueprintPath, []byte(blueprint.Example), 0o644); err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]string{"blueprint": a.blueprintPath})
			}
			fmt.Fprintln(a.stdout, a.blueprintPath)
			a.logf("wrote example blueprint; available apps: %s", strings.Join(catalog.Names(), ", "))
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "overwrite an existing blueprint")
	return c
}

func (a *app) validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "validate",
		Short:   "Check the blueprint and exit non-zero on problems",
		Example: "  sapper validate --blueprint fortress.blueprint",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			bp, err := a.load()
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{"valid": true, "fortress": bp.Fortress, "apps": bp.AppNames()})
			}
			fmt.Fprintf(a.stdout, "%s: valid (%d apps)\n", a.blueprintPath, len(bp.Apps))
			return nil
		},
	}
}

func (a *app) buildCmd() *cobra.Command {
	var dryRun, up bool
	var rt string
	c := &cobra.Command{
		Use:   "build",
		Short: "Render docker compose and restic config into .sapper/",
		Long: `build validates the blueprint and renders .sapper/docker-compose.yml, restic.env,
backup-paths.txt and drill.cron next to it. With --up it also runs
"<runtime> compose up -d" (Linux only).`,
		Example: `  sapper build --dry-run
  sapper build
  sapper build --up --runtime podman`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bp, err := a.load()
			if err != nil {
				return err
			}
			artifacts, err := compose.Render(bp)
			if err != nil {
				return err
			}
			out := filepath.Join(bp.Dir, ".sapper")
			if dryRun {
				if a.jsonOut {
					m := map[string]string{}
					for k, v := range artifacts {
						m[k] = string(v)
					}
					return a.printJSON(map[string]any{"dir": out, "files": m})
				}
				for _, name := range []string{"docker-compose.yml", "restic.env", "backup-paths.txt", "drill.cron"} {
					if data, ok := artifacts[name]; ok {
						fmt.Fprintf(a.stdout, "==> %s\n%s\n", filepath.Join(out, name), data)
					}
				}
				return nil
			}
			if up {
				if _, err := runtime.New(rt, nil); err != nil {
					return withCode(ExitUsage, err)
				}
			}
			if err := compose.Write(out, artifacts); err != nil {
				return err
			}
			if a.jsonOut && !up {
				return a.printJSON(map[string]string{"dir": out})
			}
			if !a.jsonOut {
				fmt.Fprintln(a.stdout, out)
			}
			if !up {
				return nil
			}
			r, _ := runtime.New(rt, a.commandLog())
			a.logf("starting %s with %s compose", bp.Fortress, rt)
			if err := r.Up(cmd.Context(), filepath.Join(out, "docker-compose.yml"), bp.Fortress); err != nil {
				if errors.Is(err, runtime.ErrNotLinux) {
					return withCode(ExitUsage, err)
				}
				if strings.Contains(err.Error(), "not found in PATH") {
					return withCode(ExitMissingDep, err)
				}
				return err
			}
			if a.jsonOut {
				return a.printJSON(map[string]any{"dir": out, "up": true})
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered files without writing them")
	c.Flags().BoolVar(&up, "up", false, "run compose up -d after rendering (Linux only)")
	c.Flags().StringVar(&rt, "runtime", "docker", "container runtime: docker or podman")
	_ = c.RegisterFlagCompletionFunc("runtime", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return runtime.Supported, cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

type passwordFlags struct {
	file  string
	stdin bool
}

func (p *passwordFlags) add(c *cobra.Command) {
	c.Flags().StringVar(&p.file, "password-file", "", "read the repository password from this file")
	c.Flags().BoolVar(&p.stdin, "password-stdin", false, "read the repository password from the first line of stdin")
	c.MarkFlagsMutuallyExclusive("password-file", "password-stdin")
}

// resolve returns the password from --password-file, --password-stdin or the blueprint's env var.
func (p *passwordFlags) resolve(a *app, bp *blueprint.Blueprint) (string, error) {
	switch {
	case p.file != "":
		if st, err := os.Stat(p.file); err == nil && goruntime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
			fmt.Fprintf(a.stderr, "sapper: warning: password file %s is readable by other users (mode %v); run chmod 600 on it\n", p.file, st.Mode().Perm())
		}
		data, err := os.ReadFile(p.file)
		if err != nil {
			return "", withCode(ExitUsage, fmt.Errorf("password file: %w", err))
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	case p.stdin:
		line, err := bufio.NewReader(a.stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if v := os.Getenv(bp.Backup.PasswordEnv); v != "" {
		return v, nil
	}
	return "", usagef("no repository password: set $%s, or use --password-file or --password-stdin", bp.Backup.PasswordEnv)
}

func (a *app) client(p *passwordFlags, bp *blueprint.Blueprint) (*restic.Client, error) {
	pw, err := p.resolve(a, bp)
	if err != nil {
		return nil, err
	}
	if pw == "" {
		return nil, usagef("repository password is empty")
	}
	c, err := restic.New(bp.RepoLocation(), pw)
	if errors.Is(err, restic.ErrNotInstalled) {
		return nil, withCode(ExitMissingDep, err)
	}
	if err != nil {
		return nil, err
	}
	c.Log = a.commandLog()
	c.DropEnv = []string{bp.Backup.PasswordEnv}
	return c, nil
}

func (a *app) backupCmd() *cobra.Command {
	var pw passwordFlags
	var dryRun bool
	c := &cobra.Command{
		Use:   "backup",
		Short: "Back up every declared volume with restic",
		Long: `backup snapshots every volume in the blueprint and records a checksum manifest
for the snapshot. Stop apps that write continuously first; backup fails if data
changes while it runs. The repository is initialised on first use.`,
		Example: `  SAPPER_RESTIC_PASSWORD=... sapper backup
  sapper backup --password-file /root/.config/sapper/restic-pass
  sapper backup --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bp, err := a.load()
			if err != nil {
				return err
			}
			if dryRun {
				if a.jsonOut {
					return a.printJSON(map[string]any{"repository": bp.RedactedRepo(), "paths": bp.VolumePaths()})
				}
				fmt.Fprintf(a.stdout, "repository: %s\n", bp.RedactedRepo())
				for _, p := range bp.VolumePaths() {
					fmt.Fprintln(a.stdout, "  "+p)
				}
				return nil
			}
			c, err := a.client(&pw, bp)
			if err != nil {
				return err
			}
			a.logf("backing up %d volumes to %s", len(bp.VolumePaths()), bp.RedactedRepo())
			rec, err := ops.Backup(cmd.Context(), bp, c)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.printJSON(rec)
			}
			fmt.Fprintf(a.stdout, "snapshot %s: %d entries\n", short(rec.Snapshot), rec.Entries)
			return nil
		},
	}
	pw.add(c)
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be backed up")
	return c
}

func (a *app) breachCmd() *cobra.Command {
	var pw passwordFlags
	var keep bool
	var tmp string
	c := &cobra.Command{
		Use:   "breach",
		Short: "Restore the latest backup into a throwaway directory and verify it",
		Long: `breach runs a drill: it restores the latest sapper snapshot into a new temporary
directory, compares every file with the manifest captured at backup time, deletes
the copy and prints SURVIVED or BREACHED. Live volumes are never written.

Exit codes: 0 survived, 3 breached, 4 no verdict (no snapshot, manifest or restore).`,
		Example: `  sapper breach
  sapper breach --json
  sapper breach --keep --temp-dir /var/tmp   # leave the restored copy for inspection`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bp, err := a.load()
			if err != nil {
				return err
			}
			c, err := a.client(&pw, bp)
			if err != nil {
				return err
			}
			a.logf("restoring latest snapshot into a throwaway directory")
			rep, err := ops.Breach(cmd.Context(), bp, c, ops.DrillOptions{Keep: keep, TempRoot: tmp})
			if err != nil && !errors.Is(err, ops.ErrBreached) {
				return withCode(ExitUnverified, fmt.Errorf("drill did not reach a verdict: %w", err))
			}
			if a.jsonOut {
				verdict := "SURVIVED"
				if !rep.Survived() {
					verdict = "BREACHED"
				}
				if e := a.printJSON(map[string]any{"verdict": verdict, "report": rep}); e != nil {
					return e
				}
			} else {
				fmt.Fprint(a.stdout, a.verdict(rep).Render(a.style.Color))
			}
			if !rep.Survived() {
				return withCode(ExitBreached, fmt.Errorf("%s in snapshot %s", plural(len(rep.Problems), "problem"), short(rep.Snapshot)))
			}
			return nil
		},
	}
	pw.add(c)
	c.Flags().BoolVar(&keep, "keep", false, "keep the restored copy and print its path")
	c.Flags().StringVar(&tmp, "temp-dir", "", "parent directory for the throwaway restore (default: system temp)")
	return c
}

func (a *app) verdict(rep ops.DrillReport) art.Verdict {
	lines := []string{
		fmt.Sprintf("snapshot   %s  (%s)", short(rep.Snapshot), rep.SnapshotAt),
		fmt.Sprintf("verified   %d entries, %s", rep.Checked, humanBytes(rep.Bytes)),
		fmt.Sprintf("duration   %s", rep.Duration.Round(10*time.Millisecond)),
	}
	if rep.RestoreDir != "" {
		lines = append(lines, "kept copy  "+rep.RestoreDir)
	}
	const maxShown = 10
	for i, p := range rep.Problems {
		if i == maxShown {
			lines = append(lines, fmt.Sprintf("... and %d more", len(rep.Problems)-maxShown))
			break
		}
		lines = append(lines, fmt.Sprintf("FAIL  %s: %s", p.Path, p.Reason))
	}
	return art.Verdict{Survived: rep.Survived(), Title: "BREACH DRILL // " + rep.Fortress, Lines: lines}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
