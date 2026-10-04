$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$target = Join-Path $repositoryRoot '.cache/temporal'
New-Item -ItemType Directory -Path $target -Force | Out-Null
$archive = Join-Path $target 'temporal-1.9.1.zip'
Invoke-WebRequest -Uri 'https://github.com/temporalio/cli/releases/download/v1.9.1/temporal_cli_1.9.1_windows_amd64.zip' -OutFile $archive
$expected = 'babb65844835045c91fb98930fe10b81de28db80c8c609180473d2c0b18c7589'
if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) {
    throw 'Temporal archive checksum mismatch; do not execute it.'
}
Expand-Archive -LiteralPath $archive -DestinationPath $target -Force
Write-Output 'Temporal 1.9.1 installed and SHA-256 verified in .cache/temporal.'
