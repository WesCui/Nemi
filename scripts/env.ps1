# Dot-source from a terminal in the repository root. No shell evaluation of .env values.
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$env:GOPATH = Join-Path $repositoryRoot '.cache/go'
$env:GOCACHE = Join-Path $repositoryRoot '.cache/go-build'
$env:GOTELEMETRY = 'off'
$env:NEXT_TELEMETRY_DISABLED = '1'
$envFile = Join-Path $repositoryRoot '.env'
if (Test-Path -LiteralPath $envFile) {
    foreach ($line in Get-Content -LiteralPath $envFile) {
        if ($line -match '^([A-Z][A-Z0-9_]*)=(.*)$') {
            [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
        }
    }
}
