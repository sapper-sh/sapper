// Package cli implements the sapper command line.
package cli

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/fips140"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sapper-sh/sapper/internal/art"
	"github.com/sapper-sh/sapper/internal/blueprint"
	"github.com/sapper-sh/sapper/internal/catalog"
	"github.com/sapper-sh/sapper/internal/compose"
	"github.com/sapper-sh/sapper/internal/ops"
	"github.com/sapper-sh/sapper/internal/report"
	"github.com/sapper-sh/sapper/internal/restic"
	"github.com/sapper-sh/sapper/internal/runtime"
	"github.com/sapper-sh/sapper/internal/termx"
)

// Version is set at build time with -ldflags "-X github.com/sapper-sh/sapper/internal/cli.Version=...".
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

// versionString is the --version text. In FIPS 140-3 mode it names the Go
// Cryptographic Module in use; that says which module is linked, not that sapper
// is validated (see docs/FIPS.md).
func versionString() string {
	if fips140.Enabled() {
		return Version + " (FIPS 140-3 mode, Go Cryptographic Module " + fips140.Version() + "; sapper itself is not FIPS validated)"
	}
	return Version
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "sapper",
		Short: "Self-hosted apps with backups that are restored and checked, not assumed",
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
		Version:           versionString(),
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
	root.AddCommand(a.initCmd(), a.validateCmd(), a.buildCmd(), a.backupCmd(), a.breachCmd(), a.preflightCmd(), a.reportsCmd(), a.keygenCmd(), a.statusCmd(), a.taleCmd())
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
	var dryRun, force bool
	var signKey string
	c := &cobra.Command{
		Use:   "backup",
		Short: "Back up every declared volume with restic",
		Long: `backup snapshots every volume in the blueprint and records a checksum manifest
for the snapshot. Stop apps that write continuously first; backup fails if data
changes while it runs. The repository is initialised on first use.

A copy of the manifest is stored in the repository as its own snapshot (tag
sapper-manifest), so a drill from another machine can still verify. --sign-key
signs that copy.

Before backing up, sapper compares live data with the last backup and refuses
(exit 6) when a volume is empty, has lost more than half its files or bytes, or
when most files changed and the changed files look encrypted. Those are the
signs of a missing mount, an rm -rf or ransomware, and backing them up would push
good snapshots out of retention. --force backs up anyway. The newest snapshot
that passed a drill is never removed by keep_last.`,
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
			key, err := a.signingKey(signKey)
			if err != nil {
				return err
			}
			rec, err := ops.Backup(cmd.Context(), bp, c, ops.BackupOptions{Force: force, SignKey: key})
			for _, an := range rec.Forced {
				fmt.Fprintf(a.stderr, "sapper: warning: backed up despite: %s\n", an.Detail)
			}
			if errors.Is(err, ops.ErrSuspicious) {
				return withCode(ExitSuspicious, err)
			}
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
	c.Flags().BoolVar(&force, "force", false, "back up even if live data looks damaged, or start a new repository where one went missing")
	c.Flags().StringVar(&signKey, "sign-key", envOr("SAPPER_SIGN_KEY", ""), "sign the manifest copy stored in the repository with this ed25519 key (env SAPPER_SIGN_KEY)")
	return c
}

func (a *app) breachCmd() *cobra.Command {
	var pw passwordFlags
	var keep, brief, strict bool
	var tmp, signKey, snapshot, pubKey string
	var sample int
	c := &cobra.Command{
		Use:   "breach",
		Short: "Restore the latest backup into a throwaway directory and verify it",
		Long: `breach runs a drill: it restores the latest sapper snapshot into a new temporary
directory, compares every file with the manifest captured at backup time, deletes
the copy and prints an evidence report: the verdict and the reason first, then
what was tested, what was found, how long each step took, recovery time and backup
age against the blueprint's targets, and what to do next. Live volumes are never
written.

Every drill saves its report as JSON and Markdown in the state directory, chained
by hash to the one before. See "sapper reports".

If this machine has no manifest for the snapshot (a new host after a disaster),
the drill uses the copy stored in the repository at backup time. If both exist
and disagree, the drill reports a breach. --pub-key requires that copy to be
signed by the given key.

--snapshot drills a chosen snapshot; --sample N also restores and verifies N
randomly chosen older snapshots, so damage to data only older snapshots use is
found before you need them.

--strict also fails a drill that survived but missed the blueprint's RTO or RPO
target, so a slow restore or a stale backup is not reported as success.

Exit codes: 0 survived, 3 breached, 4 no verdict (no snapshot, manifest or restore),
7 survived but missed an RTO or RPO target (only with --strict).`,
		Example: `  sapper breach
  sapper breach --brief                      # one line, for cron mail
  sapper breach --json | jq .verdict
  sapper breach --sign-key ~/.config/sapper/drill.key
  sapper breach --keep --temp-dir /var/tmp   # leave the restored copy for inspection
  sapper breach --sample 3                   # also drill three older snapshots
  sapper breach --snapshot 1a2b3c4d
  sapper breach --strict                     # also fail when RTO or RPO is missed`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bp, err := a.load()
			if err != nil {
				return err
			}
			key, err := a.signingKey(signKey)
			if err != nil {
				return err
			}
			if sample < 0 || sample > 100 {
				return usagef("--sample must be between 0 and 100")
			}
			var pub ed25519.PublicKey
			if pubKey != "" {
				if pub, err = report.LoadPublicKey(pubKey); err != nil {
					return withCode(ExitUsage, fmt.Errorf("--pub-key: %w", err))
				}
			}
			c, err := a.client(&pw, bp)
			if err != nil {
				return err
			}
			a.logf("restoring latest snapshot into a throwaway directory")
			rep, err := ops.Breach(cmd.Context(), bp, c, ops.DrillOptions{Keep: keep, TempRoot: tmp, SignKey: key, Snapshot: snapshot, Sample: sample, ManifestKey: pub})
			if rep.ReportErr != nil {
				fmt.Fprintf(a.stderr, "sapper: warning: drill report not saved: %v\n", rep.ReportErr)
			} else if rep.ReportPath != "" {
				a.logf("report saved to %s", rep.ReportPath)
			}
			if r := rep.Report; r != nil {
				switch {
				case a.jsonOut:
					if e := a.printJSON(r); e != nil {
						return e
					}
				case brief:
					fmt.Fprintln(a.stdout, report.Brief(r))
				default:
					fmt.Fprint(a.stdout, report.Terminal(r, a.reportView(rep.ReportPath)))
				}
			}
			if err != nil && !errors.Is(err, ops.ErrBreached) {
				return withCode(ExitUnverified, fmt.Errorf("drill did not reach a verdict: %s", report.CleanLines(err.Error())))
			}
			if !rep.Survived() {
				return withCode(ExitBreached, fmt.Errorf("%s in snapshot %s", plural(len(rep.Problems), "problem"), short(rep.Snapshot)))
			}
			if strict && rep.Report != nil {
				if missed := rep.Report.TargetsMissed(); len(missed) > 0 {
					return withCode(ExitTargets, fmt.Errorf("drill survived but missed its %s target (--strict)", strings.Join(missed, " and ")))
				}
			}
			return nil
		},
	}
	pw.add(c)
	c.Flags().BoolVar(&keep, "keep", false, "keep the restored copy and print its path")
	c.Flags().StringVar(&tmp, "temp-dir", "", "parent directory for the throwaway restore (default: system temp)")
	c.Flags().BoolVar(&brief, "brief", false, "print a one-line summary instead of the full report")
	c.Flags().BoolVar(&strict, "strict", false, "exit 7 when the drill survives but misses its RTO or RPO target")
	c.Flags().StringVar(&signKey, "sign-key", envOr("SAPPER_SIGN_KEY", ""), "sign the report with this ed25519 private key file (env SAPPER_SIGN_KEY; see sapper keygen)")
	c.Flags().StringVar(&snapshot, "snapshot", "", "drill this snapshot (ID or unique prefix) instead of the latest")
	c.Flags().IntVar(&sample, "sample", 0, "also drill this many randomly chosen older snapshots")
	c.Flags().StringVar(&pubKey, "pub-key", envOr("SAPPER_PUB_KEY", ""), "require the manifest copy in the repository to be signed by this ed25519 public key")
	return c
}

// signingKey loads an ed25519 private key for --sign-key, or returns nil for an empty path.
func (a *app) signingKey(path string) (ed25519.PrivateKey, error) {
	if path == "" {
		return nil, nil
	}
	k, warn, err := report.LoadPrivateKey(path)
	if err != nil {
		return nil, withCode(ExitUsage, fmt.Errorf("--sign-key: %w", err))
	}
	if warn != "" {
		fmt.Fprintln(a.stderr, "sapper: warning: "+warn)
	}
	return k, nil
}

func (a *app) preflightCmd() *cobra.Command {
	var pw passwordFlags
	var tmp string
	c := &cobra.Command{
		Use:   "preflight",
		Short: "Check that backup and breach can run, and what could still lose the backups",
		Long: `preflight checks, without changing anything: every volume exists and is not
empty (an unmounted disk looks like an empty directory), the repository is
reachable and the password opens it, no locks are held, the drill temp directory
has room for a restore, and whether this host could delete the repository.

Exits 1 if a check that would make backup or breach fail does not pass. Risks
(locks held, a repository this host can delete) are reported but do not fail.`,
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
			res := ops.Preflight(cmd.Context(), bp, c, ops.PreflightOptions{TempRoot: tmp})
			if a.jsonOut {
				if err := a.printJSON(res); err != nil {
					return err
				}
			} else {
				for _, ch := range res.Checks {
					mark := "ok  "
					switch {
					case !ch.OK && ch.Fatal:
						mark = "FAIL"
					case !ch.OK:
						mark = "RISK"
					}
					fmt.Fprintf(a.stdout, "%s  %-12s %s\n", mark, ch.Name, report.CleanLines(ch.Detail))
				}
			}
			if !res.OK() {
				return errors.New("pre-flight failed")
			}
			return nil
		},
	}
	pw.add(c)
	c.Flags().StringVar(&tmp, "temp-dir", "", "directory drills will restore under (default: system temp)")
	return c
}

func (a *app) reportView(path string) report.View {
	return report.View{Color: a.style.Color, Width: a.style.Width, Path: path, Plain: a.plain}
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
