> DRAFT. Placeholder text to be rewritten by the author.

# sapper

sapper turns a Linux host into a small self-hosted fortress described by one
blueprint file, backs the data up with restic, and runs breach drills: it
restores the latest backup into a throwaway directory, checks every file against
checksums recorded at backup time, and prints SURVIVED or BREACHED.

Drills only ever write to a fresh temporary directory. sapper refuses to run a
drill whose restore directory overlaps a data volume, the repository or its own
state.

## Install

Requires Go 1.23+ to build, and [restic](https://restic.net) at run time.
docker or podman with the compose plugin is needed only for `sapper build --up`.

    go build -o sapper ./cmd/sapper

## Quick start

    sapper init                         # writes fortress.blueprint
    sapper validate
    sapper build --dry-run              # prints .sapper/docker-compose.yml and friends
    sapper build --up                   # Linux only: docker compose up -d
    export SAPPER_RESTIC_PASSWORD=...   # or --password-file / --password-stdin
    sapper backup
    sapper breach
    sapper status

Apps in the built-in catalogue: `vaultwarden`, `filebrowser`, `memos`.

## Commands

| command    | does |
|------------|------|
| `init`     | write an example blueprint (`--yes` to overwrite) |
| `validate` | check the blueprint |
| `build`    | render `.sapper/` (`--dry-run`, `--up`, `--runtime docker\|podman`) |
| `backup`   | restic backup of every volume plus a checksum manifest (`--dry-run`) |
| `breach`   | restore latest snapshot to a temp dir and verify (`--keep`, `--temp-dir`) |
| `status`   | boot art, fortress, last backup, last drill |
| `completion` | shell completion for bash, zsh, fish, powershell |

Global flags: `-f/--blueprint`, `--json`, `-q/--quiet`, `-v/--verbose` (log
every restic/docker/podman command to stderr), `--no-color`, `--plain`.
Results go to stdout, progress and logs to stderr.

## Exit codes

| code | meaning |
|------|---------|
| 0 | success; for `breach`, the drill survived |
| 1 | operation failed |
| 2 | usage error: bad flags, arguments or blueprint, missing password |
| 3 | `breach` ran and found damaged, missing or unexpected files |
| 4 | `breach` could not reach a verdict (no snapshot, no manifest, restore failed) |
| 5 | restic, docker or podman not installed |

## Secrets

The repository password is read from `--password-file`, `--password-stdin`, or
the environment variable named by `backup.password_env`. It is never accepted as
a flag value, never written by sapper, and never shown in `-v` command logs.

## Where things live

- `.sapper/` next to the blueprint: rendered compose file, `restic.env`, backup
  path list, drill cron line. Safe to delete; `sapper build` recreates it.
- Per-user state (manifests, last backup, last drill):
  - Linux: `$XDG_STATE_HOME/sapper`, default `~/.local/state/sapper`
  - macOS: `~/Library/Application Support/sapper`
  - Windows: `%LOCALAPPDATA%\sapper`
  - override with `SAPPER_STATE_DIR`

## Platforms

| | Linux | macOS | Windows |
|---|---|---|---|
| init, validate, build (render), backup, breach, status | yes | yes | yes |
| build --up (host the fortress) | yes | no, says to run on Linux | no, says to run on Linux |

Colour follows [NO_COLOR](https://no-color.org), `--no-color` and `--plain`,
and is off when stdout is not a terminal. On Windows, ANSI output is enabled
through the console's virtual terminal mode and falls back to plain text when
that is unavailable. The boot animation runs only on an interactive colour
terminal; press Enter, pass `--no-anim` or set `SAPPER_NO_ANIM=1` to skip it.

## Telemetry

None. sapper makes no network connections of its own; restic and the container
runtime connect only where your blueprint points them.

## Limitations

See [DESIGN.md](DESIGN.md#limitations).

## Licence

Apache-2.0. See [LICENSE](LICENSE).
