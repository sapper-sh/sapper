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

Requires Go 1.26+ to build, and [restic](https://restic.net) at run time.
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
| `breach`   | restore latest snapshot to a temp dir, verify it and print an evidence report (`--brief`, `--sign-key`, `--keep`, `--temp-dir`) |
| `reports`  | list saved drill reports; `reports show [n]` (`--markdown`, `--brief`); `reports verify` (`--pub-key`, `--head`) |
| `keygen`   | create an ed25519 key pair for signing reports (`--out`) |
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
| 3 | `breach` ran and found damaged, missing or unexpected files; `reports verify` found an edited, missing or reordered report |
| 4 | `breach` could not reach a verdict (no snapshot, no manifest, restore failed) |
| 5 | restic, docker or podman not installed |

## Drill reports

Every `sapper breach` ends with an evidence report. The verdict and the reason for
it come first, then the detail:

    ░▒▓▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▓▒░
                      BREACH DRILL // my-fortress

                       ▓▒░  S U R V I V E D  ░▒▓

      The latest backup restores correctly.
      All 11 entries in snapshot 523b76e9 came back identical to
      the checksums taken at backup time.
    ░▒▓▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▓▒░

    What was tested       snapshot (short and full ID, when it was taken), apps and volumes
    Evidence              entries checked and matched, bytes, each problem with expected
                          and actual values, Merkle root over the manifest
    Isolation             restore directory checked outside live data; live data
                          compared before and after the drill
    Timings               each phase
    Recovery              restore time (RTO) and backup age (RPO) against targets
    What to do next       a concrete step for each kind of problem
    Record                report number, hash, previous hash, signature

![SURVIVED](docs/img/breach-survived.png)
![BREACHED](docs/img/breach-breached.png)

`--brief` prints one line for cron mail; `--json` prints the structured report;
`--plain` drops the art frame. Output never exceeds 80 columns.

Reports are saved in the state directory as `reports/NNNNNN.json` and a Markdown
copy, `NNNNNN.md`, to paste into a ticket. They contain host paths of your data
and the repository location (passwords in URLs are replaced with `***`); read one
before sharing it.

Each report stores the sha256 of the one before it. `sapper reports verify`
recomputes the chain and reports any edited, removed or reordered report. Two
things the chain cannot do on its own: notice that the newest reports were
deleted, and stop someone who can write the state directory from rebuilding it.
For those, sign reports with a key kept away from the state directory and record
the latest hash elsewhere:

    sapper keygen                                   # ~/.config/sapper/drill.key and .pub
    sapper breach --sign-key ~/.config/sapper/drill.key
    sapper reports verify --pub-key drill.key.pub --head 4be0c1d2e3f4

### Recovery targets

Optional. Each drill measures restore-and-verify time (RTO) and the age of the
backup it tested (RPO), and compares them with:

    targets:
      rto: 30m    # Go duration, or whole days such as 7d; at most a year
      rpo: 26h

A missed target is reported in the headline and the next steps. It does not
change the exit code: the data still restored correctly.

## Secrets

The repository password is read from `--password-file`, `--password-stdin`, or
the environment variable named by `backup.password_env`. It is never accepted as
a flag value, never written by sapper, and never shown in `-v` command logs.

## Where things live

- `.sapper/` next to the blueprint: rendered compose file, `restic.env`, backup
  path list, drill cron line. Safe to delete; `sapper build` recreates it.
- Per-user state (manifests, last backup, last drill, drill reports):
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
