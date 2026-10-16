<#
.SYNOPSIS
  Build the GPO deployment share for Winlogbeat, Metricbeat, Heartbeat and Sysmon.

.DESCRIPTION
  Downloads the Beats MSI installers from artifacts.elastic.co (verified against the published
  SHA-512) and Sysmon from Sysinternals (verified by its Microsoft Authenticode signature), renders
  the configurations from integrations/sources with your Logstash address, and writes the GPO
  startup scripts with your share path. Nothing binary is stored in the repository.

  Copy the output folder to a file server, share it read-only (Domain Computers: Read) and add the
  .bat files as computer startup scripts (docs/integrations/windows-endpoints.md).

  With -CaCert the Beats send to Logstash over TLS: the CA is put on the share, the startup scripts
  copy it to C:\ProgramData\Elastic\Beats\esm-ca.crt, and the configurations verify Logstash
  against it. Logstash must run with BEATS_TLS=true and a certificate for -ElkServer
  (integrations/sources/README.md#tls-from-the-beats).

.EXAMPLE
  .\deploy\endpoints\windows\prepare-share.ps1 -ShareUnc \\fs01\elk -ElkServer 10.0.0.20 `
      -DomainController 10.0.0.10 -CaCert .\esm-ca.crt -OutputPath C:\elk
#>
param(
    [Parameter(Mandatory = $true)][string]$ShareUnc,          # how clients reach the share, e.g. \\fs01\elk
    [Parameter(Mandatory = $true)][string]$ElkServer,         # Logstash host or IP
    [string]$DomainController = '',                           # optional, monitored by Heartbeat (ICMP)
    [string]$ElasticVersion = '8.13.4',                       # must match the Elastic Stack version
    [string]$CaCert = '',                                     # optional: the stack's CA, turns on TLS to Logstash
    [string]$OutputPath = (Join-Path (Get-Location) 'elk-share')
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$sources = Join-Path $repo 'integrations\sources'

# String.Replace is ordinal: no culture-dependent matching (see the Turkish "I" problem).
function Write-Rendered([string]$Source, [string]$Target) {
    $text = [System.IO.File]::ReadAllText($Source)
    $text = $text.Replace('ELK_SERVER_IP', $ElkServer).Replace('FILESERVER_SHARE', $ShareUnc)
    if ($DomainController) { $text = $text.Replace('DC_IP', $DomainController) }
    if ($CaCert) {
        $text = $text.Replace("  ssl.enabled: false", "  ssl.enabled: true`n  ssl.certificate_authorities: [`"C:/ProgramData/Elastic/Beats/esm-ca.crt`"]")
    }
    $eol = if ($Target.EndsWith('.bat')) { "`r`n" } else { "`n" }
    $text = ($text -split "`r?`n") -join $eol
    [System.IO.File]::WriteAllText($Target, $text, (New-Object System.Text.UTF8Encoding($false)))
}

function Get-VerifiedMsi([string]$Beat, [string]$Target) {
    $base = "https://artifacts.elastic.co/downloads/beats/$Beat/$Beat-$ElasticVersion-windows-x86_64.msi"
    Write-Host "Downloading $Beat $ElasticVersion"
    Invoke-WebRequest -Uri $base -OutFile $Target -UseBasicParsing
    # Windows PowerShell 5.1 returns the .sha512 body (binary/octet-stream) as bytes, not text
    $body = (Invoke-WebRequest -Uri "$base.sha512" -UseBasicParsing).Content
    if ($body -is [byte[]]) { $body = [System.Text.Encoding]::ASCII.GetString($body) }
    $expected = ($body -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA512 -Path $Target).Hash
    if (-not $actual.Equals($expected, [System.StringComparison]::OrdinalIgnoreCase)) {
        Remove-Item $Target -Force
        throw "SHA-512 mismatch for $Beat MSI"
    }
}

if ($CaCert) {
    if (-not (Test-Path $CaCert)) { throw "CA certificate not found: $CaCert" }
    New-Item -ItemType Directory -Force -Path $OutputPath | Out-Null
    Copy-Item $CaCert (Join-Path $OutputPath 'esm-ca.crt') -Force
}

$beats = [ordered]@{
    winlogbeat = Join-Path $sources 'windows\winlogbeat.yml'
    metricbeat = Join-Path $sources 'beats-health\metricbeat.yml'
    heartbeat  = Join-Path $sources 'beats-health\heartbeat.yml'
}
foreach ($beat in $beats.Keys) {
    $dir = Join-Path $OutputPath $beat
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Get-VerifiedMsi $beat (Join-Path $dir "$beat.msi")
    Write-Rendered $beats[$beat] (Join-Path $dir "$beat.yml")
    Write-Rendered (Join-Path $PSScriptRoot "gpo\$beat.bat") (Join-Path $OutputPath "$beat.bat")
}

$sysmonDir = Join-Path $OutputPath 'sysmon'
New-Item -ItemType Directory -Force -Path $sysmonDir | Out-Null
$zip = Join-Path $env:TEMP 'Sysmon.zip'
Write-Host 'Downloading Sysmon'
Invoke-WebRequest -Uri 'https://download.sysinternals.com/files/Sysmon.zip' -OutFile $zip -UseBasicParsing
Expand-Archive -Path $zip -DestinationPath (Join-Path $env:TEMP 'sysmon-extract') -Force
Copy-Item (Join-Path $env:TEMP 'sysmon-extract\Sysmon64.exe') $sysmonDir -Force
$sig = Get-AuthenticodeSignature (Join-Path $sysmonDir 'Sysmon64.exe')
if ($sig.Status -ne 'Valid' -or -not $sig.SignerCertificate.Subject.Contains('O=Microsoft Corporation')) {
    throw "Sysmon64.exe signature is not a valid Microsoft signature ($($sig.Status))"
}
Copy-Item (Join-Path $sources 'windows\sysmon\sysmon.xml') $sysmonDir -Force
Write-Rendered (Join-Path $PSScriptRoot 'gpo\sysmon.bat') (Join-Path $OutputPath 'sysmon.bat')

if (-not $DomainController) {
    Write-Warning 'No -DomainController given: heartbeat.yml still contains the DC_IP placeholder.'
}
Write-Host "Share prepared in $OutputPath. Copy it to $ShareUnc and add the .bat files as GPO startup scripts."
