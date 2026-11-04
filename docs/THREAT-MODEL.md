# sapper threat model

## What sapper does

`sapper` reads `fortress.blueprint` (YAML), renders `.sapper/docker-compose.yml`,
`restic.env`, `backup-paths.txt` and `drill.cron`, optionally runs
`docker|podman compose up -d`, runs `restic backup` with a per-snapshot checksum
manifest stored in the per-user state directory, and runs breach drills that
`restic restore` the latest snapshot into a fresh temporary directory and verify it.

## Assets

1. The restic repository password and any credentials in the repository URL.
2. Live app data (vaultwarden vault, filebrowser files, memos notes).
3. Backup integrity and the honesty of the drill verdict.
4. The host: sapper usually runs as an admin user who can reach the docker socket.

## Trust boundaries and attackers

| Input | Who may control it | Treatment |
|---|---|---|
| `fortress.blueprint` | the operator; possibly someone who shared a blueprint, or a writer to the directory | **Untrusted text.** Strict YAML (unknown keys rejected, 1 MiB cap). Every value that reaches a generated file or a child process is validated: no control characters, no `:`/`,` in host paths, no leading `-`, no sftp/rclone option injection, no sensitive host paths or sockets. |
| Restic repository contents / remote | storage provider, anyone who compromises it | Untrusted. Snapshot IDs must be hex before becoming file names. Restores only go to a fresh `0700` temp dir checked not to overlap live data, state or repo. |
| Restored files (snapshot data) | anyone who could write into a backed-up volume (e.g. filebrowser users) | Untrusted. Verify never follows a restored symlink parent and never reads outside the restore dir. |
| Manifests and state JSON | the sapper user (`0700` state dir, `0600` files) | Trusted-ish; still bounded: manifest paths must be clean and under a recorded root. |
| Environment, `--password-file`, stdin | the sapper user | Trusted. Password file mode is checked; the password goes to restic only as `RESTIC_PASSWORD` in the child environment, never in argv or logs. |
| `restic`, `docker`, `podman` on `PATH` | the sapper user / root | Trusted (a hostile PATH already owns the account). |

## Threats considered and mitigations

| # | Threat (CWE) | Mitigation | Test |
|---|---|---|---|
| T1 | Newline injection into `restic.env` adding e.g. `RESTIC_PASSWORD_COMMAND` (CWE-93) | control characters rejected in repository, data_root, volumes | `TestValidateRejectsInjection`, `FuzzRender` |
| T2 | Compose short-syntax injection via `:`/`,` in host paths (CWE-74) | `:`/`,` rejected | same |
| T3 | Extra lines in `backup-paths.txt` exfiltrating `/root/.ssh` into the repo | newline rejected | same |
| T4 | Cron injection via `%` or newline in the blueprint directory (CWE-78) | render refuses | `TestRenderRefusesCronUnsafeDir` |
| T5 | Argument injection into ssh via `sftp:-oProxyCommand=…` (CWE-88) | leading `-` refused for sftp host/user, rclone remote, and any repo | `TestValidateRejectsRepoOptionInjection` |
| T5b | Command substitution when an operator sources `restic.env` (`$(…)`, backticks, spaces) (CWE-78) | whitespace and shell metacharacters refused in the repository and its resolved location (POSIX only) | `TestValidateRejectsShellMetaInRepository`, `TestRenderRefusesShellMetaInRepoLocation` |
| T6 | Mounting the docker socket, `/`, `/etc`, … into a web app (CWE-250) | sensitive host paths and `*.sock` refused | `TestValidateRejectsInjection` |
| T7 | Container privilege escalation via setuid | `security_opt: no-new-privileges:true` | `TestServicesDropNewPrivileges` |
| T8 | Apps exposed to the network | ports always bound to `127.0.0.1` | `TestBuild` |
| T9 | Restore-time symlink traversal / TOCTOU reading host files (CWE-59, CWE-22) | no stat through symlink parents; entries must stay under roots | `TestVerifyDoesNotFollowRestoredSymlinkParents`, `FuzzVerify` |
| T10 | Path traversal via snapshot ID from restic output (CWE-22) | hex-only IDs | `TestBreachRejectsTraversalSnapshotID` |
| T11 | Secrets in argv/logs/JSON (CWE-532, CWE-214) | password via env only; password_env not inherited; repo URL passwords redacted in output | `TestCmdDropsPasswordSourcesFromChildEnv`, `TestBackupDryRunRedactsRepoPassword` |
| T12 | World-readable secrets on disk (CWE-276) | `.sapper/` `0700` (tightened if pre-existing), files `0600`; state `0700/0600`; warning on loose `--password-file` | `TestWriteTightensExistingDir`, `TestPasswordFileWarnsWhenWorldReadable` |
| T13 | Temp dir races (CWE-377) | `os.MkdirTemp` (`0700`, random name), restore target checked after creation, removed afterwards unless `--keep` | ops tests |
| T14 | YAML bombs / huge input (CWE-400, CWE-776) | 1 MiB cap; yaml.v3 v3.0.1 alias limits | `TestLoadRejectsHugeBlueprint`, `TestParseBillionLaughs` |
| T15 | Vulnerable dependencies | `govulncheck` clean at time of audit | CI suggestion below |
| T16 | Terminal escape or bidi-override injection through restored file names, restic error text or a hand-edited report (CWE-150) | every free-text field passes through `report.Clean` (Go-quoted when it has control, format or invalid UTF-8 characters) before display; Markdown paths go in code spans that cannot be closed from inside | `TestHostileNamesAreNeutralised`, `TestTamperedReportFieldsAreCleaned`, `FuzzClean` |
| T17 | Repository URL password copied into a shareable report via restic's error text (CWE-532) | report stores the redacted repository; the repository string in error text is replaced with its redacted form | `TestReportRedactsRepositoryPassword` |
| T18 | Silent edit of past drill results to hide a failed drill (CWE-345) | sha256 hash chain over reports; `reports verify`; optional ed25519 signatures with a pinned public key; `--head` for truncation | `TestChainDetectsTampering`, `TestSigning` |
| T19 | Signing key leaked or committed (CWE-312, CWE-522) | `keygen` writes `0600`, `O_EXCL`, default under the user config dir, never prints the private key; `--sign-key` warns on loose modes; load errors never echo file contents | `TestLoadPrivateKeyWarnsAndHidesContents`, `TestReportsEndToEnd` |
| T20 | Planted huge or symlinked report files (CWE-400, CWE-59) | report reads capped at 32 MiB, key reads at 64 KiB; new reports created with `O_EXCL` so an existing file or symlink is never followed or overwritten | `TestAppendRefusesToChainPastUnreadableReport` |

## Residual risks / not addressed

- **Image pinning by tag, not digest.** Catalogue images use version tags (`vaultwarden/server:1.32.7` …). A compromised registry account could re-push a tag. Pinning `@sha256:` digests needs a release process to refresh them.
- **No capability dropping / read-only rootfs.** Only `no-new-privileges` is set; `cap_drop: [ALL]` was not added because the images' needs were not tested.
- **`build --dry-run` prints `restic.env` verbatim**, including any credential embedded in the repository URL. Prefer restic's own credential env vars (e.g. `AWS_SECRET_ACCESS_KEY`) over URL userinfo.
- **RESTIC_PASSWORD in the child environment** is readable by the same user (and root) via `/proc/<pid>/environ` while restic runs. restic supports `--password-file`; switching to a `0600` temp file or a pipe would narrow this further.
- **Data at rest in the drill directory**: with `--keep`, a decrypted copy of all app data stays in the temp dir (`0700`).
- **Report chain without a signing key** proves only that reports were not edited *quietly*: anyone who can write the state directory can rebuild an unsigned chain. Without `--head`, deleting the newest reports is invisible. Reports contain host paths; read the Markdown copy before sharing it.
- **Upstream tools**: restic's handling of hostile repositories, docker's handling of compose files, and the catalogue images are trusted.
- **Supply chain of sapper itself**: release binaries are not signed yet; consider SLSA provenance / cosign and a `govulncheck` CI step.
