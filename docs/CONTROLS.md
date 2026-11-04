# Controls sapper supports

sapper **supports** the controls below: it produces evidence and automation an
operator can use toward them. It does **not by itself satisfy** any of them.
Each control also needs policy, scheduling, people, off-host storage and review
that sapper does not provide. This page is a mapping for operators and
assessors, not a compliance claim.

## NIST SP 800-53 Rev. 5

Source: [NIST SP 800-53 Rev. 5](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final).

| Control | What the control asks for | How sapper supports it | What remains with the operator |
|---|---|---|---|
| CP-9 System Backup | Back up user and system data at a defined frequency; protect backups. | `sapper backup` runs restic over every declared volume and records a checksum manifest locally and in the repository. | Frequency, off-site copies, retention, repository access control. |
| CP-9(1) Testing for Reliability and Integrity | Test backups to verify media reliability and information integrity. | `sapper breach` restores into a throwaway directory and compares every file with the backup-time checksums; SURVIVED / BREACHED / NO VERDICT with exit codes 0 / 3 / 4. | Defining the test frequency (e.g. the rendered `drill.cron`) and acting on failures. |
| CP-9(2) Test Restoration Using Sampling | Use a sample of backup information in restoration as part of contingency plan testing. | `sapper breach --sample N` also restores and verifies N randomly chosen older snapshots; `--snapshot` drills a chosen one. | Choosing N and the schedule. |
| CP-9(8) Cryptographic Protection | Use cryptography to prevent unauthorized disclosure and modification of backup information. | restic encrypts and authenticates the repository; sapper adds sha256 manifests and optional ed25519-signed manifest copies (`--sign-key`, `--pub-key`). | Password and key custody; whether restic's cryptography meets your requirement (see [docs/FIPS.md](FIPS.md)). |
| CP-4 Contingency Plan Testing | Test the plan to determine effectiveness and readiness; review results. | Each drill saves a hash-chained, optionally signed report (JSON and Markdown) with timings and RTO/RPO results; `sapper reports verify` checks the chain. | The contingency plan itself, exercises beyond data restore, review and corrective action. |
| CP-10 System Recovery and Reconstitution | Recover and reconstitute the system to a known state within defined time. | `targets.rto` / `targets.rpo` in the blueprint are measured on every drill; `breach --strict` exits 7 when either is missed. The blueprint and `sapper build` re-create the service definitions. | Full-host recovery, infrastructure, DNS, secrets, and the people who perform it. |

## NIST SP 800-34 Rev. 1

Source: [NIST SP 800-34 Rev. 1, Contingency Planning Guide](https://csrc.nist.gov/pubs/sp/800/34/r1/upd1/final).
The guide calls for setting RTO and RPO in a business impact analysis and testing
recovery against them. sapper measures both on every drill and records the
result; it does not perform the business impact analysis.

## CIS Critical Security Controls v8.1

Source: [CIS Control 11, Data Recovery](https://www.cisecurity.org/controls/data-recovery).

| Safeguard | How sapper supports it |
|---|---|
| 11.2 Perform Automated Backups | `sapper backup`, scheduled by the operator. |
| 11.3 Protect Recovery Data | restic encryption; `preflight` warns when this host can delete its own backups. |
| 11.4 Establish and Maintain an Isolated Instance of Recovery Data | Supported only if the operator points the repository off-host or at append-only storage. |
| 11.5 Test Data Recovery | `sapper breach` and `--sample`; CIS asks for quarterly or more frequent testing of a sample of in-scope assets, which the operator schedules. |

## NIST SSDF (SP 800-218)

Source: [NIST SP 800-218, Secure Software Development Framework](https://csrc.nist.gov/pubs/sp/800/218/final).
These describe how sapper itself is built and released.

| Practice | sapper |
|---|---|
| PO.1 Define security requirements | [THREAT-MODEL.md](THREAT-MODEL.md) lists assets, trust boundaries, threats, mitigations and the tests for each. |
| PS.1 Protect all forms of code | Release workflow uses least-privilege `permissions`, actions pinned by commit SHA. |
| PS.2 Provide a mechanism for verifying release integrity | `SHA256SUMS` signed with cosign keyless signing; see "Verifying a release" in the README. |
| PS.3 Archive and protect each release | SLSA level 3 provenance and CycloneDX SBOMs attached to each release; reproducible builds (`make repro`, CI `reproducible` job). |
| PW.4 Reuse well-secured software | restic for backup cryptography instead of new code ([DESIGN.md](../DESIGN.md)). |
| PW.6 Configure the build process | `CGO_ENABLED=0`, `-trimpath`, `-buildid=`; optional FIPS build. |
| PW.7 / PW.8 Review and test code | `go vet`, staticcheck, race tests on three OSes, fuzz tests for untrusted text, simulation scenarios. |
| RV.1 Identify and confirm vulnerabilities | govulncheck in CI; private reporting in [SECURITY.md](../SECURITY.md). |
| RV.2 Assess, prioritize and remediate | Response timelines and CVE policy in SECURITY.md. |

Supply-chain levels: [SLSA v1.0](https://slsa.dev/spec/v1.0/levels).
