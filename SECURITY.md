# Security policy

## Reporting a vulnerability

> **TODO (author):** replace `<OWNER>` below and in `.well-known/security.txt`
> with the real GitHub owner, and add a contact address, before publishing.

Please report vulnerabilities privately through GitHub's private vulnerability
reporting: **Security → Report a vulnerability** on the sapper repository
(`https://github.com/<OWNER>/sapper/security/advisories/new`). Do not open a
public issue.

Include the sapper version (`sapper --version`), OS, restic and docker/podman
versions, and a minimal blueprint or steps that reproduce the problem.

## Response timelines

| Step | Target |
|---|---|
| Acknowledge the report | within 3 business days |
| Initial assessment and severity (CVSS v4.0) | within 10 business days |
| Status updates to the reporter | at least every 14 days |
| Fix and advisory for critical or high severity | within 30 days of confirmation |
| Fix and advisory for medium or low severity | within 90 days of confirmation |

These are targets for a volunteer project, not guarantees. If a deadline will be
missed, the reporter is told why and given a new date. Reporters are credited in
the advisory unless they ask not to be.

## CVE policy

Confirmed vulnerabilities in a released version get a GitHub Security Advisory
and a CVE requested through GitHub as the CNA. Each advisory names the affected
and fixed versions, the CWE (for example CWE-22 for path traversal), a CVSS v4.0
vector, and the CPE `cpe:2.3:a:<OWNER>:sapper:*:*:*:*:*:*:*:*` with the affected
version range. Fixes are also reported to the Go vulnerability database so
`govulncheck` users are warned.

## Safe harbor

Modeled on [CISA's vulnerability disclosure policy template](https://www.cisa.gov/vulnerability-disclosure-policy-template).

If you make a good-faith effort to comply with this policy while researching
sapper, we will consider your research authorized, will work with you to
understand and resolve the issue quickly, and will not recommend or pursue legal
action against you. If a third party starts legal action against you for
activity that complied with this policy, we will make it known that your
research was authorized.

Good faith means that you:

- test only against your own installations and data, never against systems or
  data you do not own or have permission to test;
- stop and report as soon as you reach any data that is not yours, and do not
  keep, share or disclose it;
- do not run denial-of-service, social engineering or physical attacks;
- give us a reasonable time to fix the problem before disclosing it publicly,
  in line with the timelines above.

This safe harbor covers sapper's own code. It cannot authorize testing of
restic, docker, podman, the catalogue images, or anyone else's systems.

## Supported versions

Only the latest release receives security fixes.

## Scope

In scope: anything that lets a blueprint, a restic repository, a snapshot, a
manifest or another local user make sapper run unintended commands, write or read
outside the paths it should, expose secrets, or weaken the containers it renders.

Out of scope: vulnerabilities in restic, docker, podman or the catalogue images
themselves (report those upstream), and attacks that already require root or the
sapper user's account.

The threat model is in [docs/THREAT-MODEL.md](docs/THREAT-MODEL.md).

## Hardening checklist for operators

- Keep `fortress.blueprint` writable only by you; treat a blueprint from someone else like a script.
- Store the restic password in a `0600` file (`--password-file`) or pass it on stdin; avoid putting it on the command line.
- Keep the restic repository off the host (or append-only), so ransomware on the host cannot delete backups.
- Leave the published ports on `127.0.0.1` and put a TLS reverse proxy or VPN in front; never port-forward the apps directly.
- Run `sapper breach` on a schedule; a backup that has never been restored is not a backup.
- Sign drill reports (`sapper keygen`, `--sign-key`), keep a copy of the public key off the host, and now and then record the latest report hash elsewhere so `sapper reports verify --pub-key ... --head ...` can catch a rewritten history.
- Update restic, docker and the catalogue images promptly.
