<#
.SYNOPSIS
  Installs sapper on Windows through WSL2.
.DESCRIPTION
  sapper is supported on Windows through WSL2, not natively. This script:
    1. checks the Windows build (WSL2 needs Windows 10 2004 / build 19041+ or Windows 11),
    2. enables WSL2 and installs Ubuntu if no distro is present (needs admin, may need a reboot),
    3. runs the Linux installer (install.sh) inside the default WSL distro.
  Safe to rerun. No telemetry: it only contacts github.com / raw.githubusercontent.com.

  irm https://raw.githubusercontent.com/sapper-sh/sapper/main/install.ps1 | iex

  Optional: $env:SAPPER_VERSION = 'v0.1.0' to pin a release; $env:SAPPER_DISTRO = 'Ubuntu' to choose a distro.
#>
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$InstallSh = 'https://raw.githubusercontent.com/sapper-sh/sapper/main/install.sh'

function Write-Step([string]$Message) { Write-Host "sapper-install: $Message" }
function Exit-Install([string]$Message) { Write-Host "sapper-install: error: $Message" -ForegroundColor Red; exit 1 }

function Test-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    return ([Security.Principal.WindowsPrincipal]$id).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-WslDistro {
    # wsl.exe prints UTF-16; read raw output and strip NULs.
    $wslCmd = Get-Command wsl.exe -ErrorAction SilentlyContinue
    if (-not $wslCmd) { return @() }
    $out = & wsl.exe --list --quiet 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $out) { return @() }
    return @($out | ForEach-Object { ($_ -replace "`0", '').Trim() } | Where-Object { $_ })
}

if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
    Exit-Install 'this script is for Windows; on Linux/macOS run: curl -fsSL https://raw.githubusercontent.com/sapper-sh/sapper/main/install.sh | sh'
}

$build = [Environment]::OSVersion.Version.Build
if ($build -lt 19041) {
    Exit-Install "Windows build $build is too old for WSL2; update to Windows 10 2004 (build 19041) or later, or Windows 11."
}
Write-Step "Windows build ${build}: OK"

$distros = Get-WslDistro
$distro = if ($env:SAPPER_DISTRO) { $env:SAPPER_DISTRO } elseif ($distros.Count -gt 0) { $null } else { 'Ubuntu' }

if ($distros.Count -eq 0 -or ($env:SAPPER_DISTRO -and ($distros -notcontains $env:SAPPER_DISTRO))) {
    if (-not (Test-Admin)) {
        Exit-Install 'WSL2 or the Linux distro is not installed yet. Re-run this in an elevated PowerShell (Run as administrator).'
    }
    $target = if ($distro) { $distro } else { 'Ubuntu' }
    Write-Step "installing WSL2 with $target (wsl --install -d $target --no-launch)"
    & wsl.exe --install -d $target --no-launch
    if ($LASTEXITCODE -ne 0) { Exit-Install "wsl --install failed (exit $LASTEXITCODE). Make sure virtualization is enabled in BIOS/UEFI." }
    Write-Host ''
    Write-Step "WSL2 and $target are installed. Next steps:"
    Write-Step "  1. Reboot Windows if this is the first time WSL was enabled."
    Write-Step "  2. Open '$target' from the Start menu once to create your Linux user."
    Write-Step "  3. Re-run this installer (it is safe to run again)."
    exit 0
}

# Make sure the distro runs as WSL2 (WSL1 cannot run Docker).
& wsl.exe --set-default-version 2 *> $null

$wslArgs = @()
if ($distro) { $wslArgs += @('-d', $distro) }
& wsl.exe @wslArgs -- true *> $null
if ($LASTEXITCODE -ne 0) {
    Exit-Install "the WSL distro did not start. Open it once from the Start menu to finish setup (create your user), reboot if asked, then re-run."
}

$envs = ''
if ($env:SAPPER_VERSION) {
    if ($env:SAPPER_VERSION -notmatch '^v[0-9A-Za-z.\-]+$') { Exit-Install "invalid SAPPER_VERSION '$($env:SAPPER_VERSION)'" }
    $envs = "SAPPER_VERSION=$($env:SAPPER_VERSION) "
}
Write-Step 'running the Linux installer inside WSL'
& wsl.exe @wslArgs -- sh -c "command -v curl >/dev/null 2>&1 || { echo 'sapper-install: curl missing in WSL; run: sudo apt update && sudo apt install -y curl' >&2; exit 1; }; curl -fsSL $InstallSh | ${envs}sh"
if ($LASTEXITCODE -ne 0) { Exit-Install "the Linux installer failed inside WSL (exit $LASTEXITCODE)." }

Write-Host ''
Write-Step 'sapper is installed inside WSL. Use it from a WSL shell:  wsl  then  sapper --help'
Write-Step 'For "sapper build --up", enable Docker Desktop > Settings > Resources > WSL integration, or install docker-ce inside WSL.'
Write-Step 'Keep fortress data in the Linux filesystem (~/...), not /mnt/c, for speed and correct permissions.'
