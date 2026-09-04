param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("gateway", "official")]
    [string]$Mode
)

$ErrorActionPreference = "Stop"
$codexDir = Join-Path $env:USERPROFILE ".codex"
$configPath = Join-Path $codexDir "config.toml"
$profilePath = Join-Path $codexDir "personal-gateway.config.toml"
$stableBackupPath = Join-Path $codexDir "config.toml.personal-gateway-official.bak"

function Read-Utf8([string]$Path) {
    return [IO.File]::ReadAllText($Path, [Text.Encoding]::UTF8)
}

function Write-AtomicUtf8([string]$Path, [string]$Content) {
    $tempPath = "$Path.tmp-personal-gateway"
    $rollbackPath = "$Path.rollback-personal-gateway"
    [IO.File]::WriteAllText($tempPath, $Content, (New-Object Text.UTF8Encoding($false)))
    try {
        [IO.File]::Replace($tempPath, $Path, $rollbackPath, $true)
        Remove-Item -LiteralPath $rollbackPath -Force -ErrorAction SilentlyContinue
    }
    finally {
        Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    }
}

function Get-TopValue([string]$Content, [string]$Name, [string]$Fallback) {
    $head = ($Content -split "(?m)^\s*\[", 2)[0]
    $match = [regex]::Match($head, "(?m)^\s*" + [regex]::Escape($Name) + "\s*=\s*`"([^`"]+)`"\s*$")
    if ($match.Success) { return $match.Groups[1].Value }
    return $Fallback
}

function Set-TopValue([string]$Content, [string]$Name, [string]$Value) {
    $parts = [regex]::Split($Content, "(?m)(?=^\s*\[)", 2)
    $head = $parts[0]
    $pattern = "(?m)^\s*" + [regex]::Escape($Name) + "\s*=.*$"
    $line = $Name + " = `"" + $Value + "`""
    if ([regex]::IsMatch($head, $pattern)) {
        $head = [regex]::Replace($head, $pattern, $line, 1)
    }
    else {
        $head = $line + [Environment]::NewLine + $head
    }
    if ($parts.Count -eq 2) { return $head + $parts[1] }
    return $head
}

if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw "Codex config not found: $configPath"
}

$content = Read-Utf8 $configPath

if ($Mode -eq "gateway") {
    $userKey = [Environment]::GetEnvironmentVariable("CODEX_GATEWAY_API_KEY", "User")
    if ([string]::IsNullOrWhiteSpace($userKey)) {
        throw "CODEX_GATEWAY_API_KEY is missing from the user environment."
    }
    if (-not (Test-Path -LiteralPath $stableBackupPath)) {
        $legacyBackup = Get-ChildItem -LiteralPath $codexDir -Filter "config.toml.bak-personal-gateway-*" -File |
            Sort-Object LastWriteTime -Descending |
            Select-Object -First 1
        if ($legacyBackup) {
            $backupSource = Read-Utf8 $legacyBackup.FullName
        }
        elseif ((Get-TopValue $content "model_provider" "openai") -eq "openai") {
            $backupSource = $content
        }
        else {
            throw "Official config backup is missing; refusing to overwrite a non-OpenAI config."
        }
        [IO.File]::WriteAllText($stableBackupPath, $backupSource, (New-Object Text.UTF8Encoding($false)))
    }
    if (-not (Test-Path -LiteralPath $profilePath -PathType Leaf)) {
        throw "Gateway profile not found: $profilePath"
    }
    $profile = Read-Utf8 $profilePath
    $providerMatch = [regex]::Match($profile, "(?ms)^\[model_providers\.personal_gateway\]\s*.*\z")
    if (-not $providerMatch.Success) {
        throw "Gateway provider block is missing from $profilePath"
    }
    $providerBlock = $providerMatch.Value.Trim()
    $content = [regex]::Replace($content, "(?ms)^\[model_providers\.personal_gateway\]\s*.*?(?=^\[|\z)", "")
    $content = (Set-TopValue $content "model" "gpt-5.4").TrimEnd() + [Environment]::NewLine
    $content = (Set-TopValue $content "model_provider" "personal_gateway").TrimEnd() + [Environment]::NewLine
    $content += [Environment]::NewLine + $providerBlock + [Environment]::NewLine
    Write-AtomicUtf8 $configPath $content
    Write-Host "Codex Desktop is bound to Personal Gateway. Reopen the app to apply it."
    exit 0
}

$backupContent = $null
if (Test-Path -LiteralPath $stableBackupPath -PathType Leaf) {
    $backupContent = Read-Utf8 $stableBackupPath
}
else {
    $legacyBackup = Get-ChildItem -LiteralPath $codexDir -Filter "config.toml.bak-personal-gateway-*" -File |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if ($legacyBackup) { $backupContent = Read-Utf8 $legacyBackup.FullName }
}

$officialModel = if ($backupContent) { Get-TopValue $backupContent "model" "gpt-5.6-sol" } else { "gpt-5.6-sol" }
$officialProvider = if ($backupContent) { Get-TopValue $backupContent "model_provider" "openai" } else { "openai" }
$content = Set-TopValue $content "model" $officialModel
$content = Set-TopValue $content "model_provider" $officialProvider
Write-AtomicUtf8 $configPath $content
Write-Host "Codex Desktop is restored to the official OpenAI provider. Reopen the app to apply it."
