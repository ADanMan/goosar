# Goosar installer for Windows — one command to get started.
#
# The repository is private, so raw.githubusercontent.com 404s for everyone: run
# this script from a checkout.
#
# Install / upgrade CLI only (connects to goosar.ru):
#   powershell -ExecutionPolicy Bypass -File scripts\install.ps1
#
# Install CLI + provision a self-host server (Docker):
#   powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -WithServer
#
# After installation, run `goosar setup` to configure your environment.
#
# Flag parity with scripts/install.sh (#447) — same behavior, PowerShell spelling:
#
#   install.sh                 install.ps1
#   ------------------------   -------------------------------
#   --with-server              -WithServer
#   --upgrade                  -Upgrade
#   --stop                     -Stop
#   --tag <vX.Y.Z>             -Tag <vX.Y.Z>
#   --profile <p>              -DeliveryProfile / -Profile <perimeter|demo|dev|local|cloud>
#   --resend-key <key>         -ResendKey <key>
#   --smtp-host <host>         -SmtpHost <host>
#   --smtp-port <port>         -SmtpPort <port>
#   --smtp-user <user>         -SmtpUser <user>
#   --smtp-password <pass>     -SmtpPassword <pass>
#   --smtp-from <address>      -SmtpFrom <address>
#   --smtp-tls <mode>          -SmtpTls <starttls|implicit>
#   --llm-key <key>            -LlmKey <key>
#   --llm-base-url <url>       -LlmBaseUrl <url>
#   --help                     -Help
#
# `-DeliveryProfile` rather than `-Profile`: $PROFILE is a PowerShell automatic
# variable and shadowing it inside the installer is a trap, not a convenience.
# `-Profile` stays available as an alias (ADR-0015 documents that spelling) —
# an alias binds the parameter without introducing a $Profile variable.
#
# Environment variables (same names as install.sh):
#   GOOSAR_INSTALL_DIR   Self-host server install directory
#                         (default: $env:USERPROFILE\.goosar\server)
#   GOOSAR_SELFHOST_REF  Git ref to check out for self-host assets
#                         (default: latest release tag, falling back to main)

[CmdletBinding()]
param(
    [switch]$WithServer,
    [switch]$Upgrade,
    [switch]$Stop,
    # "" is the unset default and must be in the set: a ValidateSet attribute
    # stays attached to the variable, so any later assignment of "" (the tests
    # reset the flags this way) would be rejected.
    [ValidateSet("cloud", "perimeter", "demo", "dev", "local", "")]
    [Alias("Profile")]
    [string]$DeliveryProfile = "",
    [string]$Tag = "",
    [string]$ResendKey = "",
    [string]$SmtpHost = "",
    [string]$SmtpPort = "",
    [string]$SmtpUser = "",
    [string]$SmtpPassword = "",
    [string]$SmtpFrom = "",
    [string]$SmtpTls = "",
    [string]$LlmKey = "",
    [string]$LlmBaseUrl = "",
    [switch]$Help
)

$ErrorActionPreference = "Stop"
# git writes hints to stderr on successful calls; PowerShell 7.4+ would otherwise
# turn those into terminating errors.
$PSNativeCommandUseErrorActionPreference = $false

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$RepoUrl       = "https://github.com/adanman/goosar.git"
$RepoWebUrl    = "https://github.com/adanman/goosar"
# $env:USERPROFILE is empty on non-Windows PowerShell; falling back to $HOME
# keeps the script loadable there, which is how scripts/install.test.ps1 runs
# the .env bootstrap on a CI Linux/macOS runner.
$UserHome = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
$DefaultInstallDir = Join-Path $UserHome ".goosar\server"
$InstallDir    = if ($env:GOOSAR_INSTALL_DIR) { $env:GOOSAR_INSTALL_DIR } else { $DefaultInstallDir }

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Write-Info  { param([string]$Msg) Write-Host "==> $Msg" -ForegroundColor Cyan }
function Write-Ok    { param([string]$Msg) Write-Host "[OK] $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg) Write-Warning $Msg }
# Throws rather than `exit 1`: a terminating error still stops the installer
# (ErrorActionPreference = Stop, non-zero exit), and it lets the hermetic tests
# in scripts/install.test.ps1 assert on a refusal instead of dying with it.
function Write-Fail  { param([string]$Msg) Write-Host "[ERROR] $Msg" -ForegroundColor Red; throw $Msg }

function Test-CommandExists {
    param([string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function New-RandomHex {
    param([int]$ByteCount)

    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    return -join ($bytes | ForEach-Object { "{0:x2}" -f $_ })
}

function New-RandomBase64 {
    param([int]$ByteCount)

    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    return [Convert]::ToBase64String($bytes)
}

# Writes .env with LF line endings and UTF-8 without BOM. Set-Content on
# Windows PowerShell would emit CRLF and a BOM, and both end up *inside* the
# value docker compose reads.
function Write-EnvLines {
    param([string]$Path, [string[]]$Lines)

    $full = (Resolve-Path -LiteralPath $Path).Path
    [System.IO.File]::WriteAllText($full, (($Lines -join "`n") + "`n"), (New-Object System.Text.UTF8Encoding($false)))
}

# Reads .env / .env.example as UTF-8. Not Get-Content: Windows PowerShell 5.1 —
# which is what `powershell -File scripts\install.ps1` runs — decodes a BOM-less
# file through the ANSI codepage, and Write-EnvLines would then write the
# mangled characters back as UTF-8. .env.example is full of em dashes.
# Resolve-Path first: .NET resolves a relative path against the process working
# directory, which is not PowerShell's current location.
function Read-EnvLines {
    param([string]$Path)

    $full = (Resolve-Path -LiteralPath $Path).Path
    return [System.IO.File]::ReadAllLines($full)
}

# Replace the KEY= line(s) or append one — the PowerShell twin of set_env_kv in
# scripts/install.sh. Literal string work, not -replace: an API key or a base64
# secret containing $, & or \ must land in .env verbatim.
function Set-EnvKv {
    param([string]$Path, [string]$Name, [string]$Value)

    $prefix = "$Name="
    $found = $false
    $out = New-Object System.Collections.Generic.List[string]
    foreach ($line in @(Read-EnvLines $Path)) {
        if ($line.StartsWith($prefix)) {
            $found = $true
            $out.Add("$prefix$Value")
        } else {
            $out.Add($line)
        }
    }
    if (-not $found) {
        $out.Add("$prefix$Value")
    }
    Write-EnvLines -Path $Path -Lines $out.ToArray()
}

# Newest release tag known to this clone, by version sort (v0.10.0 > v0.9.0).
# Filtered to strict vX.Y.Z: a suffixed local tag (v1.0.0-dirty, left over
# from a build or a test run) must never outrank a real release just because
# --sort=-v:refname treats "1.0.0" as newer than "0.11.1".
function Get-LatestReleaseTag {
    try {
        $tags = git tag --list 2>$null | Where-Object { $_ -match '^v[0-9]+\.[0-9]+\.[0-9]+$' }
        $tag = $tags | Sort-Object { [version]($_.TrimStart('v')) } | Select-Object -Last 1
        if ($tag) { return "$tag".Trim() }
    } catch {}
    return ""
}

# --- .env bootstrap (#447) --------------------------------------------------
# PowerShell cannot run scripts/selfhost-env.sh, so this is its port and must
# stay in step with it: same variables, same encodings (hex for JWT/passwords,
# base64 for the two secretbox keys — secretbox.ValidateKeyEnv requires exactly
# 32 base64-decoded bytes), same perimeter default, same image pin, same
# never-touch-an-existing-.env rule. scripts/install-ps1.test.sh asserts the
# variable list against selfhost-env.sh so a new secret cannot be added on the
# bash side alone.
function Initialize-SelfHostEnv {
    [CmdletBinding()]
    param(
        [string]$ProfileName = "",
        [string]$ImageTag = ""
    )

    if (-not (Test-Path ".env.example")) {
        Write-Fail "no .env.example in $((Get-Location).Path) - run from the checkout root."
    }

    # --- Profile validation, BEFORE anything is written ---------------------
    # Both profiles are resolved and validated here rather than inside the
    # .env-creation branch below: a rejected value used to fail AFTER
    # `Copy-Item .env.example .env`, so the stand kept a full, profile-less
    # .env - and the corrective re-run then took the "existing .env" path,
    # applied nothing and exited 0. Same order as scripts/selfhost-env.sh: a
    # typo must leave the directory exactly as it found it.
    #
    # One flag, two variables (same split as install.sh): the ADR-0015 names
    # are the deployment TYPE, `cloud` is the legacy egress switch.
    $deploymentProfile = ""
    $profileValue = "$ProfileName".Trim().ToLowerInvariant()
    if (@("perimeter", "demo", "dev", "local") -contains $profileValue) {
        $deploymentProfile = $profileValue
        $profileValue = "perimeter"
    }
    if (-not $profileValue) { $profileValue = $env:GOOSAR_DELIVERY_PROFILE }
    if (-not $profileValue) { $profileValue = "perimeter" }
    if (@("cloud", "perimeter") -notcontains $profileValue) {
        # The server treats an unknown profile as a hard startup error, so
        # catch the typo here instead of shipping a crash loop.
        Write-Fail "GOOSAR_DELIVERY_PROFILE must be 'cloud' or 'perimeter' (got '$profileValue')."
    }

    # Trim and lowercase only - the same normalization the server's parser
    # applies (strings.TrimSpace + strings.ToLower). Inner whitespace stays a
    # typo, so 'de mo' is refused rather than read as 'demo'.
    if (-not $deploymentProfile) { $deploymentProfile = $env:GOOSAR_DEPLOYMENT_PROFILE }
    $deploymentProfile = "$deploymentProfile".Trim().ToLowerInvariant()
    if ($deploymentProfile -and (@("perimeter", "demo", "dev", "local") -notcontains $deploymentProfile)) {
        Write-Fail "GOOSAR_DEPLOYMENT_PROFILE must be 'perimeter', 'demo', 'dev' or 'local' (got '$deploymentProfile')."
    }

    if (Test-Path ".env") {
        # A profile is only ever applied while .env is being created. Saying so
        # is the difference between "nothing to do" and an operator who walks
        # away believing the stand just switched profiles.
        if ($deploymentProfile) {
            Write-Warn ".env already exists - GOOSAR_DEPLOYMENT_PROFILE=$deploymentProfile was NOT applied. Edit GOOSAR_DEPLOYMENT_PROFILE (and the rest of the profile's variables) in .env and restart the backend."
        }
        # Never modified, except the opt-in PUBLIC_HOST derivation below.
        $current = Get-EnvFileValue -Path ".env" -Name "GOOSAR_IMAGE_TAG" -Default ""
        $latest = Get-LatestReleaseTag
        if ($current -match '^v[0-9]' -and $latest -and $current -ne $latest) {
            try {
                if ([System.Version]$latest.TrimStart('v') -gt [System.Version]$current.TrimStart('v')) {
                    Write-Warn ".env pins GOOSAR_IMAGE_TAG=$current but the newest release tag here is $latest. Edit .env and restart the stack."
                }
            } catch {}
        }
    } else {
        Write-Info "Creating .env from .env.example..."
        Copy-Item ".env.example" ".env"

        $jwt = New-RandomHex 32
        $pgpass = New-RandomHex 24
        $vcsKey = New-RandomBase64 32
        $mcpKey = New-RandomBase64 32
        # Grafana admin password for the optional monitoring overlay (#396):
        # generated unconditionally, because the overlay refuses to start on an
        # empty value months later.
        $grafanaPass = New-RandomHex 24

        $lines = New-Object System.Collections.Generic.List[string]
        foreach ($line in @(Read-EnvLines ".env")) {
            $updated =
                if ($line.StartsWith("JWT_SECRET=")) { "JWT_SECRET=$jwt" }
                elseif ($line.StartsWith("POSTGRES_PASSWORD=")) { "POSTGRES_PASSWORD=$pgpass" }
                elseif ($line -match '^(DATABASE_URL=postgres://[^:]+:)[^@]*(@.*)$') { $Matches[1] + $pgpass + $Matches[2] }
                elseif ($line.StartsWith("GOOSAR_VCS_SECRET_KEY=")) { "GOOSAR_VCS_SECRET_KEY=$vcsKey" }
                elseif ($line.StartsWith("GOOSAR_MCP_SECRET_KEY=")) { "GOOSAR_MCP_SECRET_KEY=$mcpKey" }
                elseif ($line.StartsWith("GRAFANA_ADMIN_PASSWORD=")) { "GRAFANA_ADMIN_PASSWORD=$grafanaPass" }
                else { $line }
            $lines.Add($updated)
        }
        Write-EnvLines -Path ".env" -Lines $lines.ToArray()

        Write-Ok "Generated random JWT_SECRET, POSTGRES_PASSWORD, GOOSAR_VCS_SECRET_KEY, GOOSAR_MCP_SECRET_KEY, and GRAFANA_ADMIN_PASSWORD"
        Write-Host "    Back GOOSAR_VCS_SECRET_KEY and GOOSAR_MCP_SECRET_KEY up with the database: losing them loses every stored integration credential."

        # Delivery profile (#428). Self-hosting IS the on-prem product, so a
        # fresh .env is pinned to `perimeter` unless the operator asks for cloud
        # behavior explicitly. See SELF_HOSTING.md - the delivery profile and egress section.
        # Both profile values were resolved and validated above, before the
        # first byte was written.
        Set-EnvKv -Path ".env" -Name "GOOSAR_DELIVERY_PROFILE" -Value $profileValue
        Write-Info "Set GOOSAR_DELIVERY_PROFILE=$profileValue"

        # Deployment profile (ADR-0015, #530): the variable set of the named
        # profile, mirroring scripts/selfhost-env.sh. POSTGRES_SSLMODE is
        # deliberately left to the operator there and here — see the comment
        # in selfhost-env.sh.
        if ($deploymentProfile) {
            Set-EnvKv -Path ".env" -Name "GOOSAR_DEPLOYMENT_PROFILE" -Value $deploymentProfile
            Set-EnvKv -Path ".env" -Name "GOOSAR_ROLE_WORKSPACES" -Value "auto"
            Set-EnvKv -Path ".env" -Name "GOOSAR_DOWNLOAD_GITHUB_RELEASES" -Value "off"
            Set-EnvKv -Path ".env" -Name "GOOSAR_PROVISIONING_STORE" -Value "local"
            if ($deploymentProfile -eq "perimeter") {
                Set-EnvKv -Path ".env" -Name "ALLOW_SIGNUP" -Value "false"
                Set-EnvKv -Path ".env" -Name "GOOSAR_EXTERNAL_IMAGES" -Value "block"
                Write-Host "    Registration is closed. Set ALLOWED_EMAIL_DOMAINS (or ALLOWED_EMAILS) before"
                Write-Host "    the first start, or NOBODY can sign in - including you. GOOSAR_DEPLOYMENT_ADMIN_EMAILS"
                Write-Host "    grants the administrator role to an account that already exists; it does not create one."
            } else {
                Set-EnvKv -Path ".env" -Name "ALLOW_SIGNUP" -Value "true"
                if ($deploymentProfile -eq "dev") {
                    Set-EnvKv -Path ".env" -Name "RATE_LIMIT_API" -Value "6000"
                    Set-EnvKv -Path ".env" -Name "RATE_LIMIT_AUTH" -Value "100"
                }
                Write-Host "    Role workspaces are provisioned CLOSED while registration is open with no"
                Write-Host "    allowlist. Open the roles you want joinable in the UI, or set ALLOWED_EMAIL_DOMAINS."
            }
            Write-Info "Applied deployment profile $deploymentProfile (ADR-0015)"
        }

        # Same precedence as install.sh ("${GOOSAR_IMAGE_TAG:-$pin}"): an
        # explicit env var wins over the ref this installer happens to check out.
        $tagValue = $env:GOOSAR_IMAGE_TAG
        if (-not $tagValue) { $tagValue = $ImageTag }
        if (-not $tagValue) { $tagValue = Get-LatestReleaseTag }
        if ($tagValue) {
            Set-EnvKv -Path ".env" -Name "GOOSAR_IMAGE_TAG" -Value $tagValue
            Write-Info "Pinned GOOSAR_IMAGE_TAG=$tagValue"
        } else {
            Write-Info "No release tag found in this clone; images default to :latest"
        }
    }

    Update-PublicOrigins -Path ".env"
}

# One PUBLIC_HOST derives the public-origin variables that otherwise have to be
# set by hand — forgetting any of them ends in a silent WS 403. A variable is
# only derived while it still holds its stock localhost/template default; an
# explicit operator value always wins. GOOSAR_TRUSTED_PROXIES is a CIDR list,
# not derivable from a hostname, and stays manual.
function Update-PublicOrigins {
    [CmdletBinding()]
    param([string]$Path = ".env")

    $publicHost = Get-EnvFileValue -Path $Path -Name "PUBLIC_HOST" -Default ""
    if (-not $publicHost) { return }

    $origin = if ($publicHost -match '^https?://') { $publicHost } else { "https://$publicHost" }
    $origin = $origin.TrimEnd('/')

    $derived = @()
    foreach ($key in @("FRONTEND_ORIGIN", "GOOSAR_APP_URL", "GOOSAR_PUBLIC_URL", "CORS_ALLOWED_ORIGINS")) {
        $current = Get-EnvFileValue -Path $Path -Name $key -Default ""
        # A comma list (CORS with several origins) is always an explicit
        # operator value, even when it starts with a localhost origin.
        if ($current -like "*,*") { continue }
        if ($current -eq "" -or $current -like '*${*' -or $current -like 'http://localhost*') {
            Set-EnvKv -Path $Path -Name $key -Value $origin
            $derived += $key
        }
    }
    if ($derived.Count -gt 0) {
        Write-Info "Derived $($derived -join ' ') = $origin from PUBLIC_HOST (explicit values win)"
    }
}

# Wrapper around Initialize-SelfHostEnv for the --with-server path, mirroring
# bootstrap_env in scripts/install.sh.
function Initialize-ServerEnv {
    [CmdletBinding()]
    param([string]$ServerRef)

    $hadEnv = Test-Path ".env"
    # Pin GOOSAR_IMAGE_TAG to the version this installer actually installs; a
    # non-release ref (main, a branch) has no tag to pin.
    $pin = if ($ServerRef -match '^v[0-9]') { $ServerRef } else { "" }

    Initialize-SelfHostEnv -ProfileName $DeliveryProfile -ImageTag $pin

    if ($hadEnv) {
        Write-Ok "Using existing .env (secrets and profile left untouched)"
        # The profile is only written when .env is created, so on a re-run
        # -DeliveryProfile is a no-op. Say so: an operator who passes
        # `-DeliveryProfile cloud` expecting an egress change must not walk away
        # believing the deployment switched.
        # A deployment-profile name is already reported by Initialize-SelfHostEnv
        # (which also fires for the GOOSAR_DEPLOYMENT_PROFILE variable); this
        # covers the remaining spelling, `cloud`.
        if ($DeliveryProfile -and (@("perimeter", "demo", "dev", "local") -notcontains "$DeliveryProfile".Trim().ToLowerInvariant())) {
            Write-Warn "-DeliveryProfile $DeliveryProfile ignored: .env already exists. Edit GOOSAR_DEPLOYMENT_PROFILE (or GOOSAR_DELIVERY_PROFILE for cloud) in $InstallDir\.env and restart the backend."
        }
    }
}

# Write the -ResendKey / -Smtp* / -Llm* values into .env before the stack
# starts, so no extra edit + restart is needed. Twin of apply_env_overrides.
function Invoke-EnvOverrides {
    [CmdletBinding()]
    param([string]$Path = ".env")

    $map = [ordered]@{
        RESEND_API_KEY       = $ResendKey
        SMTP_HOST            = $SmtpHost
        SMTP_PORT            = $SmtpPort
        SMTP_USERNAME        = $SmtpUser
        SMTP_PASSWORD        = $SmtpPassword
        SMTP_FROM_EMAIL      = $SmtpFrom
        SMTP_TLS             = $SmtpTls
        GOOSAR_LLM_API_KEY  = $LlmKey
        GOOSAR_LLM_BASE_URL = $LlmBaseUrl
    }

    $applied = $false
    foreach ($key in $map.Keys) {
        if ($map[$key]) {
            Set-EnvKv -Path $Path -Name $key -Value $map[$key]
            $applied = $true
        }
    }

    if ($LlmKey -and -not $LlmBaseUrl -and
        -not (Get-EnvFileValue -Path $Path -Name "GOOSAR_LLM_BASE_URL" -Default "")) {
        Write-Warn "-LlmKey set without -LlmBaseUrl: the server requires an explicit GOOSAR_LLM_BASE_URL for the LLM layer to activate."
    }
    # The server refuses to start with SMTP_HOST and no sender address (#429).
    if ($SmtpHost -and -not $SmtpFrom -and
        -not (Get-EnvFileValue -Path $Path -Name "SMTP_FROM_EMAIL" -Default "")) {
        Write-Warn "-SmtpHost set without -SmtpFrom: the server refuses to start without a sender address (SMTP_FROM_EMAIL)."
    }

    if ($applied) {
        Write-Ok "Applied installer flags to $Path"
    }
}

function Get-EnvFileValue {
    param(
        [string]$Path,
        [string]$Name,
        [string]$Default
    )

    if (-not (Test-Path $Path)) {
        return $Default
    }

    $prefix = "$Name="
    $line = Read-EnvLines $Path |
        Where-Object { $_.StartsWith($prefix) } |
        Select-Object -Last 1
    if (-not $line) {
        return $Default
    }

    $value = $line.Substring($prefix.Length).Trim().Trim('"').Trim("'")
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $Default
    }
    return $value
}

# ALLOW_SIGNUP=false with no allowlist is a stand nobody can sign into - the
# operator included, since the login flow is the only account-creation path.
# `-Profile perimeter` writes exactly that, and the preset's own message scrolls
# past behind `docker pull` output, so the check is repeated in the final
# summary where the operator actually looks. Twin of
# warn_signup_closed_without_allowlist in scripts/install.sh: the condition is
# the fact "registration closed, no list", not the profile name.
function Write-SignupClosedWithoutAllowlistWarning {
    [CmdletBinding()]
    param([string]$Path = ".env")

    if (-not (Test-Path $Path)) { return }
    if ((Get-EnvFileValue -Path $Path -Name "ALLOW_SIGNUP" -Default "true") -ne "false") { return }
    if (Get-EnvFileValue -Path $Path -Name "ALLOWED_EMAIL_DOMAINS" -Default "") { return }
    if (Get-EnvFileValue -Path $Path -Name "ALLOWED_EMAILS" -Default "") { return }

    Write-Warn "Registration is CLOSED (ALLOW_SIGNUP=false) and no allowlist is set: nobody can sign in yet."
    Write-Host "     Set ALLOWED_EMAIL_DOMAINS (or ALLOWED_EMAILS) in $Path and restart the backend:"
    Write-Host "        cd $InstallDir; docker compose -f docker-compose.selfhost.yml up -d"
    Write-Host "     GOOSAR_DEPLOYMENT_ADMIN_EMAILS does NOT substitute: it grants the admin role to"
    Write-Host "     an account that already exists, it never creates one."
}

function Get-SelfHostBackendPort {
    foreach ($name in @("BACKEND_PORT", "API_PORT", "SERVER_PORT", "PORT")) {
        $value = Get-EnvFileValue -Path (Join-Path $InstallDir ".env") -Name $name -Default ""
        if (-not [string]::IsNullOrWhiteSpace($value)) {
            return $value
        }
    }
    return "8081"
}

function Get-SelfHostFrontendPort {
    return Get-EnvFileValue -Path (Join-Path $InstallDir ".env") -Name "FRONTEND_PORT" -Default "3001"
}

function Get-LatestVersion {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/adanman/goosar/releases/latest" -ErrorAction Stop
        return $release.tag_name
    } catch {
        return $null
    }
}

function Get-SelfHostRef {
    if ($env:GOOSAR_SELFHOST_REF) {
        return $env:GOOSAR_SELFHOST_REF
    }

    $latest = Get-LatestVersion
    if ($latest) {
        return $latest
    }

    return "main"
}

function Checkout-ServerRef {
    param([string]$Ref)

    if ($Ref -eq "main") {
        git fetch origin main --depth 1 2>$null
        git checkout --force main 2>$null
        git reset --hard origin/main 2>$null
        return
    }

    git fetch origin --tags --force 2>$null
    $tagRef = "refs/tags/$Ref"
    git show-ref --verify --quiet $tagRef 2>$null
    if ($LASTEXITCODE -eq 0) {
        git checkout --force $Ref 2>$null
        return
    }

    git fetch origin $Ref --depth 1 2>$null
    git checkout --force $Ref 2>$null
}

function Pull-OfficialSelfHostImages {
    docker compose -f docker-compose.selfhost.yml pull
    if ($LASTEXITCODE -eq 0) {
        return
    }

    Write-Host ""
    Write-Warn "Official images for the selected self-host channel are not published yet."
    Write-Host "This can happen before the first GHCR release is available."
    Write-Host "From $InstallDir, build from source instead:"
    Write-Host "  docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build"
    exit 1
}

function Convert-ToCliArch {
    param([object]$Value)

    if ($null -eq $Value) {
        return $null
    }

    $normalized = "$Value".Trim().ToUpperInvariant()
    switch ($normalized) {
        "9"      { return "amd64" }
        "AMD64"  { return "amd64" }
        "X64"    { return "amd64" }
        "X86_64" { return "amd64" }
        "12"     { return "arm64" }
        "ARM64"  { return "arm64" }
        "AARCH64" { return "arm64" }
        default  { return $null }
    }
}

function Get-WindowsCliArch {
    $signals = @()
    $nativeArchSignalFound = $false

    # Prefer the native processor architecture over the current PowerShell
    # process architecture. This keeps Windows on ARM from being misdetected
    # when PowerShell is running through x64/x86 emulation.
    try {
        if (Get-Command Get-CimInstance -ErrorAction SilentlyContinue) {
            $processorArch = Get-CimInstance -ClassName Win32_Processor -ErrorAction Stop |
                Select-Object -First 1 -ExpandProperty Architecture
            $signals += [pscustomobject]@{ Source = "Win32_Processor.Architecture"; Value = $processorArch }
            $nativeArchSignalFound = $true
        }
    } catch {}

    try {
        if (-not $nativeArchSignalFound -and (Get-Command Get-WmiObject -ErrorAction SilentlyContinue)) {
            $processorArch = Get-WmiObject -Class Win32_Processor -ErrorAction Stop |
                Select-Object -First 1 -ExpandProperty Architecture
            $signals += [pscustomobject]@{ Source = "Win32_Processor.Architecture"; Value = $processorArch }
            $nativeArchSignalFound = $true
        }
    } catch {}

    try {
        $signals += [pscustomobject]@{
            Source = "RuntimeInformation.OSArchitecture"
            Value = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
        }
    } catch {}

    $signals += [pscustomobject]@{ Source = "PROCESSOR_ARCHITEW6432"; Value = $env:PROCESSOR_ARCHITEW6432 }
    $signals += [pscustomobject]@{ Source = "PROCESSOR_ARCHITECTURE"; Value = $env:PROCESSOR_ARCHITECTURE }

    foreach ($signal in $signals) {
        $arch = Convert-ToCliArch $signal.Value
        if ($arch) {
            return $arch
        }
    }

    $details = ($signals |
        Where-Object { $null -ne $_.Value -and "$($_.Value)".Trim() -ne "" } |
        ForEach-Object { "$($_.Source)=$($_.Value)" }) -join ", "
    if (-not $details) {
        $details = "no architecture signals available"
    }

    Write-Fail "Unsupported Windows architecture ($details). Only x64 and ARM64 are supported."
}

function Get-InstalledCliVersion {
    try {
        $firstLine = goosar version 2>$null | Select-Object -First 1
        if ("$firstLine" -match '\b(v?\d+(?:\.\d+)+)\b') {
            $version = $Matches[1]
            if ($version -notlike 'v*') {
                $version = "v$version"
            }
            return $version
        }
    } catch {}

    return $null
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
function Install-CliBinary {
    Write-Info "Installing Goosar CLI from GitHub Releases..."

    if (-not [Environment]::Is64BitOperatingSystem) {
        Write-Fail "Goosar requires a 64-bit Windows installation."
    }

    $arch = Get-WindowsCliArch

    $latest = Get-LatestVersion
    if (-not $latest) {
        Write-Fail "Could not determine latest release. Check your network connection."
    }

    $version = $latest.TrimStart('v')
    $url = "https://github.com/adanman/goosar/releases/download/$latest/goosar-cli-$version-windows-$arch.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "goosar-install"

    if (Test-Path $tmpDir) { Remove-Item $tmpDir -Recurse -Force }
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    Write-Info "Downloading $url ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmpDir "goosar.zip") -UseBasicParsing
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Failed to download CLI binary: $_"
    }

    # Verify SHA256 checksum
    $checksumUrl = "https://github.com/adanman/goosar/releases/download/$latest/checksums.txt"
    try {
        $checksums = Invoke-WebRequest -Uri $checksumUrl -UseBasicParsing -ErrorAction Stop
        $checksumContent = if ($checksums.Content -is [byte[]]) {
            [System.Text.Encoding]::UTF8.GetString($checksums.Content)
        } else {
            [string]$checksums.Content
        }
        $zipFile = Join-Path $tmpDir "goosar.zip"
        $actualHash = (Get-FileHash -Path $zipFile -Algorithm SHA256).Hash.ToLower()
        $releaseAsset = "goosar-cli-$version-windows-$arch.zip"
        $legacyAsset = "goosar_windows_$arch.zip"
        $expectedLine = ($checksumContent -split "`r?`n") |
            Where-Object {
                $_ -match [regex]::Escape($releaseAsset) -or
                $_ -match [regex]::Escape($legacyAsset)
            } |
            Select-Object -First 1
        if ($expectedLine) {
            $expectedHash = ($expectedLine -split "\s+")[0].ToLower()
            if ($actualHash -ne $expectedHash) {
                Remove-Item $tmpDir -Recurse -Force
                Write-Fail "Checksum verification failed. Expected: $expectedHash, Got: $actualHash"
            }
            Write-Ok "Checksum verified"
        } else {
            Remove-Item $tmpDir -Recurse -Force
            Write-Fail "checksums.txt has no entry for $releaseAsset - aborting: the download cannot be verified."
        }
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Could not download checksums.txt - aborting: the download cannot be verified. $_"
    }

    Expand-Archive -Path (Join-Path $tmpDir "goosar.zip") -DestinationPath $tmpDir -Force

    $binDir = Join-Path $UserHome ".goosar\bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    }

    $exeSrc = Join-Path $tmpDir "goosar.exe"
    if (-not (Test-Path $exeSrc)) {
        $exeSrc = Get-ChildItem -Path $tmpDir -Filter "goosar.exe" -Recurse | Select-Object -First 1 -ExpandProperty FullName
    }
    if (-not $exeSrc -or -not (Test-Path $exeSrc)) {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "goosar.exe not found in downloaded archive."
    }

    Copy-Item $exeSrc (Join-Path $binDir "goosar.exe") -Force
    Remove-Item $tmpDir -Recurse -Force

    Add-ToUserPath $binDir
    Write-Ok "Goosar CLI installed to $binDir\goosar.exe"
}

function Add-ToUserPath {
    param([string]$Dir)
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -and $currentPath.Split(";") -contains $Dir) {
        return
    }
    $newPath = if ($currentPath) { "$currentPath;$Dir" } else { $Dir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    # Also update current session
    if ($env:Path -notlike "*$Dir*") {
        $env:Path = "$Dir;$env:Path"
    }
    Write-Info "Added $Dir to user PATH (restart your terminal for other sessions to pick it up)."
}

function Install-Cli {
    if (Test-CommandExists "goosar") {
        $currentVer = Get-InstalledCliVersion
        $latestVer = Get-LatestVersion

        $currentCmp = if ($currentVer) { $currentVer -replace '^v','' } else { $null }
        $latestCmp = if ($latestVer) { $latestVer -replace '^v','' } else { $null }

        $isUpToDate = $currentCmp -and -not $latestCmp
        if (-not $isUpToDate) {
            try {
                $isUpToDate = $currentCmp -and $latestCmp -and ([System.Version]$currentCmp -ge [System.Version]$latestCmp)
            } catch {
                $isUpToDate = $currentCmp -and $latestCmp -and ($currentCmp -eq $latestCmp)
            }
        }

        if ($isUpToDate) {
            Write-Ok "Goosar CLI is up to date ($currentVer)"
            return
        }

        Write-Info "Goosar CLI $currentVer installed, latest is $latestVer - upgrading..."
        Install-CliBinary

        $newVer = Get-InstalledCliVersion
        Write-Ok "Goosar CLI upgraded ($currentVer -> $newVer)"
        return
    }

    Install-CliBinary

    if (-not (Test-CommandExists "goosar")) {
        Write-Fail "CLI installed but 'goosar' not found on PATH. Restart your terminal and try again."
    }
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
function Test-Docker {
    if (-not (Test-CommandExists "docker")) {
        Write-Fail @"
Docker is not installed. Goosar self-hosting requires Docker and Docker Compose.

Install Docker Desktop for Windows:
  https://docs.docker.com/desktop/install/windows-install/

After installing Docker, re-run this script with -WithServer.
"@
    }

    # A native command's non-zero exit never throws, so check $LASTEXITCODE:
    # a try/catch here would silently accept a stopped Docker Desktop.
    docker info 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Docker is installed but not running. Please start Docker Desktop and re-run this script."
    }

    Write-Ok "Docker is available"
}

# ---------------------------------------------------------------------------
# Server setup (self-host / local)
# ---------------------------------------------------------------------------
function Install-Server {
    Write-Info "Setting up Goosar server..."
    $serverRef = Get-SelfHostRef
    Write-Info "Using self-host assets from $serverRef..."

    if (Test-Path (Join-Path $InstallDir ".git")) {
        Write-Info "Updating existing installation at $InstallDir..."
        Write-Warn "Any local changes in $InstallDir will be overwritten."
    } else {
        Write-Info "Cloning Goosar repository..."
        if (-not (Test-CommandExists "git")) {
            Write-Fail "Git is not installed. Please install git and re-run."
        }
        if (Test-Path $InstallDir) {
            Write-Warn "Removing incomplete installation at $InstallDir..."
            Remove-Item $InstallDir -Recurse -Force
        }
        $parentDir = Split-Path $InstallDir -Parent
        if (-not (Test-Path $parentDir)) {
            New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
        }
        git clone --depth 1 $RepoUrl $InstallDir
    }

    Push-Location $InstallDir
    Checkout-ServerRef $serverRef
    Write-Ok "Repository ready at $InstallDir ($serverRef)"

    Initialize-ServerEnv -ServerRef $serverRef
    Invoke-EnvOverrides -Path ".env"

    Write-Info "Pulling official Goosar images..."
    Pull-OfficialSelfHostImages
    Write-Info "Starting Goosar services (this may take a few minutes on first run)..."
    docker compose -f docker-compose.selfhost.yml up -d

    Write-Info "Waiting for backend to be ready..."
    $backendPort = Get-SelfHostBackendPort
    $ready = $false
    for ($i = 1; $i -le 45; $i++) {
        try {
            $null = Invoke-WebRequest -Uri "http://localhost:$backendPort/health" -UseBasicParsing -TimeoutSec 2
            $ready = $true
            break
        } catch {
            Start-Sleep -Seconds 2
        }
    }

    if ($ready) {
        Write-Ok "Goosar server is running"
    } else {
        Write-Warn "Server is still starting. Check logs with:"
        Write-Host "  cd $InstallDir; docker compose -f docker-compose.selfhost.yml logs"
    }

    Pop-Location
}


# ---------------------------------------------------------------------------
# Main: Default mode (cloud)
# ---------------------------------------------------------------------------
function Start-DefaultInstall {
    Write-Host ""
    Write-Host "  Goosar - Installer" -ForegroundColor White
    Write-Host ""

    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Goosar CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Next: configure your environment"
    Write-Host ""
    Write-Host "     goosar setup               " -NoNewline; Write-Host "# Connect to Goosar Cloud (goosar.ru)" -ForegroundColor DarkGray
    Write-Host "     goosar setup self-host      " -NoNewline; Write-Host "# Connect to a self-hosted server" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Self-hosting? Install the server first:"
    Write-Host "     powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -WithServer"
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Main: Local mode (self-host)
# ---------------------------------------------------------------------------
function Start-LocalInstall {
    Write-Host ""
    Write-Host "  Goosar - Self-Host Installer" -ForegroundColor White
    Write-Host "  Provisioning server infrastructure + installing CLI"
    Write-Host ""

    Test-Docker
    Install-Server
    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Goosar server is running and CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    $frontendPort = Get-SelfHostFrontendPort
    $backendPort = Get-SelfHostBackendPort
    Write-Host "  Frontend:  http://localhost:$frontendPort"
    Write-Host "  Backend:   http://localhost:$backendPort"
    Write-Host "  Server at: $InstallDir"
    Write-Host ""
    Write-Host "  Next: configure your CLI to connect"
    Write-Host ""
    Write-Host "     goosar setup self-host  " -NoNewline; Write-Host "# Configure + authenticate + start daemon" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Delivery profile: $(Get-EnvFileValue -Path (Join-Path $InstallDir '.env') -Name 'GOOSAR_DELIVERY_PROFILE' -Default 'perimeter')   Image tag: $(Get-EnvFileValue -Path (Join-Path $InstallDir '.env') -Name 'GOOSAR_IMAGE_TAG' -Default 'latest')"
    Write-Host ""
    Write-Host "  Login: open the frontend, enter your email, then print the code from the"
    Write-Host "  backend logs (that log path needs APP_ENV=development in .env - the stack"
    Write-Host "  defaults to APP_ENV=production, where a server with no mail transport"
    Write-Host "  refuses sign-in). Pass -ResendKey <key> or -SmtpHost/-SmtpFrom to this"
    Write-Host "  installer, or set RESEND_API_KEY / SMTP_HOST in .env, to email codes."
    Write-Host ""
    Write-Host "  To stop all services:"
    Write-Host "     powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Stop"
    Write-Host ""
    Write-SignupClosedWithoutAllowlistWarning -Path (Join-Path $InstallDir ".env")
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
function Start-Stop {
    Write-Host ""
    Write-Info "Stopping Goosar services..."

    if (Test-Path $InstallDir) {
        Push-Location $InstallDir
        if (Test-Path "docker-compose.selfhost.yml") {
            docker compose -f docker-compose.selfhost.yml down
            Write-Ok "Docker services stopped"
        } else {
            Write-Warn "No docker-compose.selfhost.yml found at $InstallDir"
        }
        Pop-Location
    } else {
        Write-Warn "No Goosar installation found at $InstallDir"
    }

    if (Test-CommandExists "goosar") {
        try {
            goosar daemon stop 2>$null
            Write-Ok "Daemon stopped"
        } catch {}
    }

    Write-Host ""
}

# ---------------------------------------------------------------------------
# Upgrade: move an existing -WithServer installation to another image tag
# ---------------------------------------------------------------------------
# The compose stack runs whatever GOOSAR_IMAGE_TAG .env pins, so an upgrade is:
# repoint the pin, pull, `up -d`. Migrations run on backend startup and are NOT
# reversible, which is why the rollback recipe pairs the old tag with a restore
# of the pre-upgrade database dump.
function Start-Upgrade {
    Write-Host ""
    Write-Host "  Goosar - Self-Host Upgrade" -ForegroundColor White
    Write-Host ""

    if (-not (Test-Path (Join-Path $InstallDir ".git"))) {
        Write-Fail "No Goosar installation found at $InstallDir. Install one first: install.ps1 -WithServer"
    }
    Test-Docker
    Push-Location $InstallDir
    try {
        if (-not (Test-Path ".env")) {
            Write-Fail "$InstallDir\.env is missing - this installation was not provisioned by -WithServer."
        }

        $previous = Get-EnvFileValue -Path ".env" -Name "GOOSAR_IMAGE_TAG" -Default "latest"

        Write-Info "Fetching release tags..."
        git fetch origin --tags --force 2>$null

        $target = $Tag
        if (-not $target) {
            $target = Get-LatestReleaseTag
            if (-not $target) {
                Write-Fail "No release tags found in $InstallDir. Pass an explicit tag: -Upgrade -Tag vX.Y.Z"
            }
        } else {
            # Checkout-ServerRef swallows a failed checkout, so a typo'd -Tag
            # would leave the working tree on the old release while .env gets
            # pinned to a tag that does not exist. Refuse before anything is
            # written.
            git show-ref --verify --quiet "refs/tags/$target" 2>$null
            if ($LASTEXITCODE -ne 0) {
                Write-Fail "No such release tag: $target."
            }
        }

        if ($target -eq $previous) {
            Write-Ok "Already on $previous - nothing to upgrade."
            return
        }

        Write-Info "Upgrading $previous -> $target"
        # Take the compose/self-host assets of the target release too: compose
        # file, migrations and scripts must match the images being started.
        Checkout-ServerRef $target
        Set-EnvKv -Path ".env" -Name "GOOSAR_IMAGE_TAG" -Value $target

        Write-Info "Pulling images for $target..."
        Pull-OfficialSelfHostImages
        Write-Info "Starting the upgraded stack..."
        docker compose -f docker-compose.selfhost.yml up -d

        Write-Host ""
        Write-Ok "Upgraded to $target (was $previous)"
        Write-Host ""
        Write-Host "  Back up first, always: bash scripts/backup.sh - migrations run on backend"
        Write-Host "  startup and are one-way."
        Write-Host ""
        Write-Host "  Rollback recipe (from $InstallDir):"
        Write-Host "     git checkout --force $previous"
        Write-Host "     set GOOSAR_IMAGE_TAG=$previous in .env"
        Write-Host "     docker compose -f docker-compose.selfhost.yml pull; docker compose -f docker-compose.selfhost.yml up -d"
        Write-Host ""
        Write-Host "  Images roll back; the database does not. If $target applied a migration,"
        Write-Host "  the older backend will refuse to start against the newer schema. In that"
        Write-Host "  case restore the pre-upgrade dump as well (scripts/restore.sh) - everything"
        Write-Host "  written since the upgrade is lost with it."
        Write-Host ""
    } finally {
        Pop-Location
    }
}

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
function Show-Usage {
    Write-Host "Usage: install.ps1 [-WithServer | -Upgrade | -Stop] [options]"
    Write-Host ""
    Write-Host "  (default)      Install / upgrade the Goosar CLI"
    Write-Host "  -WithServer    Install CLI + provision a self-host server (Docker)"
    Write-Host "  -Upgrade       Move an existing self-host installation to a newer image tag"
    Write-Host "                 (newest release tag unless -Tag is given)"
    Write-Host "  -Stop          Stop a self-hosted installation"
    Write-Host ""
    Write-Host "Options for -Upgrade:"
    Write-Host "  -Tag <vX.Y.Z>            Target release tag (default: newest in the clone)"
    Write-Host ""
    Write-Host "Options for -WithServer (written into the server .env):"
    Write-Host "  -DeliveryProfile <p>     Deployment profile (ADR-0015), also spelled"
    Write-Host "  (alias -Profile)         -Profile, written as GOOSAR_DEPLOYMENT_PROFILE"
    Write-Host "                           with the rest of that profile's variable set:"
    Write-Host "                             perimeter  customer network (default): signup"
    Write-Host "                                        closed, external images blocked"
    Write-Host "                             demo       preview stand: signup open"
    Write-Host "                             dev        development stand"
    Write-Host "                             local      developer machine"
    Write-Host "                           Legacy value: cloud - sets only"
    Write-Host "                           GOOSAR_DELIVERY_PROFILE=cloud. See"
    Write-Host "                           SELF_HOSTING.md - deployment profiles section."
    Write-Host "  -ResendKey <key>         RESEND_API_KEY - email login codes instead of logs"
    Write-Host "  -SmtpHost <host>         SMTP_HOST - own relay instead of Resend"
    Write-Host "  -SmtpPort <port>         SMTP_PORT (default 25)"
    Write-Host "  -SmtpUser <user>         SMTP_USERNAME (omit for an unauthenticated relay)"
    Write-Host "  -SmtpPassword <pass>     SMTP_PASSWORD"
    Write-Host "  -SmtpFrom <address>      SMTP_FROM_EMAIL - required with -SmtpHost"
    Write-Host "  -SmtpTls <mode>          SMTP_TLS: starttls (default) or implicit"
    Write-Host "  -LlmKey <key>            GOOSAR_LLM_API_KEY - enable the server LLM layer"
    Write-Host "  -LlmBaseUrl <url>        GOOSAR_LLM_BASE_URL - required with -LlmKey"
    Write-Host ""
    Write-Host "Environment variables:"
    Write-Host "  GOOSAR_INSTALL_DIR   Self-host server install directory"
    Write-Host "  GOOSAR_SELFHOST_REF  Git ref to check out for self-host assets"
    Write-Host ""
    Write-Host "After installation, run 'goosar setup' to configure your environment."
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
# Dot-sourcing ($MyInvocation.InvocationName -eq '.') loads the functions
# without running anything, which is how scripts/install.test.ps1 exercises the
# .env bootstrap without Docker or the network.
if ($MyInvocation.InvocationName -ne '.') {
    if ($Help) { Show-Usage }
    elseif ($Stop) { Start-Stop }
    elseif ($Upgrade) { Start-Upgrade }
    elseif ($WithServer) { Start-LocalInstall }
    else { Start-DefaultInstall }
}
