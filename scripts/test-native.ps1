$ErrorActionPreference = "Stop"
go test -v -count=1 -timeout=45s ./internal/instrumentation/native -run TestNativeProducer
if ($LASTEXITCODE -ne 0) { throw 'Native producer integration failed' }
go test -v -count=5 -timeout=45s ./internal/instrumentation/native -run '^TestNativeProducerGeneratedProbesEndToEnd$'
if ($LASTEXITCODE -ne 0) { throw 'Repeated native probe startup/capture/retirement failed' }
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
go test -v -count=3 -timeout=45s ./internal/instrumentation/native -run '^TestNativeProducerInstallsHooksInOwnedProcess$'
if ($LASTEXITCODE -ne 0) { throw 'Repeated owned-process hook installation failed' }
$hookArchive = Join-Path $dir 'installed-hook-metrics.ndjson'
$hookWorkload = Join-Path (Split-Path $env:AP_NATIVE_LAB) 'APHookLab.exe'
& (Join-Path $dir 'native-lab.exe') --install-hooks --workload $hookWorkload --threads 3 --iterations 10 | Set-Content -Encoding utf8 $hookArchive
if ($LASTEXITCODE -ne 0) { throw 'Owned-process hook controller executable failed' }
$last = Get-Content $hookArchive | Select-Object -Last 1 | ConvertFrom-Json
$rows = @($last.metrics.procedures)
if ($last.metrics.incomplete -or $last.metrics.open_call_count -ne 0 -or $rows.Count -ne 3) { throw 'Installed-hook capture incomplete' }
foreach ($row in $rows) { if ($row.procedure -ne 'APHookSum' -or $row.call_count -ne 10) { throw 'Installed-hook count/selection invalid' } }
$view = & (Join-Path $dir 'viewer.exe') --instrumentation --replay $hookArchive
if ($LASTEXITCODE -ne 0 -or !($view -join "`n").Contains('APHookSum')) { throw 'Installed-hook metrics did not reach viewer' }
Write-Host 'PASS: Go controller -> native startup installation -> actual hooked calls -> metrics -> independent viewer; original code verified after removal'
$launchTarget = Join-Path (Split-Path $env:AP_NATIVE_LAB) 'APLaunchTarget.exe'
$launchSymbols = [IO.Path]::ChangeExtension($launchTarget, '.tds')
if (Test-Path $launchSymbols) { Remove-Item $launchSymbols }
go run ./experiments/tdsfixture $launchTarget
if ($LASTEXITCODE -ne 0) { throw 'Owned native TD32 fixture generation failed' }
go build -o (Join-Path $dir 'instrument.exe') ./cmd/instrument
if ($LASTEXITCODE -ne 0) { throw 'Instrument command build failed' }
$launchArchive = Join-Path $dir 'unmodified-target-metrics.ndjson'
$nativeDll = Join-Path (Split-Path $env:AP_NATIVE_LAB) 'AntiPessimizerNative.dll'
& (Join-Path $dir 'instrument.exe') --image $launchTarget --dll $nativeDll --unit LabWorkload --procedure Lab.Hotspot --arg=--threads --arg=3 --arg=--iterations --arg=10 --output $launchArchive
if ($LASTEXITCODE -ne 0) { throw 'Unmodified target instrumentation command failed' }
$last = Get-Content $launchArchive | Select-Object -Last 1 | ConvertFrom-Json
$rows = @($last.metrics.procedures)
if ($last.metrics.incomplete -or $rows.Count -ne 4 -or @($rows | Where-Object { $_.call_count -eq 10 }).Count -ne 3 -or @($rows | Where-Object { $_.call_count -eq 1 }).Count -ne 1) { throw 'Unmodified target/finalizer counts invalid' }
$view = & (Join-Path $dir 'viewer.exe') --instrumentation --replay $launchArchive
if ($LASTEXITCODE -ne 0 -or !($view -join "`n").Contains('Lab.Hotspot') -or !($view -join "`n").Contains('LabWorkload')) { throw 'TD32-selected unmodified target metrics did not reach viewer' }
Write-Host 'PASS: TD32 selection -> unmodified owned process -> native installation -> worker/finalizer calls -> metrics -> independent viewer'

go test -v -count=3 -timeout=45s ./internal/instrumentation/native -run '^TestNativeProducerFramedCallsRecursionAndRealExceptionUnwind$'
if ($LASTEXITCODE -ne 0) { throw 'Repeated framed calls/recursion/Windows unwind failed' }

# The service owns a real unmodified target; viewers only control/observe it.
foreach ($name in @('collector')) {
    go build -o (Join-Path $dir "$name.exe") "./cmd/$name"
    if ($LASTEXITCODE -ne 0) { throw "Cannot build $name" }
}
Copy-Item -Force $nativeDll (Join-Path $dir 'AntiPessimizerNative.dll')
$servicePipe = "\\.\pipe\ap-instrument-e2e-$PID"
$collector = Start-Process -FilePath (Join-Path $dir 'collector.exe') -ArgumentList @('--serve', '--address', $servicePipe) -PassThru -NoNewWindow
try {
    $ready = $false
    for ($attempt = 0; $attempt -lt 100; $attempt++) {
        $null = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op status 2>$null
        if ($LASTEXITCODE -eq 0) { $ready = $true; break }
        Start-Sleep -Milliseconds 50
    }
    if (!$ready) { throw 'Instrumented service did not start' }
    $catalog = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op instrument-catalog --image $launchTarget | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or $catalog.catalog.total -ne 1) { throw 'Service TD32 catalog failed' }
    $procedureID = $catalog.catalog.procedures[0].id
    $null = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op instrument-start --image $launchTarget --image-id $catalog.catalog.image_id --catalog-id $catalog.catalog.catalog_id --procedure-ids $procedureID --arg=--threads --arg=3 --arg=--iterations --arg=10
    if ($LASTEXITCODE -ne 0) { throw 'Service instrumented launch failed' }
    $finished = $false
    for ($attempt = 0; $attempt -lt 100; $attempt++) {
        $state = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op latest | ConvertFrom-Json
        if ($LASTEXITCODE -ne 0) { throw 'Service latest failed' }
        if ($state.status.state -in @('stopped', 'error')) { $finished = $true; break }
        Start-Sleep -Milliseconds 50
    }
    if (!$finished -or $state.status.state -ne 'stopped' -or $state.metric.metrics.incomplete -or $state.status.mode -ne 'instrumentation') { throw "Service capture incomplete: $($state | ConvertTo-Json -Depth 8)" }
    $rows = @($state.metric.metrics.procedures)
    if ($rows.Count -ne 4 -or @($rows | Where-Object { $_.call_count -eq 10 }).Count -ne 3 -or @($rows | Where-Object { $_.call_count -eq 1 }).Count -ne 1) { throw 'Service counts differ from direct native capture' }
    $archive = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op instrument-history | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or @($archive.metrics).Count -eq 0) { throw 'Service retained no instrumented history' }
    $null = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op instrument-start --image $launchTarget --image-id $catalog.catalog.image_id --catalog-id $catalog.catalog.catalog_id --procedure-ids $procedureID --arg=--threads --arg=2 --arg=--iterations --arg=10000
    if ($LASTEXITCODE -ne 0) { throw 'Service restart failed' }
    $running = $false
    for ($attempt = 0; $attempt -lt 100; $attempt++) {
        $state = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op latest | ConvertFrom-Json
        if ($state.status.state -eq 'error') { throw 'Restarted capture failed' }
        if ($state.status.state -eq 'running') { $running = $true; break }
        Start-Sleep -Milliseconds 50
    }
    if (!$running) { throw 'Restarted capture did not become ready for Clear' }
    $cleared = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op instrument-clear | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw 'Native all-thread Clear failed through the service' }
    $state = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op latest | ConvertFrom-Json
    if ($state.metric.metrics.clear_epoch -ne 1) { throw 'Clear acknowledged before publishing its epoch' }
    $null = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op stop
    if ($LASTEXITCODE -ne 0) { throw 'Service Stop failed' }
    $state = & (Join-Path $dir 'viewer.exe') --address $servicePipe --op latest | ConvertFrom-Json
    if ($state.status.state -ne 'stopped' -or ($state.metric -and !$state.metric.metrics.incomplete)) { throw 'Stopped owned target advertised complete capture' }
    Write-Host 'PASS: service TD32 selection -> owned native launch -> call/time metrics -> independent viewer queries/history -> restart and explicit Stop'
} finally {
    if (!$collector.HasExited) { Stop-Process -Id $collector.Id -Force; $collector.WaitForExit(5000) | Out-Null }
}

go test -v -count=3 -timeout=45s ./internal/instrumentation/native -run '^TestNativeProducerClearBarrierPreservesOpenCallsAndRecursiveMetrics$'
if ($LASTEXITCODE -ne 0) { throw 'Repeated all-thread Clear boundaries failed' }
