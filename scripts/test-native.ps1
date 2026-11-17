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
