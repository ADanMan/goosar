# Goosar — CLI installer for Windows, served by the deployment
# itself at <app-url>/install.ps1 (#567). The PowerShell mirror of install.sh:
# same URL convention, same fail-closed checksum rules, no GitHub, no sudo.
#
# Run (PowerShell 5.1 or 7):
#   & ([scriptblock]::Create((irm https://goosar.example.com/install.ps1))) `
#       -AppUrl https://goosar.example.com -ServerUrl https://goosar.example.com
#
# Or download first and read it:
#   irm https://goosar.example.com/install.ps1 -OutFile install.ps1
#   powershell -ExecutionPolicy Bypass -File install.ps1 -AppUrl ... -ServerUrl ...
#
# Parameters (env equivalents in brackets):
#   -AppUrl <url>        Goosar web URL that serves this script and the CLI
#                        binaries. Required [GOOSAR_APP_URL].
#   -ServerUrl <url>     Goosar API URL; when given, the exact `goosar`
#                        commands to connect are printed [GOOSAR_SERVER_URL].
#   -InstallDir <dir>    Where goosar.exe goes
#                        (default: %USERPROFILE%\.goosar\bin) [GOOSAR_BIN_DIR].
#   -SkipChecksum        Skip checksums.txt verification. Loud warning.
#                        [GOOSAR_SKIP_CHECKSUM]
#   -Runner <name>       Agent runner to install: none (default). The runner
#                        bundled with a deployment has no Windows build, so
#                        `none` is the only choice here [GOOSAR_RUNNER].
#   -Help                This text.
#
# Download URL convention (served by the Goosar deployment):
#   <app-url>/cli/goosar-cli-windows-amd64.tar.gz
#   <app-url>/cli/checksums.txt            (required unless -SkipChecksum)
#
# A deployment serves exactly one CLI version — the one matching its server.

param(
    [string]$AppUrl = "",
    [string]$ServerUrl = "",
    [string]$InstallDir = "",
    [switch]$SkipChecksum,
    [string]$Runner = "",
    [string]$Version = "",
    [switch]$Help
)

$ErrorActionPreference = "Stop"

function Write-Info { param([string]$Msg) Write-Host "==> $Msg" -ForegroundColor Cyan }
function Write-Ok   { param([string]$Msg) Write-Host "[OK] $Msg" -ForegroundColor Green }
function Write-Warn { param([string]$Msg) Write-Host "[WARN] $Msg" -ForegroundColor Yellow }
function Write-Fail { param([string]$Msg) Write-Host "[ERROR] $Msg" -ForegroundColor Red; exit 1 }

if ($Help) {
    # A here-string, not the file header: when the script arrives through
    # `irm … | scriptblock` there is no file and $PSCommandPath is empty.
    Write-Host @"
Goosar — CLI installer for Windows

Usage:
  & ([scriptblock]::Create((irm <app-url>/install.ps1))) -AppUrl <app-url> [-ServerUrl <api-url>]

Parameters (env equivalents in brackets):
  -AppUrl <url>        Goosar web URL that serves this script and the CLI
                       binaries. Required [GOOSAR_APP_URL].
  -ServerUrl <url>     Goosar API URL; when given, the exact goosar
                       commands to connect are printed [GOOSAR_SERVER_URL].
  -InstallDir <dir>    Where goosar.exe goes
                       (default: %USERPROFILE%\.goosar\bin) [GOOSAR_BIN_DIR].
  -SkipChecksum        Skip checksums.txt verification. Loud warning.
                       [GOOSAR_SKIP_CHECKSUM]
  -Runner <name>       Agent runner to install: none (default). The runner
                       bundled with a deployment has no Windows build, so
                       none is the only choice here [GOOSAR_RUNNER].
  -Help                This text.

Download URL convention (served by the Goosar deployment):
  <app-url>/cli/goosar-cli-windows-amd64.tar.gz
  <app-url>/cli/checksums.txt            (required unless -SkipChecksum)

A deployment serves exactly one CLI version — the one matching its server.
"@
    exit 0
}

# --- Inputs ------------------------------------------------------------------
if ($Version) {
    # Same refusal as install.sh (#566): a deployment serves exactly one CLI
    # version, so a versioned archive never exists.
    Write-Fail "-Version is not supported: a deployment serves exactly one CLI version, the one matching its server. Run the installer without -Version, or install from a deployment of the version you need."
}
if (-not $AppUrl) { $AppUrl = $env:GOOSAR_APP_URL }
if (-not $ServerUrl) { $ServerUrl = $env:GOOSAR_SERVER_URL }
if (-not $InstallDir) { $InstallDir = $env:GOOSAR_BIN_DIR }
if (-not $SkipChecksum -and $env:GOOSAR_SKIP_CHECKSUM) { $SkipChecksum = $true }
if (-not $Runner) { $Runner = $env:GOOSAR_RUNNER }
if (-not $Runner) { $Runner = "none" }
$Runner = $Runner.Trim().ToLowerInvariant()
if ($Runner -ne "none") {
    Write-Fail "Unknown runner '$Runner'. Available on Windows: none (the runner bundled with a deployment has no Windows build)."
}

if (-not $AppUrl) {
    Write-Fail "-AppUrl is required: the URL of the Goosar deployment that serves this script, e.g. -AppUrl https://goosar.example.com (env: GOOSAR_APP_URL)"
}
if ($AppUrl -notmatch '^https?://') { $AppUrl = "https://$AppUrl" }
$AppUrl = $AppUrl.TrimEnd('/')
if ($ServerUrl) {
    if ($ServerUrl -notmatch '^https?://') { $ServerUrl = "https://$ServerUrl" }
    $ServerUrl = $ServerUrl.TrimEnd('/')
}
if (-not $InstallDir) { $InstallDir = Join-Path $env:USERPROFILE ".goosar\bin" }

# --- Platform ----------------------------------------------------------------
if (-not [Environment]::Is64BitOperatingSystem) {
    Write-Fail "Goosar requires 64-bit Windows."
}
$archRaw = $env:PROCESSOR_ARCHITEW6432
if (-not $archRaw) { $archRaw = $env:PROCESSOR_ARCHITECTURE }
if ($archRaw -ne "AMD64") {
    Write-Fail "Unsupported architecture: $archRaw. The deployment serves goosar-cli-windows-amd64 only; on ARM64 Windows run the x64 build through emulation or install the desktop app."
}
if (-not (Get-Command tar.exe -ErrorAction SilentlyContinue)) {
    Write-Fail "tar.exe not found. Windows 10 1803+ ships it; on older systems install the desktop app instead."
}

$Asset = "goosar-cli-windows-amd64.tar.gz"
$Url = "$AppUrl/cli/$Asset"

Write-Host ""
Write-Host "  Goosar — CLI Installer" -ForegroundColor White
Write-Host "  Source: $AppUrl"
Write-Host ""

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("goosar-cli-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TmpDir | Out-Null
try {
    # --- Download ------------------------------------------------------------
    Write-Info "Downloading $Url"
    try {
        Invoke-WebRequest -Uri $Url -OutFile (Join-Path $TmpDir $Asset) -UseBasicParsing
    } catch {
        Write-Fail "Failed to download the CLI from:`n  $Url`n`nCheck that:`n  - $AppUrl is reachable from this machine`n  - this Goosar deployment bundles CLI binaries under /cli/ (it may not)`n  - a windows/amd64 build exists on this deployment`n`nAsk your Goosar administrator to publish the CLI binaries, or install the Goosar desktop app instead.`n$_"
    }

    # --- Checksum (fail-closed, same four branches as install.sh) ------------
    if ($SkipChecksum) {
        Write-Warn "Checksum verification skipped (-SkipChecksum / GOOSAR_SKIP_CHECKSUM). Only do this in an environment you trust."
    } else {
        $checksumsPath = Join-Path $TmpDir "checksums.txt"
        try {
            Invoke-WebRequest -Uri "$AppUrl/cli/checksums.txt" -OutFile $checksumsPath -UseBasicParsing
        } catch {
            Write-Fail "Could not download checksums.txt - aborting: the download cannot be verified. Ask the administrator to publish it next to the CLI archives, or pass -SkipChecksum if you trust this source. $_"
        }
        $line = Get-Content -LiteralPath $checksumsPath |
            Where-Object { ($_ -split '\s+')[-1] -replace '^\*', '' -replace '^.*/', '' -eq $Asset } |
            Select-Object -First 1
        if (-not $line) {
            Write-Fail "checksums.txt has no entry for $Asset - aborting: the download cannot be verified."
        }
        $expected = ($line -split '\s+')[0].ToLower()
        $actual = (Get-FileHash -Path (Join-Path $TmpDir $Asset) -Algorithm SHA256).Hash.ToLower()
        if ($actual -ne $expected) {
            Write-Fail "Checksum verification failed for $Asset.`n  expected: $expected`n  got:      $actual`nThe download is corrupt or the server is not the one you think it is."
        }
        Write-Ok "Checksum verified"
    }

    # --- Extract -------------------------------------------------------------
    # tar.exe is a native command: on 5.1 its exit code never throws, on 7.4+
    # a non-zero exit throws NativeCommandExitException. Handle both so a
    # corrupt archive is reported as such, not as "no goosar.exe inside".
    $tarOut = ""
    try {
        $tarOut = & tar.exe -xzf (Join-Path $TmpDir $Asset) -C $TmpDir goosar.exe 2>&1 | Out-String
        $tarExit = $LASTEXITCODE
    } catch {
        $tarExit = 1
        $tarOut = "$_"
    }
    if ($tarExit -ne 0) {
        Write-Fail "Could not extract $Asset (tar.exe exit $tarExit): $tarOut`nThe download may be corrupt or the temp directory full; re-run the installer."
    }
    $exe = Join-Path $TmpDir "goosar.exe"
    if (-not (Test-Path -LiteralPath $exe)) {
        Write-Fail "The archive did not contain goosar.exe at its root."
    }

    # --- Install (stage next to the target, then atomic move) ---------------
    if (-not (Test-Path -LiteralPath $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    $target = Join-Path $InstallDir "goosar.exe"
    $staged = Join-Path $InstallDir ".goosar.install.$PID.exe"
    try {
        Move-Item -LiteralPath $exe -Destination $staged -Force
        Move-Item -LiteralPath $staged -Destination $target -Force
    } catch {
        Write-Fail "Could not write $target`: $_`nIf a goosar daemon is running from this path, stop it first (goosar daemon stop) and re-run the installer."
    }
    Write-Ok "Installed $target"
} finally {
    Remove-Item -LiteralPath $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

# --- Verify (fatal, like install.sh after #566) ------------------------------
# Windows PowerShell 5.1 wraps every native stderr line merged by 2>&1 into an
# ErrorRecord and, under ErrorActionPreference=Stop, escalates it to a
# terminating error even when the process exits 0. Relax the preference for
# this one call; the exit code is the verdict.
$prevEap = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$versionOut = (& $target --version 2>&1 | Out-String).Trim()
$probeExit = $LASTEXITCODE
$ErrorActionPreference = $prevEap
if ($probeExit -ne 0) {
    Write-Fail "Installed, but '$target --version' failed:`n$versionOut`n`nThe binary was written but does not run on this machine. Remove $target if you do not want the broken copy around."
}
Write-Ok "$versionOut"

# --- PATH --------------------------------------------------------------------
# The user PATH is REG_EXPAND_SZ and may hold %VAR% entries. Reading it through
# [Environment]::GetEnvironmentVariable expands them, and writing the expanded
# string back destroys every %VAR% — a classic way to corrupt a user's PATH.
# Read unexpanded, write back with the same kind.
function Add-ToUserPath {
    param([string]$Dir)
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Environment", $true)
    if (-not $key) {
        # A minimal or freshly provisioned profile may not have the key yet.
        $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey("Environment")
    }
    if (-not $key) {
        Write-Warn "Could not open HKCU\Environment to update PATH. Add $Dir to your user PATH by hand."
        return $false
    }
    try {
        $current = [string]$key.GetValue("Path", "", [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $entries = @($current -split ';' | Where-Object { $_ })
        if ($entries | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') }) { return $false }
        $new = if ($current) { "$current;$Dir" } else { $Dir }
        $key.SetValue("Path", $new, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    } finally {
        $key.Close()
    }
    if (($env:Path -split ';' | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') }).Count -eq 0) {
        $env:Path = "$Dir;$env:Path"
    }
    return $true
}

if (Add-ToUserPath $InstallDir) {
    Write-Info "Added $InstallDir to your user PATH. Open a new terminal for other sessions to pick it up."
}

# --- Next steps --------------------------------------------------------------
Write-Host ""
Write-Host "  Agent runner: none installed. Agents need a runner on the machine that"
Write-Host "  executes tasks: install an agent CLI yourself; goosar doctor shows what is missing."
Write-Host ""
if ($ServerUrl) {
    Write-Host "  Next: connect this machine to your Goosar" -ForegroundColor White
    Write-Host ""
    Write-Host "     goosar setup self-host --server-url $ServerUrl --app-url $AppUrl" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Then: goosar daemon status"
} else {
    Write-Host "  Next: goosar setup self-host --server-url <api-url> --app-url $AppUrl" -ForegroundColor Cyan
}
Write-Host ""
