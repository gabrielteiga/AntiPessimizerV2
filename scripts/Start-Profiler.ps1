$ErrorActionPreference = "Stop"
$collector = Join-Path $PSScriptRoot "collector.exe"
$viewer = Join-Path $PSScriptRoot "viewer.exe"
# The collector owns ETW privileges; the viewer stays a separate user process.
& $viewer --op status 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) {
    Start-Process -FilePath $collector -ArgumentList "--serve" -Verb RunAs
}
Start-Process -FilePath $viewer -ArgumentList "--gui"
