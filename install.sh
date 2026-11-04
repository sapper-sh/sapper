#!/bin/sh
# sapper installer for Linux, macOS and WSL2.
#   curl -fsSL https://raw.githubusercontent.com/sapper-sh/sapper/main/install.sh | sh
# Env: SAPPER_VERSION (tag, default latest), SAPPER_INSTALL_DIR (default
# ~/.local/bin, or /usr/local/bin as root). No telemetry: the only network
# calls go to github.com (and proxy.golang.org for the go install fallback).
set -eu

REPO=sapper-sh/sapper
say() { printf 'sapper-install: %s\n' "$*"; }
die() { printf 'sapper-install: error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

fetch() { # url dest
	if have curl; then curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
	elif have wget; then wget -q -O "$2" "$1"
	else die "need curl or wget"; fi
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
	linux|darwin) ;;
	*) die "unsupported OS '$os'; on Windows use install.ps1 (WSL2)" ;;
esac
arch=$(uname -m)
case "$arch" in
	x86_64|amd64) arch=amd64 ;;
	aarch64|arm64) arch=arm64 ;;
	*) die "unsupported architecture '$arch'" ;;
esac
wsl=no
if [ "$os" = linux ] && grep -qi microsoft /proc/version 2>/dev/null; then wsl=yes; fi
say "detected $os/$arch$( [ "$wsl" = yes ] && printf ' (WSL)')"

if [ -n "${SAPPER_INSTALL_DIR:-}" ]; then dir=$SAPPER_INSTALL_DIR
elif [ "$(id -u)" = 0 ]; then dir=/usr/local/bin
else dir=$HOME/.local/bin; fi
mkdir -p "$dir"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

version=${SAPPER_VERSION:-}
if [ -z "$version" ]; then
	if fetch "https://api.github.com/repos/$REPO/releases/latest" "$tmp/latest.json" 2>/dev/null; then
		version=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n1)
	fi
fi

installed=no
if [ -n "$version" ]; then
	asset="sapper-$version-$os-$arch"
	base="https://github.com/$REPO/releases/download/$version"
	say "downloading $asset"
	fetch "$base/$asset" "$tmp/$asset" || die "download failed: $base/$asset"
	fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" || die "could not fetch SHA256SUMS"
	if have cosign && fetch "$base/SHA256SUMS.sigstore.json" "$tmp/SHA256SUMS.sigstore.json" 2>/dev/null; then
		cosign verify-blob --bundle "$tmp/SHA256SUMS.sigstore.json" \
			--certificate-identity-regexp "^https://github.com/$REPO/\.github/workflows/release\.yml@refs/tags/" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com \
			"$tmp/SHA256SUMS" >/dev/null 2>&1 || die "cosign signature verification FAILED"
		say "cosign signature verified"
	else
		say "cosign not found; skipping signature check (checksum still verified)"
	fi
	want=$(awk -v f="$asset" '$2==f || $2=="*"f {print $1}' "$tmp/SHA256SUMS")
	[ -n "$want" ] || die "$asset not listed in SHA256SUMS"
	if have sha256sum; then got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
	else got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}'); fi
	[ "$want" = "$got" ] || die "checksum mismatch for $asset"
	say "checksum OK"
	chmod 0755 "$tmp/$asset"
	mv "$tmp/$asset" "$dir/sapper"
	installed=yes
else
	say "no published release yet; building from source with go install"
	have go || die "no release available and Go is not installed; install Go 1.26+ (https://go.dev/dl) and rerun"
	GOBIN=$dir go install "github.com/$REPO/cmd/sapper@latest"
	installed=yes
fi

[ "$installed" = yes ] && say "installed $dir/sapper"
case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "add $dir to your PATH, e.g.: echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile" ;;
esac

# Runtime dependencies: advise, never install silently.
if have restic; then say "restic: found"
else
	case "$os" in
		darwin) say "restic: missing; install with: brew install restic" ;;
		*) say "restic: missing; install with: sudo apt install restic (or your distro's package; https://restic.net)" ;;
	esac
fi
if have docker || have podman; then
	say "container engine: found (needed only for 'sapper build --up')"
elif [ "$wsl" = yes ]; then
	say "docker: missing; install Docker Desktop on Windows and enable Settings > Resources > WSL integration for this distro, or install docker-ce inside WSL"
else
	say "docker: missing (needed only for 'sapper build --up'); see https://docs.docker.com/engine/install/"
fi
say "done. Try: sapper --help"
