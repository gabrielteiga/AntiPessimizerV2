param([string]$Output = "dist")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force $Output | Out-Null
foreach ($command in @("collector", "viewer", "td32dump", "symresolve", "instrument-replay")) {
    go build -trimpath -o (Join-Path $Output "$command.exe") "./cmd/$command"
    if ($LASTEXITCODE -ne 0) { throw "Build failed: $command" }
}
Copy-Item README.md (Join-Path $Output "README.md")
Copy-Item docs/live.md (Join-Path $Output "USAGE.md")
Copy-Item docs/instrumentation.md (Join-Path $Output "INSTRUMENTATION.md")
Copy-Item testdata/instrumentation/nested.ndjson (Join-Path $Output "instrumentation-demo-events.ndjson")
Copy-Item scripts/Start-Profiler.ps1 (Join-Path $Output "Start-Profiler.ps1")
'@echo off', 'powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-Profiler.ps1"' | Set-Content -Encoding ascii (Join-Path $Output "Start-Profiler.cmd")
$licenses = Join-Path $Output "licenses"
New-Item -ItemType Directory -Force $licenses | Out-Null
foreach ($module in @("github.com/Microsoft/go-winio", "golang.org/x/sys", "golang.org/x/arch")) {
    go mod download $module
    if ($LASTEXITCODE -ne 0) { throw "Cannot download dependency license: $module" }
    $moduleDir = go list -m -f '{{.Dir}}' $module
    if ($LASTEXITCODE -ne 0) { throw "Cannot locate license: $module" }
    Copy-Item (Join-Path $moduleDir "LICENSE") (Join-Path $licenses (($module -replace '/', '-') + '.txt'))
}
$goRoot = go env GOROOT
Copy-Item (Join-Path $goRoot "LICENSE") (Join-Path $licenses "Go.txt")
$commit = git rev-parse HEAD
$goVersion = go version
@("commit=$commit", $goVersion) | Set-Content (Join-Path $Output "BUILD.txt")
Get-ChildItem $Output -Filter *.exe | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $([IO.Path]::GetFileName($_.Path))" } | Set-Content (Join-Path $Output "SHA256SUMS.txt")
