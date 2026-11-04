# Design

## Goals

1. One file describes the fortress; everything else is derived.
2. A backup is only trusted after it has been restored and verified.
3. Drills cannot damage live data, by construction and by explicit checks.
4. Output is scriptable: stable exit codes, JSON, results on stdout.

## Structure

| package | responsibility |
|---|---|
| `internal/blueprint` | strict YAML parsing (unknown keys rejected) and validation that reports every problem at once |
| `internal/catalog` | built-in app definitions with pinned image tags |
| `internal/compose` | blueprint to compose file, `restic.env`, path list, cron line; atomic 0600 writes |
| `internal/manifest` | checksum manifest of volume trees; verification of a restored tree |
| `internal/restic` | thin wrapper over the restic binary; optional command log |
| `internal/report` | evidence report: building, wording, terminal/Markdown/one-line rendering, hash chain, ed25519 signing |
| `internal/ops` | backup and breach orchestration, isolation checks, state records |
| `internal/runtime` | docker/podman compose invocation and error hints |
| `internal/paths` | per-user state directory per platform |
| `internal/termx` | TTY, width, colour policy, Windows VT mode |
| `internal/art` | boot art variants, ice palette, verdict frame, reveal animation |
| `internal/cli` | cobra commands, flags, exit codes |

## Key decisions

**Shell out to restic instead of linking a backup library.** restic is audited,
widely deployed and has a stable JSON interface. Reimplementing encryption,
deduplication and repository formats would be the largest source of bugs.
Rejected: kopia (fine tool, smaller install base on distros), borg (no native
Windows support, Python dependency), a custom tar+age format.

**Own checksum manifest instead of `restic check --read-data`.** `restic check`
proves the repository is internally consistent. It does not prove that what
comes back matches what was on disk. The manifest is built before the backup
and again after; if they differ the backup is rejected because the data changed
under it. A drill restores and compares sha256, size, type, symlink target and
permission bits (permissions skipped on Windows). Files present in the restore
but not in the manifest are failures too.

**Drills bypass the restic cache.** restic caches tree blobs locally. With the
cache, a drill still passed after the repository's tree pack was damaged or
deleted (docs/SIMULATIONS.md, S01). Restores and manifest reads use `--no-cache`.

**Refuse suspicious backups instead of warning about them.** Ransomware, an
unmounted disk and `rm -rf` all show up as data that changed sharply since the
last backup. Backing it up is what pushes good snapshots out of retention, so
backup stops (exit 6) and `--force` is required. A warning in a cron mail nobody
reads would not help.

**Restore into `os.MkdirTemp`, then verify isolation.** The target is resolved
through symlinks and rejected if it overlaps any volume, the repository, the
state directory or `.sapper/`. The copy is deleted afterwards unless `--keep`.

**Manifests keyed by snapshot ID in per-user state.** Linux uses XDG state,
macOS Application Support, Windows `%LOCALAPPDATA%`. The directory name adds a
hash of the blueprint path so two fortresses with the same name do not collide.
Rejected: `~/.sapper` (ignores platform conventions); storing state only beside
the blueprint (often inside a git checkout, easy to commit by accident).

**Compose rather than direct container API calls.** Users can read, diff and run
the generated file without sapper. Ports bind to 127.0.0.1 so nothing is
exposed until the user adds a reverse proxy.
Rejected: talking to the Docker API (ties sapper to one engine), Kubernetes
(far beyond a single host), Nix (high barrier for the target audience).

**Cobra for the CLI.** Gives help, examples, suggestions for mistyped commands
and completions for bash, zsh, fish and PowerShell with little code.
Rejected: the standard `flag` package (no subcommand help or completions),
urfave/cli (comparable, smaller ecosystem).

**Hosting is Linux only; data commands are portable.** The fortress runs on a
Linux server, but people inspect, back up and drill from laptops. `build --up`
refuses on macOS and Windows with a clear message.

**Evidence reports instead of a bare verdict.** A verdict on its own asks to be
trusted. The report shows its working, ordered like a good postmortem or audit
finding: bottom line first, then scope, evidence, timeline and actions. Every
problem names the file, what was expected and what was found, and is paired with
a command or a decision. The copy is plain on purpose: short declarative
sentences, real numbers, no exclamation marks, no reassurance. `TestCopyStyle`
keeps it that way.

**Hash chain and optional signatures, not a transparency log.** Each report
records the previous report's sha256, and the Merkle root of the manifest it
checked. This makes quiet edits visible at almost no cost. It is not proof against
someone who controls the state directory and can rebuild the chain; signing with a
key held elsewhere and recording the head hash outside the host close most of that
gap. Rejected: an external timestamping service (network dependency, against the
no-telemetry rule), git as the store (heavy, and history can be rewritten too).

**Measured RTO is restore plus verify.** A restore is not usable until it is
known to be correct. RPO is the age of the drilled snapshot when the drill started,
which is the data a restore at that moment would have lost.

**Live data check by stat, not by hash.** Before restoring and after verifying,
sapper compares path, size, mode and mtime of every live entry. Hashing again would
double the cost of a drill. A running app can change data during a drill; the report
says so and does not treat it as a breach.

## Limitations

- The manifest copy in the repository is protected by restic's encryption, so
  anyone with the password can add one. Unsigned, sapper refuses two copies that
  disagree but cannot tell which is genuine. Sign with `backup --sign-key` and
  drill with `--pub-key` to settle it.
- Suspicious-change checks are thresholds: an empty volume, less than half the
  files or bytes, or at least half the files changed with mostly high-entropy
  contents. Ransomware that encrypts less than that per backup gets through
  (scenario S26 in docs/SIMULATIONS.md). Files that are already compressed or
  encrypted, such as photos, archives or vault blobs, look random when they change.
- Backups of running apps can fail with "volumes changed during backup". There
  is no automatic stop/start or database dump hook yet.
- The catalogue has three apps and no way to add custom ones.
- `drill.cron` is a line to install by hand; no systemd timer generation
  (`sapper schedule`), no Windows Task Scheduler support.
- Older snapshots are drilled only with `--sample` or `--snapshot`, and only
  those with a manifest (local or in the repository).
- The append-only check is a warning, and only meaningful for local and sftp
  repositories; sapper cannot see a REST server's or bucket's settings.
- `build --up` was not exercised against a real docker daemon in development.
- Large volumes are hashed twice per backup and once per drill; no parallelism.
- No packaging, signed releases or SBOM yet.
- Report chains are per host. Two machines drilling the same repository keep
  separate chains.
- A missed RTO or RPO target does not change the exit code.
- The report store has no retention; a report is a few kilobytes unless a drill
  finds many problems (findings are capped at 1,000 per report).
- The boot animation is skipped by Enter only after the line is submitted
  (stdin stays in cooked mode).
