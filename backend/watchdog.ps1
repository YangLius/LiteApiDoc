# LiteApiDoc watchdog: run exe in foreground, auto-restart on crash; capture stderr/stdout per run.
# Deploy: create data\watchdog.stop, Stop-Process liteapidoc, wait for this script to exit,
# replace exe, delete stop marker, relaunch this script.
$ErrorActionPreference = 'Continue'
Set-Location $PSScriptRoot
$exe = Join-Path $PSScriptRoot 'liteapidoc.exe'
$stopMarker = Join-Path $PSScriptRoot 'data\watchdog.stop'
Remove-Item $stopMarker -ErrorAction SilentlyContinue

while ($true) {
    if (Test-Path $stopMarker) { break }
    $stamp = Get-Date -Format 'yyyyMMdd_HHmmss'
    "[$(Get-Date -Format 'HH:mm:ss')] start liteapidoc (stderr -> data\stderr_$stamp.log)" | Out-File 'data\watchdog.log' -Append -Encoding utf8
    $cmd = "`"$exe`" -web 9900 -mock 9901 -db ./data/liteapidoc.db 1>>`"data\stdout.log`" 2>>`"data\stderr_$stamp.log`""
    cmd /c $cmd
    "[$(Get-Date -Format 'HH:mm:ss')] exit code=$LASTEXITCODE" | Out-File 'data\watchdog.log' -Append -Encoding utf8
    if (Test-Path $stopMarker) { break }
    Start-Sleep -Seconds 2
}
"[$(Get-Date -Format 'HH:mm:ss')] watchdog stopped" | Out-File 'data\watchdog.log' -Append -Encoding utf8
