# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's private vulnerability
reporting: **Security → Report a vulnerability** on the sapper repository
(`https://github.com/<OWNER>/sapper/security/advisories/new` — placeholder until the
repository is published). Do not open a public issue.

Include the sapper version (`sapper --version`), OS, restic and docker/podman
versions, and a minimal blueprint or steps that reproduce the problem. We aim to
acknowledge reports within 7 days and to publish a fix and advisory within 90 days.

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
