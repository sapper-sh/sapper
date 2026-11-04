# sapper on Windows (via WSL2)

sapper is supported on Windows **through WSL2**, not natively. restic, docker
compose and the fortress all run inside a Linux distro under WSL2. A native
`sapper.exe` is published and still does `init`, `validate`, `build` (render),
`backup`, `breach` and `status`, but `build --up` refuses and points you to WSL.

## Install

In PowerShell (run as administrator the first time, so WSL can be enabled):

    irm https://raw.githubusercontent.com/sapper-sh/sapper/main/install.ps1 | iex

What it does, and it is safe to rerun:

1. Checks the Windows build (needs Windows 10 2004 / build 19041+ or Windows 11).
2. If WSL has no distro, runs `wsl --install -d Ubuntu --no-launch` and stops.
   Reboot if asked, open **Ubuntu** once from the Start menu to create your
   Linux user, then run the one-liner again.
3. Runs `install.sh` inside WSL, which downloads the release binary, checks it
   against `SHA256SUMS` (and the cosign signature if `cosign` is installed),
   and installs to `~/.local/bin`. Before the first release it builds with
   `go install` instead (needs Go 1.26+ inside WSL).

Options: `$env:SAPPER_VERSION = 'v0.1.0'` pins a release; `$env:SAPPER_DISTRO = 'Debian'` picks a distro.
No telemetry; the scripts only talk to GitHub.

Prefer to do it by hand? Inside any WSL2 shell:

    curl -fsSL https://raw.githubusercontent.com/sapper-sh/sapper/main/install.sh | sh

## restic and docker

- restic, inside WSL: `sudo apt update && sudo apt install -y restic`
- docker, either:
  - Docker Desktop for Windows with **Settings > Resources > WSL integration**
    enabled for your distro, or
  - docker-ce installed inside WSL (`sudo service docker start`, or enable systemd in `/etc/wsl.conf`).

sapper detects WSL (`/proc/version` contains `microsoft`) and its docker
error hints then mention WSL integration instead of `systemctl`.

## Caveats

- Keep fortress state and data in the Linux filesystem (`~/`), not under
  `/mnt/c`: it is much faster and file permissions behave.
- WSL1 is not supported (no docker); the installer sets WSL2 as default.
- WSL stops when idle, so hosted services and scheduled drills only run while
  WSL is running. For an always-on server, use a Linux host.
- Corporate machines may block virtualization or `wsl --install`; ask IT.
