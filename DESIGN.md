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

## Limitations

- Manifests live only in local state. If the host is lost, a drill from a new
  machine has no manifest to compare against. Planned: store the manifest in
  the repository as its own tagged snapshot.
- Backups of running apps can fail with "volumes changed during backup". There
  is no automatic stop/start or database dump hook yet.
- The catalogue has three apps and no way to add custom ones.
- `drill.cron` is a line to install by hand; no systemd timer generation
  (`sapper schedule`), no Windows Task Scheduler support.
- Only the latest snapshot is drilled.
- `build --up` was not exercised against a real docker daemon in development.
- Large volumes are hashed twice per backup and once per drill; no parallelism.
- No packaging, signed releases or SBOM yet.
- The boot animation is skipped by Enter only after the line is submitted
  (stdin stays in cooked mode).
