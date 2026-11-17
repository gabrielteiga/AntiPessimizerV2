$ErrorActionPreference = "Stop"
go test -v -count=1 -timeout=45s ./internal/instrumentation/native -run TestNativeProducer
if ($LASTEXITCODE -ne 0) { throw 'Native producer integration failed' }
$dir = Join-Path $env:RUNNER_TEMP 'ap-native-e2e'
New-Item -ItemType Directory -Force $dir | Out-Null
foreach ($name in @('native-lab', 'viewer')) {
    go build -o (Join-Path $dir "$name.exe") "./cmd/$name"
    if ($LASTEXITCODE -ne 0) { throw "Cannot build $name" }
}
$archive = Join-Path $dir 'native-metrics.ndjson'
& (Join-Path $dir 'native-lab.exe') --workload $env:AP_NATIVE_LAB --threads 3 --iterations 10 | Set-Content -Encoding utf8 $archive
if ($LASTEXITCODE -ne 0) { throw 'Native controller executable failed' }
$last = Get-Content $archive | Select-Object -Last 1 | ConvertFrom-Json
if ($last.source -ne 'native-hook' -or $last.clock -ne 'qpc' -or $last.metrics.incomplete -or $last.metrics.open_call_count -ne 0) { throw 'Native metrics provenance/completeness invalid' }
foreach ($entry in @(@('APLabParent',10), @('APLabChild',20), @('APLabRecursive',40), @('APLabFloat',10))) {
    $rows = @($last.metrics.procedures | Where-Object { $_.procedure -eq $entry[0] })
    if ($rows.Count -ne 3) { throw "Native thread rows missing for $($entry[0])" }
    foreach ($row in $rows) { if ($row.call_count -ne $entry[1]) { throw "Wrong native call count for $($entry[0])" } }
}
$view = & (Join-Path $dir 'viewer.exe') --instrumentation --replay $archive
if ($LASTEXITCODE -ne 0 -or !($view -join "`n").Contains('mode=instrumentation source=native-hook clock=qpc') -or !($view -join "`n").Contains('APLabParent')) { throw 'Native metrics did not reach the independent viewer' }
Write-Host 'PASS: native C callbacks -> bounded rings -> local pipe -> Go accounting -> instrumented archive -> independent viewer; real QPC, exact counts and averages'
