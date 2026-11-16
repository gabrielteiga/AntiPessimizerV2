$ErrorActionPreference = "Stop"
$dir = Join-Path $env:RUNNER_TEMP ("antipessimizer-e2e-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $dir | Out-Null
foreach ($name in @("collector", "viewer")) {
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
        $resolved = @($latest.snapshot.hotspots | Where-Object { $_.procedure -eq 'Lab.Hotspot' -and $_.symbol_status -eq 'resolved' -and $_.sample_count -gt 0 })
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
    Write-Host "PASS: Windows ETW -> TD32 Lab.Hotspot -> collector -> named pipe -> viewer -> archive -> comparison ($($comparison.Before.Accepted) samples)"
} finally {
    if ($lab -and !$lab.HasExited) { Stop-Process -Id $lab.Id -Force }
    if ($collector -and !$collector.HasExited) { Stop-Process -Id $collector.Id -Force }
}
