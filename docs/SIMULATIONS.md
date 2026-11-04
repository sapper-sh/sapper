# Disaster simulations

Backups usually fail quietly and get noticed during a restore. Each scenario below
recreates a failure that has lost real data, runs it against a real restic
repository (restic 0.17.3), and records what sapper did.

Run it yourself:

    ./scripts/simulate.sh          # about three minutes; exits 1 on any miss
    go test -v ./internal/sim/     # the same tests, with detail

The tests are in `internal/sim`. `go test -short ./...` skips them.

## How to read the results

- **Caught**: sapper refused, failed the drill, or warned, and said why.
- **Partial**: sapper did not report false success, but the message did not say
  what was wrong or what to do.
- **Missed**: sapper reported success, or let the failure through without a word.
- **Gap**: a known miss, documented below. It does not fail the campaign.

"Before" is the code at commit `ada1ced`, before this work. "After" is the current
code. Scenarios marked *simulated* fake the condition instead of producing it;
the table says how.

## Scoreboard

Before: 9 caught, 3 partial, 12 missed (of 24 that could run).
After: 25 caught, 0 partial, 0 missed, 1 known gap (of 26).

| # | Scenario | Real-world precedent | Before | After | What sapper does now |
|---|---|---|---|---|---|
| S01 | One bit flipped in a pack file | Silent corruption: CERN found about 1 bad file in 1,500 ([StorageMojo on the CERN study](https://storagemojo.com/2007/09/19/cerns-data-corruption-research/)) | Missed | Caught | The drill restores with `--no-cache` and fails with NO VERDICT, quoting restic's decryption error. Before, restic served the damaged tree pack from its local cache and the drill said SURVIVED. |
| S02 | Pack file deleted | Code Spaces 2014: attacker deleted data and backups ([Ars Technica](https://arstechnica.com/information-technology/2014/06/aws-console-breach-leads-to-demise-of-service-with-proven-backup-plan/)) | Missed | Caught | Same fix as S01: NO VERDICT, restic names the missing pack. |
| S03 | Pack file truncated | Interrupted uploads, full disks | Missed | Caught | Same fix: NO VERDICT, "file is too short". |
| S04 | Index files deleted | Partial deletion of a repository | Caught | Caught | NO VERDICT: restic cannot find the snapshot's trees. |
| S05 | Every snapshot deleted | Code Spaces 2014 ([Ars Technica](https://arstechnica.com/information-technology/2014/06/aws-console-breach-leads-to-demise-of-service-with-proven-backup-plan/)) | Caught | Caught | NO VERDICT, "no snapshot found". Detecting it does not bring the data back; see S23. |
| S06 | Ransomware encrypts live data, then the nightly backup runs | Conti playbooks target backups ([eSentire](https://www.esentire.com/blog/analysis-of-leaked-conti-intrusion-procedures-by-esentires-threat-response-unit-tru), [Threatpost](https://threatpost.com/conti-ransomware-backups/175114/)) | Missed | Caught | Backup refuses with exit 6: most files changed and the changed files have high entropy. `--force` overrides. |
| S07 | Volume not mounted, so backup sees an empty directory | rsync `--delete` from an unmounted mount point ([Ask Ubuntu](https://askubuntu.com/questions/1332148/rsync-with-delete-before-except-if-source-empty)) | Missed | Caught | Backup refuses, exit 6: "the directory is empty; the last backup had N files. If it is a mount point, check that the disk is mounted". |
| S08 | `rm -rf` removes most files before the backup | Toy Story 2, 1998: 90% of the files deleted, backups had failed silently ([The Next Web](https://thenextweb.com/news/how-pixars-toy-story-2-was-deleted-twice-once-by-technology-and-again-for-its-own-good)) | Missed | Caught | Backup refuses, exit 6: "only 1 of the 11 files from the last backup are left". |
| S09 | Host lost; drill from a new machine | Maersk/NotPetya 2017: every domain controller wiped, one offline copy survived ([Wired](https://www.wired.com/story/notpetya-cyberattack-ukraine-russia-code-crashed-the-world/?page=2)) | Partial | Caught | Every backup now stores its manifest inside the repository. A drill with no local manifest uses that copy, says so in the report, and verifies. |
| S10 | Local manifest edited after the backup | Covering tracks after tampering | Caught | Caught | BREACHED. Now also a `manifest-mismatch` finding: the local manifest and the repository copy disagree. |
| S11 | Saved drill report edited | Hiding a failed drill | Caught | Caught | `sapper reports verify` reports the broken hash. |
| S12 | Newest drill report deleted | Hiding a failed drill | Caught | Caught | Caught only with `--head <hash>` recorded elsewhere. Without it, deletion of the newest reports is invisible (documented in the README). |
| S13 | Database written while the backup runs | SQLite: copying a live database can produce a corrupt copy ([sqlite.org](https://sqlite.org/howtocorrupt.html)) | Caught | Caught | Backup refuses: "volumes changed during backup". sapper has no database dump hook yet; stop the apps first. |
| S14 | Backup killed mid-run (SIGKILL) | Power loss, OOM killer, reboots | Caught | Caught | The killed run leaves no manifest or record. restic removes its own stale lock (same host, dead PID); the next backup and drill succeed. |
| S15 | Repository locked by another restic process | Stale locks after crashes ([restic unlock](https://man.archlinux.org/man/extra/restic/restic-unlock.1.en)) | Partial | Caught | `ErrLocked`: the lock holder from restic, then how to inspect (`restic list locks --no-lock`) and clear (`restic unlock`) a stale lock, and a warning not to use `--remove-all` while restic may be running. `sapper preflight` lists held locks. |
| S16 | Wrong password or lost key | Lost keys make encrypted backups unreadable | Partial | Caught | NO VERDICT with `ErrWrongPassword`, which names the password sources and says nobody can read the backups without it. `sapper preflight` checks the password. |
| S17 | Disk fills during the drill restore (*simulated*: restic run with `ulimit -f`, since tmpfs needs root) | Toy Story 2's backup failed on a full volume | Caught | Caught | NO VERDICT with restic's write error. Never SURVIVED. |
| S18 | A snapshot stamped a year in the future (*simulated*: `restic backup --time`) | Clock skew between hosts | Missed | Caught | The drill warns that the snapshot is stamped in the future and that newer backups may exist, and suggests `--snapshot`. Snapshot times are now compared as times, not strings. |
| S19 | Retention prunes the only verified snapshot | GitLab 2017: old backups cleaned out while new ones were empty ([GitLab](https://about.gitlab.com/blog/gitlab-dot-com-database-incident/)) | Missed | Caught | `keep_last` never forgets the newest snapshot that passed a drill. Retention now applies only to this fortress's snapshots. |
| S20 | An older snapshot is damaged; the latest is fine | Damage nobody looks for until a point-in-time restore is needed | Missed | Caught | `sapper breach --sample N` also restores and verifies N random older snapshots. A failure makes the drill BREACHED: "The latest backup restores correctly, but an older snapshot does not." |
| S21 | Backups silently stopped for 10 days | GitLab 2017: pg_dump failed, failure emails were rejected ([GitLab postmortem](https://about.gitlab.com/blog/postmortem-of-database-outage-of-january-31/)) | Caught | Caught | The drill reports the RPO target missed. It does not change the exit code (documented). |
| S22 | Repository deleted; the next backup would start a new empty one | Code Spaces; any wiped storage | Missed | Caught | Backup refuses: this host backed up there before and the repository is gone. `--force` starts a new one. Before, sapper ran `restic init` and carried on. |
| S23 | Ransomware on the host could delete the repository | Conti and Veeam targeting ([Threatpost](https://threatpost.com/conti-ransomware-backups/175114/)) | Missed | Caught | `sapper preflight` reports a RISK when this user can write a local repository or uses sftp, and recommends `rest-server --append-only` ([rest-server](https://github.com/restic/rest-server)) or object lock. This is a warning, not protection. |
| S24 | Not enough space for a drill restore (*simulated*: free-space probe returns 1 KiB) | Full disks | Missed | Caught | The drill and `preflight` check free space against the manifest size first and suggest `--temp-dir`. |
| S25 | Someone with the repository password plants a forged manifest copy | Insider or stolen backup credentials (Conti reads Veeam credentials) | n/a | Caught | With `backup --sign-key` and `breach --pub-key`, an unsigned or wrongly signed copy is refused. Without a key, two differing copies for one snapshot are refused. |
| S26 | Slow ransomware: a quarter of the files encrypted before each backup | Ransomware that stays under detection thresholds | Gap | Gap | Not caught. The mass-change check needs at least half the files changed. The backup matches what was on disk, so the drill passes too. `keep_last` keeps older clean snapshots for a while. |

Two corrections to the first "before" run: S16 and S17 were first scored Missed
because the test treated "no problems found" as success even when the drill
stopped early. Both drills returned NO VERDICT, so they are scored Partial and
Caught above. S19 was first scored Caught because the test matched the old
snapshot ID in the new snapshot's `parent` field. A direct check showed `keep_last 1`
had pruned it.

## What is not simulated

- GitLab's root cause, failure mail rejected by DMARC, is outside sapper. sapper
  gives exit codes (0, 3, 4, 6) and `--brief`; something has to alert on them.
- Expired cloud credentials and unreachable remotes: there is no cloud repository
  on the test machine. The same restic exit-code handling applies.
- Real ENOSPC, power loss during a repository write, and ZFS-style media errors
  under the repository. S01 to S03 damage pack files directly instead.
- A Postgres data directory copied while it runs. S13 shows sapper refuses any
  tree that changes during a backup. That catches writes during the backup, not
  writes before it that left a crash-consistent copy.

## Where the ideas came from

- restic proves the repository is consistent; `restic check --read-data` reads
  every pack. sapper also proves the restored files match the files on disk at
  backup time. S01 showed the drill must bypass restic's cache to do this.
- SQLite's test suite runs every branch and injects I/O errors, out-of-memory
  failures and simulated power loss ([How SQLite Is Tested](https://sqlite.org/testing.html)).
  This campaign uses the same idea: break the system on purpose and check the
  result.
- Jepsen injects faults and checks the history against what the system claims
  ([jepsen-io/jepsen](https://github.com/jepsen-io/jepsen/)). Here the claim is
  "SURVIVED means it will restore", and every scenario tests that claim.
- ZFS checksums each block and scrub reads all of them
  ([Wisconsin ZFS study](https://research.cs.wisc.edu/wind/Publications/zfs-corruption-fast10.pdf)).
  `breach --sample` does the same for older snapshots, a few at a time.
