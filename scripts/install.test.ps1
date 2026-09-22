# Hermetic tests for scripts/install.ps1 — the PowerShell mirror of
# scripts/install.test.sh (#447).
#
# Nothing here touches Docker, the network or a real installation: install.ps1
# is dot-sourced (which skips its entry point) and only the .env bootstrap and
# the flag-override helpers are exercised, against a fixture checkout in a temp
# directory.
#
# Run:  pwsh -NoProfile -File scripts/install.test.ps1
# The bash wrapper scripts/install-ps1.test.sh runs this when pwsh exists and
# performs the static parity checks either way.

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $false

$RootDir = Split-Path $PSScriptRoot -Parent
$Failures = @()

# The bootstrap honors both of these; a runner that exports one would flip the
# assertions below.
$env:GOOSAR_IMAGE_TAG = $null
$env:GOOSAR_DELIVERY_PROFILE = $null

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-Equal {
    param($Expected, $Actual, [string]$Message)
    if ("$Expected" -ne "$Actual") { throw "$Message (want '$Expected', got '$Actual')" }
}

function Get-EnvVal {
    param([string]$Path, [string]$Name)
    $prefix = "$Name="
    $line = Get-Content -LiteralPath $Path | Where-Object { $_.StartsWith($prefix) } | Select-Object -Last 1
    if (-not $line) { return "" }
    return $line.Substring($prefix.Length)
}

# A fixture checkout: .env.example plus a git repo carrying one release tag, so
# the image pin has something to derive from.
function New-CheckoutFixture {
    $dir = Join-Path ([System.IO.Path]::GetTempPath()) ("goosar-ps1-test-" + [Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $dir | Out-Null
    Copy-Item (Join-Path $RootDir ".env.example") (Join-Path $dir ".env.example")
    git -C $dir init -q | Out-Null
    git -C $dir -c user.email=t@t -c user.name=t commit -q --allow-empty -m x | Out-Null
    git -C $dir tag v1.2.3 | Out-Null
    return $dir
}

function Invoke-Case {
    param([string]$Name, [scriptblock]$Body)
    $dir = New-CheckoutFixture
    Push-Location $dir
    try {
        & $Body $dir
        Write-Host "  ok   $Name"
    } catch {
        $script:Failures += "$Name : $_"
        Write-Host "  FAIL $Name : $_"
    } finally {
        Pop-Location
        Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# install.ps1 is dot-sourced at script scope so its functions land here; its
# entry point sees InvocationName '.' and runs nothing.
. (Join-Path $RootDir "scripts/install.ps1")

$FlagNames = @("DeliveryProfile", "Tag", "ResendKey", "SmtpHost", "SmtpPort", "SmtpUser",
               "SmtpPassword", "SmtpFrom", "SmtpTls", "LlmKey", "LlmBaseUrl")

# Resets every flag and applies the given ones, the way the bash tests set OPT_*
# before calling a helper.
function Set-Flags {
    param([hashtable]$Flags = @{})
    foreach ($name in $FlagNames) {
        Set-Variable -Name $name -Value "" -Scope Script
    }
    foreach ($name in $Flags.Keys) {
        Set-Variable -Name $name -Value $Flags[$name] -Scope Script
    }
}

Write-Host "install.ps1 tests"

# --- .env bootstrap (#447 / parity with #428) -------------------------------

Invoke-Case "bootstrap generates every secret and pins the installed tag" {
    param($dir)
    Set-Flags
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null

    foreach ($key in @("JWT_SECRET", "POSTGRES_PASSWORD", "GOOSAR_VCS_SECRET_KEY", "GOOSAR_MCP_SECRET_KEY", "GRAFANA_ADMIN_PASSWORD")) {
        Assert-True ((Get-EnvVal ".env" $key) -ne "") ".env bootstrap left $key empty"
    }
    Assert-Equal "v1.2.3" (Get-EnvVal ".env" "GOOSAR_IMAGE_TAG") "GOOSAR_IMAGE_TAG must pin the installed version, not :latest"
    Assert-Equal "perimeter" (Get-EnvVal ".env" "GOOSAR_DELIVERY_PROFILE") "GOOSAR_DELIVERY_PROFILE default"
}

# Encodings must match scripts/selfhost-env.sh byte for byte: the server's
# secretbox.ValidateKeyEnv requires exactly 32 base64-decoded bytes, and a hex
# key there is a startup error.
Invoke-Case "generated secrets use the same encodings as selfhost-env.sh" {
    param($dir)
    Set-Flags
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null

    Assert-True ((Get-EnvVal ".env" "JWT_SECRET") -match '^[0-9a-f]{64}$') "JWT_SECRET must be 32 random bytes as hex"
    Assert-True ((Get-EnvVal ".env" "POSTGRES_PASSWORD") -match '^[0-9a-f]{48}$') "POSTGRES_PASSWORD must be 24 random bytes as hex"
    Assert-True ((Get-EnvVal ".env" "GRAFANA_ADMIN_PASSWORD") -match '^[0-9a-f]{48}$') "GRAFANA_ADMIN_PASSWORD must be 24 random bytes as hex"
    foreach ($key in @("GOOSAR_VCS_SECRET_KEY", "GOOSAR_MCP_SECRET_KEY")) {
        $decoded = [Convert]::FromBase64String((Get-EnvVal ".env" $key))
        Assert-Equal 32 $decoded.Length "$key must decode to exactly 32 bytes"
    }
    # DATABASE_URL carries the generated password, not the .env.example default.
    $pg = Get-EnvVal ".env" "POSTGRES_PASSWORD"
    Assert-True ((Get-EnvVal ".env" "DATABASE_URL") -like "*:$pg@*") "DATABASE_URL was not repointed at the generated password"
}

# docker compose reads a BOM and a CR as part of the *value*: a BOM lands in the
# first key's name and a CR at the end of every value, so JWT_SECRET silently
# gains a trailing carriage return. This is the whole reason Write-EnvLines
# exists instead of Set-Content.
Invoke-Case ".env is written UTF-8 without BOM and with LF line endings" {
    param($dir)
    Set-Flags @{ ResendKey = "re_test123" }
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null
    Invoke-EnvOverrides -Path ".env" | Out-Null

    $bytes = [System.IO.File]::ReadAllBytes((Join-Path $dir ".env"))
    Assert-True (-not ($bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF)) ".env must not start with a UTF-8 BOM"
    Assert-True (-not ($bytes -contains 0x0D)) ".env must use LF line endings, not CRLF"
    # An em dash from .env.example survived the read/write round trip, so the
    # file really is UTF-8 and was not decoded through the ANSI codepage.
    $text = [System.Text.Encoding]::UTF8.GetString($bytes)
    Assert-True ($text.Contains([char]0x2014)) ".env.example comments must survive as UTF-8"
}

Invoke-Case "secrets differ between runs" {
    param($dir)
    Set-Flags
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null
    $first = Get-EnvVal ".env" "GOOSAR_MCP_SECRET_KEY"
    Remove-Item ".env" -Force
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null
    Assert-True ($first -ne (Get-EnvVal ".env" "GOOSAR_MCP_SECRET_KEY")) "secrets must be random per run"
}

Invoke-Case "an explicit GOOSAR_IMAGE_TAG wins over the installed ref" {
    param($dir)
    Set-Flags
    $env:GOOSAR_IMAGE_TAG = "v9.9.9"
    try {
        Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null
    } finally {
        $env:GOOSAR_IMAGE_TAG = $null
    }
    Assert-Equal "v9.9.9" (Get-EnvVal ".env" "GOOSAR_IMAGE_TAG") "install.sh honors GOOSAR_IMAGE_TAG over the pin; install.ps1 must too"
}

Invoke-Case "-DeliveryProfile cloud is honored and PUBLIC_HOST derives the origins" {
    param($dir)
    (Get-Content ".env.example") -replace '^PUBLIC_HOST=.*', 'PUBLIC_HOST=goosar.corp.example' | Set-Content ".env.example"
    Set-Flags @{ DeliveryProfile = "cloud" }
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null

    Assert-Equal "cloud" (Get-EnvVal ".env" "GOOSAR_DELIVERY_PROFILE") "-DeliveryProfile cloud not applied"
    foreach ($key in @("FRONTEND_ORIGIN", "GOOSAR_APP_URL", "GOOSAR_PUBLIC_URL", "CORS_ALLOWED_ORIGINS")) {
        Assert-Equal "https://goosar.corp.example" (Get-EnvVal ".env" $key) "PUBLIC_HOST derivation missed $key"
    }
}

Invoke-Case "PUBLIC_HOST derivation leaves explicit operator values alone" {
    param($dir)
    (Get-Content ".env.example") -replace '^PUBLIC_HOST=.*', 'PUBLIC_HOST=https://goosar.corp.example/' | Set-Content ".env.example"
    Set-Flags
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null
    Set-EnvKv -Path ".env" -Name "CORS_ALLOWED_ORIGINS" -Value "https://a.example,https://b.example"
    Set-EnvKv -Path ".env" -Name "FRONTEND_ORIGIN" -Value "https://kept.example"
    Update-PublicOrigins -Path ".env"

    Assert-Equal "https://a.example,https://b.example" (Get-EnvVal ".env" "CORS_ALLOWED_ORIGINS") "a comma list is an explicit value"
    Assert-Equal "https://kept.example" (Get-EnvVal ".env" "FRONTEND_ORIGIN") "an explicit origin must win"
    Assert-Equal "https://goosar.corp.example" (Get-EnvVal ".env" "GOOSAR_APP_URL") "a full-origin PUBLIC_HOST must lose its trailing slash"
}

# The never-overwrite rule: a re-run must not touch an existing .env, and a
# -DeliveryProfile that silently does nothing is the failure mode worth warning
# about (an operator expecting an egress change would walk away believing it).
Invoke-Case "a re-run never overwrites an existing .env and warns about the ignored profile" {
    param($dir)
    Set-Flags
    Initialize-ServerEnv -ServerRef "v1.2.3" | Out-Null
    $before = Get-Content ".env" -Raw

    Set-Variable -Name DeliveryProfile -Value "cloud" -Scope Script
    $warnings = (Initialize-ServerEnv -ServerRef "v1.2.3" 3>&1 | Out-String)

    Assert-Equal $before (Get-Content ".env" -Raw) "an existing .env must not be modified"
    Assert-Equal "perimeter" (Get-EnvVal ".env" "GOOSAR_DELIVERY_PROFILE") "existing profile must not be rewritten"
    Assert-True ($warnings -like "*-DeliveryProfile cloud ignored*") "a re-run with -DeliveryProfile must warn that it is a no-op"
}

Invoke-Case "an unknown delivery profile is refused" {
    param($dir)
    Set-Flags
    $failed = $false
    try {
        Initialize-SelfHostEnv -ProfileName "bogus" | Out-Null
    } catch {
        $failed = $true
    }
    Assert-True $failed "an unknown GOOSAR_DELIVERY_PROFILE must fail instead of shipping a crash loop"
}

# --- deployment profiles (ADR-0015, #530) -----------------------------------

Invoke-Case "a deployment profile writes its ADR-0015 variable set" {
    param($dir)
    Set-Flags
    Initialize-SelfHostEnv -ProfileName "demo" | Out-Null

    Assert-Equal "demo" (Get-EnvVal ".env" "GOOSAR_DEPLOYMENT_PROFILE") "the profile name must land in .env"
    Assert-Equal "true" (Get-EnvVal ".env" "ALLOW_SIGNUP") "demo keeps registration open"
    Assert-Equal "auto" (Get-EnvVal ".env" "GOOSAR_ROLE_WORKSPACES") "every profile provisions role workspaces"
    Assert-Equal "off" (Get-EnvVal ".env" "GOOSAR_DOWNLOAD_GITHUB_RELEASES") "no profile looks builds up on github.com"
    Assert-Equal "local" (Get-EnvVal ".env" "GOOSAR_PROVISIONING_STORE") "unset means the provisioning endpoints answer 503"
    # The ADR pins external images to the stock default outside perimeter.
    Assert-True ((Get-EnvVal ".env" "GOOSAR_EXTERNAL_IMAGES") -ne "block") "only perimeter blocks external images"
    # No stand type opts back into cloud egress.
    Assert-Equal "perimeter" (Get-EnvVal ".env" "GOOSAR_DELIVERY_PROFILE") "a deployment profile must not change the delivery profile"
}

Invoke-Case "the dev profile relaxes the rate limits and the others do not" {
    param($dir)
    Set-Flags
    Initialize-SelfHostEnv -ProfileName "dev" | Out-Null
    Assert-Equal "6000" (Get-EnvVal ".env" "RATE_LIMIT_API") "a load run on stock limits measures the limiter"
    Assert-Equal "100" (Get-EnvVal ".env" "RATE_LIMIT_AUTH") "a load run on stock limits measures the limiter"
}

Invoke-Case "the environment variable applies when -Profile is absent" {
    param($dir)
    Set-Flags
    $env:GOOSAR_DEPLOYMENT_PROFILE = "  Perimeter  "
    try {
        Initialize-SelfHostEnv | Out-Null
    } finally {
        Remove-Item Env:GOOSAR_DEPLOYMENT_PROFILE -ErrorAction SilentlyContinue
    }
    # Trimmed and lowercased, like the server's own parser.
    Assert-Equal "perimeter" (Get-EnvVal ".env" "GOOSAR_DEPLOYMENT_PROFILE") "an exported profile must not be discarded"
    Assert-Equal "false" (Get-EnvVal ".env" "ALLOW_SIGNUP") "the exported profile must apply its whole variable set"
}

Invoke-Case "the perimeter profile closes registration and blocks external images" {
    param($dir)
    Set-Flags
    Initialize-SelfHostEnv -ProfileName "perimeter" | Out-Null

    Assert-Equal "perimeter" (Get-EnvVal ".env" "GOOSAR_DEPLOYMENT_PROFILE") "the profile name must land in .env"
    Assert-Equal "false" (Get-EnvVal ".env" "ALLOW_SIGNUP") "a customer perimeter must not accept open signups"
    Assert-Equal "block" (Get-EnvVal ".env" "GOOSAR_EXTERNAL_IMAGES") "a customer perimeter blocks external images"
}

# A rejected profile used to fail AFTER Copy-Item, leaving a full, profile-less
# .env behind - and the corrective re-run then took the "existing .env" path,
# applied nothing and exited 0. Parity with selfhost-env.sh.
Invoke-Case "a rejected deployment profile leaves no .env behind" {
    param($dir)
    Set-Flags
    foreach ($bogus in @("bogus", "de mo")) {
        $env:GOOSAR_DEPLOYMENT_PROFILE = $bogus
        $failed = $false
        try {
            Initialize-SelfHostEnv | Out-Null
        } catch {
            $failed = $true
        } finally {
            Remove-Item Env:GOOSAR_DEPLOYMENT_PROFILE -ErrorAction SilentlyContinue
        }
        Assert-True $failed "GOOSAR_DEPLOYMENT_PROFILE='$bogus' must be refused (the server refuses it at startup)"
        Assert-True (-not (Test-Path ".env")) "a refused profile ('$bogus') must not leave a .env behind"
    }
}

# The corrective re-run after a typo must actually apply the profile, which it
# only can while .env is still absent.
Invoke-Case "the corrective re-run after a rejected profile applies the right one" {
    param($dir)
    Set-Flags
    $env:GOOSAR_DEPLOYMENT_PROFILE = "bogus"
    try { Initialize-SelfHostEnv | Out-Null } catch { } finally {
        Remove-Item Env:GOOSAR_DEPLOYMENT_PROFILE -ErrorAction SilentlyContinue
    }
    Initialize-SelfHostEnv -ProfileName "demo" | Out-Null
    Assert-Equal "demo" (Get-EnvVal ".env" "GOOSAR_DEPLOYMENT_PROFILE") "the re-run must apply the corrected profile"
}

# The profile is only written while .env is created; silence on a re-run leaves
# an operator believing the stand just switched profiles.
Invoke-Case "a re-run with a deployment profile warns that it was NOT applied" {
    param($dir)
    Set-Flags
    Initialize-SelfHostEnv -ProfileName "demo" | Out-Null
    $before = Get-Content ".env" -Raw

    $warnings = (Initialize-SelfHostEnv -ProfileName "perimeter" 3>&1 | Out-String)

    Assert-True ($warnings -like "*GOOSAR_DEPLOYMENT_PROFILE=perimeter was NOT applied*") "a re-run must say the profile was not applied"
    Assert-Equal $before (Get-Content ".env" -Raw) "an existing .env must not be modified"
    Assert-Equal "demo" (Get-EnvVal ".env" "GOOSAR_DEPLOYMENT_PROFILE") "the existing profile must survive the re-run"
}

# ALLOW_SIGNUP=false with no allowlist is a stand nobody - the operator
# included - can sign into. The preset's own message scrolls past behind
# `docker pull`, so the gate has to be in the final summary.
Invoke-Case "closed signup without an allowlist is gated in the summary" {
    param($dir)
    Set-Flags
    Initialize-SelfHostEnv -ProfileName "perimeter" | Out-Null

    # Write-Host lands on the information stream (6), Write-Warning on 3.
    $output = (Write-SignupClosedWithoutAllowlistWarning -Path ".env" 3>&1 6>&1 | Out-String)
    Assert-True ($output -like "*ALLOW_SIGNUP=false*") "closed signup with no allowlist was not gated"
    Assert-True ($output -like "*ALLOWED_EMAIL_DOMAINS*") "the gate must name the variable that opens the door"

    # With an allowlist the gate must stay quiet, or it is noise operators
    # learn to ignore.
    Set-EnvKv -Path ".env" -Name "ALLOWED_EMAIL_DOMAINS" -Value "example.test"
    $quiet = (Write-SignupClosedWithoutAllowlistWarning -Path ".env" 3>&1 6>&1 | Out-String)
    Assert-Equal "" ("$quiet".Trim()) "the gate fired even though an allowlist is set"

    # ...and the summary must actually call it: a helper nobody invokes is the
    # same silent success this test exists to prevent.
    $source = Get-Content (Join-Path $RootDir "scripts/install.ps1") -Raw
    $body = $source.Substring($source.IndexOf("function Start-LocalInstall"))
    Assert-True ($body -like "*Write-SignupClosedWithoutAllowlistWarning*") "Start-LocalInstall does not call the signup gate"
}

Invoke-Case "an older pin warns that the stand is behind" {
    param($dir)
    Set-Flags
    Initialize-ServerEnv -ServerRef "v1.0.0" | Out-Null
    $warnings = (Initialize-SelfHostEnv 3>&1 | Out-String)
    Assert-True ($warnings -like "*v1.2.3*") "a stale GOOSAR_IMAGE_TAG pin must warn"
}

# --- flag overrides (parity with apply_env_overrides) -----------------------

Invoke-Case "flag overrides write .env entries and append missing keys" {
    param($dir)
    Set-Flags @{
        ResendKey    = "re_test123"
        SmtpHost     = "smtp.example.com"
        SmtpPort     = "587"
        SmtpUser     = "mailer@example.com"
        SmtpPassword = 'p@ss&word#1'
        SmtpFrom     = "noreply@example.com"
        SmtpTls      = "starttls"
        LlmKey       = "sk-test"
        LlmBaseUrl   = 'https://llm.example.com/v1?a=1&b=2#frag'
    }
    Set-Content ".env" -Value "JWT_SECRET=abc"
    Invoke-EnvOverrides -Path ".env" | Out-Null

    Assert-Equal "abc" (Get-EnvVal ".env" "JWT_SECRET") "an unrelated key was clobbered"
    Assert-Equal "re_test123" (Get-EnvVal ".env" "RESEND_API_KEY") "RESEND_API_KEY not appended"
    Assert-Equal "smtp.example.com" (Get-EnvVal ".env" "SMTP_HOST") "SMTP_HOST not written"
    Assert-Equal "587" (Get-EnvVal ".env" "SMTP_PORT") "SMTP_PORT not written"
    Assert-Equal "mailer@example.com" (Get-EnvVal ".env" "SMTP_USERNAME") "SMTP_USERNAME not written"
    Assert-Equal 'p@ss&word#1' (Get-EnvVal ".env" "SMTP_PASSWORD") "SMTP_PASSWORD must land verbatim"
    Assert-Equal "noreply@example.com" (Get-EnvVal ".env" "SMTP_FROM_EMAIL") "SMTP_FROM_EMAIL not written"
    Assert-Equal "starttls" (Get-EnvVal ".env" "SMTP_TLS") "SMTP_TLS not written"
    Assert-Equal "sk-test" (Get-EnvVal ".env" "GOOSAR_LLM_API_KEY") "GOOSAR_LLM_API_KEY not written"
    Assert-Equal 'https://llm.example.com/v1?a=1&b=2#frag' (Get-EnvVal ".env" "GOOSAR_LLM_BASE_URL") "a URL with & and # must land verbatim"
}

Invoke-Case "unset flags write nothing" {
    param($dir)
    Set-Flags
    Set-Content ".env" -Value "JWT_SECRET=abc"
    Invoke-EnvOverrides -Path ".env" | Out-Null
    Assert-True ((Get-Content ".env" -Raw) -notlike "*GOOSAR_LLM*") "unset flags must not write LLM keys"
    Assert-True ((Get-Content ".env" -Raw) -notlike "*SMTP_HOST*") "unset flags must not write SMTP keys"
}

Invoke-Case "-LlmKey without -LlmBaseUrl warns" {
    param($dir)
    Set-Flags @{ LlmKey = "sk-test" }
    Set-Content ".env" -Value @("GOOSAR_LLM_API_KEY=", "GOOSAR_LLM_BASE_URL=")
    $warnings = (Invoke-EnvOverrides -Path ".env" 3>&1 | Out-String)
    Assert-True ($warnings -like "*LlmBaseUrl*") "expected a base-url warning"
}

Invoke-Case "-SmtpHost without -SmtpFrom warns" {
    param($dir)
    Set-Flags @{ SmtpHost = "smtp.example.com" }
    Set-Content ".env" -Value @("SMTP_HOST=", "SMTP_FROM_EMAIL=")
    $warnings = (Invoke-EnvOverrides -Path ".env" 3>&1 | Out-String)
    Assert-True ($warnings -like "*SmtpFrom*") "expected an smtp-from warning"
}

# --- usage ------------------------------------------------------------------

# Show-Usage writes through Write-Host, which no pipeline captures, so assert on
# the source the way install.test.sh guards run_upgrade's rollback recipe.
Invoke-Case "usage documents every override flag" {
    param($dir)
    $source = Get-Content (Join-Path $RootDir "scripts/install.ps1") -Raw
    $usage = $source.Substring($source.IndexOf("function Show-Usage"))
    foreach ($flag in @("WithServer", "Upgrade", "Stop", "Tag", "DeliveryProfile", "ResendKey",
                        "SmtpHost", "SmtpPort", "SmtpUser", "SmtpPassword", "SmtpFrom", "SmtpTls",
                        "LlmKey", "LlmBaseUrl")) {
        Assert-True ($usage -like "*-$flag*") "usage does not document -$flag"
    }
}

# The rollback recipe is the point of -Upgrade: images roll back, the database
# does not. Guard the text so it cannot be dropped silently.
Invoke-Case "upgrade prints the rollback recipe" {
    param($dir)
    $source = Get-Content (Join-Path $RootDir "scripts/install.ps1") -Raw
    $body = $source.Substring($source.IndexOf("function Start-Upgrade"))
    foreach ($needle in @("Rollback recipe", "scripts/restore.sh", "GOOSAR_IMAGE_TAG")) {
        Assert-True ($body -like "*$needle*") "Start-Upgrade does not mention '$needle'"
    }
}

if ($Failures.Count -gt 0) {
    Write-Host ""
    Write-Host "install.ps1 tests FAILED ($($Failures.Count)):"
    $Failures | ForEach-Object { Write-Host "  $_" }
    exit 1
}

Write-Host "install.ps1 tests passed"
