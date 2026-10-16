# Create deploy/kubernetes/overlays/<overlay>/secrets.env with random passwords (git-ignored).
# Existing values are kept; missing keys are added.
#   .\deploy\kubernetes\init-secrets.ps1 lab|onprem|gke
param([Parameter(Mandatory = $true)][string]$Overlay)
$ErrorActionPreference = 'Stop'
$dir = Join-Path $PSScriptRoot "overlays\$Overlay"
if (-not (Test-Path $dir)) { throw "unknown overlay: $Overlay" }
$file = Join-Path $dir 'secrets.env'

function New-Secret([int]$Length) {
    $chars = [char[]]'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
    $bytes = New-Object byte[] $Length
    [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    -join ($bytes | ForEach-Object { $chars[$_ % $chars.Length] })
}

# Case-sensitive operators: case-insensitive matching breaks on "I" under the Turkish culture.
$existing = @()
if (Test-Path $file) { $existing = Get-Content $file | Where-Object { $_ -cmatch '^[A-Z_]+=' } | ForEach-Object { $_.Split('=')[0] } }
$oldMcp = if (Test-Path $file) { Get-Content $file | Where-Object { $_ -cmatch '^MCP_TOKEN=' } | ForEach-Object { $_.Substring(10) } | Select-Object -First 1 }
$wanted = [ordered]@{
    LOGSTASH_INGEST_PASSWORD = (New-Secret 24)
    ESM_PLATFORM_PASSWORD    = (New-Secret 24)
    # One analyst to start with; add name:token pairs per person (ADR-028). An older MCP_TOKEN is kept.
    ANALYST_TOKENS           = 'analyst:' + $(if ($oldMcp) { $oldMcp } else { New-Secret 48 })
    LLM_API_KEY              = ''
}
$add = foreach ($k in $wanted.Keys) { if ($existing -cnotcontains $k) { "$k=$($wanted[$k])" } }
if ($add) { [System.IO.File]::AppendAllText($file, (($add -join "`n") + "`n")) }
Write-Output "wrote $file (set LLM_API_KEY there when LLM_PROVIDER is not mock)"
