$ErrorActionPreference = "Stop"
$dir = Join-Path $env:RUNNER_TEMP ("antipessimizer-e2e-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $dir | Out-Null
foreach ($name in @("collector", "viewer", "instrument-replay")) {
    go build -o (Join-Path $dir "$name.exe") "./cmd/$name"
    if ($LASTEXITCODE -ne 0) { throw "Build failed: $name" }
}
go build -o (Join-Path $dir "cpulab.exe") ./experiments/cpulab
if ($LASTEXITCODE -ne 0) { throw "Workload build failed" }
go run ./experiments/tdsfixture (Join-Path $dir "cpulab.exe")
if ($LASTEXITCODE -ne 0) { throw "Cannot generate controlled TD32 fixture" }
$address = '\\.\pipe\AntiPessimizer-e2e-' + [guid]::NewGuid().ToString('N')
$viewer = Join-Path $dir "viewer.exe"
function Request([string[]] $Arguments) {
    $json = & $viewer --address $address @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Viewer request failed: $Arguments" }
    if ($json) { return ($json | ConvertFrom-Json) }
}
$collector = $null; $lab = $null
try {
    $collector = Start-Process (Join-Path $dir "collector.exe") -ArgumentList @('--serve','--address',$address) -PassThru -RedirectStandardError (Join-Path $dir 'collector.log')
    $ready = $false
    for ($i=0; $i -lt 30; $i++) {
        try { $null = Request @('--op','status'); $ready = $true; break } catch { Start-Sleep -Milliseconds 100 }
    }
    if (!$ready) { throw 'Collector did not start' }
    $lab = Start-Process (Join-Path $dir "cpulab.exe") -ArgumentList @('--duration','20s') -PassThru
    $null = Request @('--op','start','--pid',"$($lab.Id)",'--window-ms','250')
    $sampled = $false
    for ($i=0; $i -lt 60; $i++) {
        $latest = Request @('--op','latest')
        if ($latest.status.state -eq 'error') { throw $latest.status.error }
        $resolved = @($latest.snapshot.hotspots | Where-Object { $_.procedure -eq 'Lab.Hotspot' -and $_.unit -eq 'LabWorkload' -and $_.symbol_status -eq 'resolved' -and $_.sample_count -gt 0 })
        if ($resolved.Count -gt 0) { $sampled=$true; break }
        Start-Sleep -Milliseconds 200
    }
    if (!$sampled) { throw 'No resolved Lab.Hotspot samples reached the independent viewer' }
    if ($latest.snapshot.target.pid -ne $lab.Id) { throw 'Wrong process identity' }
    $null = Request @('--op','stop')
    $archive = Join-Path $dir 'session.ndjson'
    $null = Request @('--save',$archive,'--seconds','30')
    & $viewer --replay $archive | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Archive replay failed' }
    $comparison = & $viewer --replay $archive --compare $archive | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw 'Archive comparison failed' }
    if ($comparison.Before.Accepted -le 0) { throw 'Archive contains no samples' }
    foreach ($change in $comparison.Changes) { if ($change.DeltaPerSecond -ne 0) { throw 'Identical archives differ' } }
    # A second session must release ETW automatically when its owned target exits.
    $lab = Start-Process (Join-Path $dir "cpulab.exe") -ArgumentList @('--duration','3s') -PassThru
    $second = Request @('--op','start','--pid',"$($lab.Id)",'--window-ms','250')
    if ($second.status.session_id -eq $latest.status.session_id) { throw 'Session identity reused on restart' }
    $lab.WaitForExit()
    $stopped = $false
    for ($i=0; $i -lt 60; $i++) {
        $status = Request @('--op','status')
        if ($status.status.state -eq 'error') { throw $status.status.error }
        if ($status.status.state -eq 'stopped') { $stopped=$true; break }
        Start-Sleep -Milliseconds 100
    }
    if (!$stopped) { throw 'Collector did not finish after target exit' }
    $last = Request @('--op','latest')
    if ($last.snapshot.target.pid -ne $lab.Id -or $last.snapshot.sequence -lt 1) { throw 'Missing final snapshot after exit' }
    $null = Request @('--op','start','--demo')
    $null = Request @('--op','stop')
    Write-Host 'PASS: natural target exit drains ETW and permits a fresh session'
    Write-Host "PASS: Windows ETW -> TD32 Lab.Hotspot -> collector -> named pipe -> viewer -> archive -> comparison ($($comparison.Before.Accepted) samples)"
} finally {
    if ($lab -and !$lab.HasExited) { Stop-Process -Id $lab.Id -Force }
    if ($collector -and !$collector.HasExited) { Stop-Process -Id $collector.Id -Force }
}

# Instrumentation accounting is a separate explicit-event contract, not ETW data.
& (Join-Path $dir "instrument-replay.exe") --input ./testdata/instrumentation/nested.ndjson | Set-Content (Join-Path $dir 'instrumentation.json')
if ($LASTEXITCODE -ne 0) { throw 'Instrumentation event replay failed' }
$instrumented = Get-Content (Join-Path $dir 'instrumentation.json') -Raw | ConvertFrom-Json
$parent = @($instrumented.metrics.procedures | Where-Object { $_.procedure -eq 'Lab.Parent' })
if ($parent.Count -ne 1 -or $parent[0].call_count -ne 2 -or $parent[0].average_inclusive_ns -ne 20000000 -or $parent[0].average_exclusive_ns -ne 15000000) { throw 'Incorrect instrumentation calls/times/averages' }
Write-Host 'PASS: explicit entry/exit replay preserves call count, inclusive/exclusive totals and averages'

$metricPath = Join-Path $dir 'instrumentation.json'
$metricView = & $viewer --instrumentation --replay $metricPath --thread 7 --unit Lab
if ($LASTEXITCODE -ne 0) { throw 'Instrumented viewer replay failed' }
$displayed = $metricView -join "`n"
if (!$displayed.Contains('mode=instrumentation source=synthetic') -or !$displayed.Contains('calls=2 exclusive_ms=30.000000 average_exclusive_ms=15.000000 inclusive_ms=40.000000 average_inclusive_ms=20.000000')) { throw 'Call/time/average metrics did not reach the independent viewer' }
$filtered = & $viewer --instrumentation --replay $metricPath --thread 8
if ($LASTEXITCODE -ne 0 -or ($filtered -join "`n").Contains('tid=7')) { throw 'Instrumented thread filter failed' }
Write-Host 'PASS: instrument-replay.exe -> metrics archive -> viewer.exe preserves calls, totals, averages and thread filters'
