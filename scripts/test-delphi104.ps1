param([Parameter(Mandatory)][string]$Output)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repository = Split-Path $PSScriptRoot
$lab = Join-Path $repository 'testdata/delphi/ProfilerLab'
$image = Join-Path $lab 'bin/Symbols/ProfilerLab.exe'
if (!$env:BDS -or !(Test-Path (Join-Path $env:BDS 'bin/dcc64.exe'))) {
    throw 'A licensed Delphi 10.4 Win64 build and its RAD Studio environment are required.'
}
if (!(Test-Path $image)) { throw 'The Symbols build of ProfilerLab.exe is required.' }
$null = New-Item -ItemType Directory -Path $Output
$Output = (Resolve-Path $Output).Path
$cases = @(
    @{ Worker = 'A'; Symbol = 'CPUHotspotA'; Checksum = '269958183'; Phase = 'cpu_a' },
    @{ Worker = 'B'; Symbol = 'CPUHotspotB'; Checksum = '429183498'; Phase = 'cpu_b' }
)

function Read-LabReport([string]$Path) {
    $rows = @(Import-Csv -Path $Path -Delimiter "`t")
    $begin = @($rows | Where-Object event -eq 'session_begin')
    $end = @($rows | Where-Object event -eq 'session_end')
    $workers = @($rows | Where-Object event -eq 'work_end')
    if ($begin.Count -ne 1 -or $end.Count -ne 1 -or $workers.Count -ne 4) {
        throw "Invalid workload report: $Path"
    }
    $tags = @{}
    foreach ($item in $begin[0].detail.Split(';')) {
        $key, $value = $item.Split('=', 2)
        if (!$key -or !$value -or $tags.ContainsKey($key)) { throw "Invalid build details: $item" }
        $tags.Add($key, $value)
    }
    $compiler = $tags['compiler']
    if ($compiler -notmatch '^34([.,]0+)?$') { throw "Expected Delphi 10.4 CompilerVersion 34.0; got $compiler." }
    foreach ($case in $cases) {
        $matched = @($workers | Where-Object worker -eq $case.Worker)
        if ($matched.Count -ne 2 -or @($matched | Where-Object phase -eq $case.Phase).Count -ne 1 -or
            @($matched | Where-Object phase -eq 'parallel').Count -ne 1) {
            throw "Incorrect worker phases for $($case.Worker)"
        }
        foreach ($worker in $matched) {
            if ($worker.checksum -ne $case.Checksum -or $worker.iterations -ne '1000' -or $worker.pid -ne $begin[0].pid) {
                throw "Workload changed for $($case.Worker)"
            }
        }
    }
    $parallel = @($workers | Where-Object phase -eq 'parallel')
    if ($parallel[0].tid -eq $parallel[1].tid) { throw 'Delphi parallel workers share a TID.' }
    return @{ Tags = $tags; Workers = $workers; PID = [uint32]$begin[0].pid }
}

function Read-ExceptionReport([string]$Path) {
    $rows = @(Import-Csv -Path $Path -Delimiter "`t")
    if ($rows.Count -ne 2) { throw "Invalid Delphi exception report: $Path" }
    foreach ($index in 0, 1) {
        $worker, $checksum = if ($index -eq 0) { 'normal', '4' } else { 'nested', '2' }
        $row = $rows[$index]
        if ($row.event -ne 'exception_result' -or $row.phase -ne 'delphi_exception' -or
            $row.worker -ne $worker -or $row.iterations -ne '4' -or $row.checksum -ne $checksum -or
            [uint32]$row.pid -eq 0 -or [uint32]$row.tid -eq 0 -or
            $row.pid -ne $rows[0].pid -or $row.tid -ne $rows[0].tid) {
            throw "Unexpected Delphi exception result: $Path row $index"
        }
    }
    return @{ PID = [uint32]$rows[0].pid; TID = [uint32]$rows[0].tid }
}

$baseline = Read-LabReport (Join-Path $lab 'logs/smoke-Symbols.tsv')
$compilerPath = Join-Path $env:BDS 'bin/dcc64.exe'
$os = Get-CimInstance Win32_OperatingSystem
$manifest = [ordered]@{
    compiler_version = $baseline.Tags['compiler']
    compiler_build = (Get-Item $compilerPath).VersionInfo.FileVersion
    compiler_sha256 = (Get-FileHash $compilerPath -Algorithm SHA256).Hash.ToLowerInvariant()
    image_sha256 = (Get-FileHash $image -Algorithm SHA256).Hash.ToLowerInvariant()
    windows_edition = $os.Caption
    windows_version = $os.Version
}
Copy-Item (Join-Path $lab 'logs/smoke-Symbols.tsv') (Join-Path $Output 'baseline.tsv')

Push-Location $repository
try {
    $adapter = Join-Path $Output 'adapter'
    & ./scripts/build-native.ps1 -Output $adapter -AdapterOnly
    foreach ($tool in @('symresolve', 'instrument')) {
        go build -o (Join-Path $Output "$tool.exe") "./cmd/$tool"
        if ($LASTEXITCODE -ne 0) { throw "Cannot build $tool" }
    }

    $resolver = Join-Path $Output 'symresolve.exe'
    $inProbe = Join-Path $Output 'instrument.exe'
    $selectArgs = @('--image', $image, '--unit', 'LabWorkload')
    foreach ($case in $cases) {
        $selectArgs += @('--procedure', "LabWorkload.$($case.Symbol)")
    }
    $catalogText = & $inProbe @selectArgs --list --check
    if ($LASTEXITCODE -ne 0) { throw 'Delphi catalog preflight failed.' }
    $catalog = ($catalogText -join "`n") | ConvertFrom-Json
    if (@($catalog.procedures).Count -ne 2 -or @($catalog.checks).Count -ne 2 -or
        $catalog.image_id -ne "sha256:$($manifest.image_sha256)") {
        throw 'Wrong Delphi catalog identity or selection.'
    }
    $symbolFile = Get-Item $catalog.symbol_source
    $manifest['symbol_source'] = $symbolFile.Name
    $manifest['symbol_sha256'] = (Get-FileHash $symbolFile.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    $baseText = $baseline.Tags['module_base']
    $baseAddress = [Convert]::ToUInt64($baseText.Substring(2), 16)
    $resolveArgs = @('--source', '--image', $image, '--base', $baseText)
    foreach ($case in $cases) {
        $address = [Convert]::ToUInt64($baseline.Tags[$case.Symbol].Substring(2), 16)
        $match = @($catalog.procedures | Where-Object name -eq "LabWorkload.$($case.Symbol)")
        if ($match.Count -ne 1 -or $address -lt $baseAddress -or $address - $baseAddress -ne $match[0].rva) {
            throw "Wrong catalog RVA for $($case.Symbol)"
        }
        # A procedure can start with an unmapped compiler-generated prologue.
        for ($offset = 0; $offset -lt [Math]::Min(256, [int]$match[0].size); $offset++) {
            $resolveArgs += ('0x{0:x16}' -f [uint64]($address + $offset))
        }
    }
    $resolvedText = & $resolver @resolveArgs
    if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve the original Delphi TD32 symbols under ASLR.' }
    $resolved = ($resolvedText -join "`n") | ConvertFrom-Json
    $sources = foreach ($case in $cases) {
        $source = $resolved.resolutions | Where-Object {
            $_.in_image -and $_.location.procedure -eq "LabWorkload.$($case.Symbol)" -and
            $_.PSObject.Properties['source'] -and $_.source.line -gt 0 -and
            [IO.Path]::GetFileName($_.source.file) -eq 'LabWorkload.pas'
        } | Select-Object -First 1
        if (!$source) {
            throw "Missing TD32 procedure or source at $($case.Symbol)"
        }
        @{ procedure = $source.location.procedure; rva = $source.location.rva; line = $source.source.line }
    }
    $manifest['symbols'] = @($sources)
    $manifest | ConvertTo-Json -Depth 8 | Set-Content -Path (Join-Path $Output 'manifest.json') -Encoding utf8

    $unsupported = @($catalog.checks | Where-Object plan_supported -ne $true)
    if ($unsupported.Count -ne 0) {
        throw "Delphi procedures are outside the supported native subset: $($unsupported | ConvertTo-Json -Compress)"
    }
    $targetReport = Join-Path $Output 'instrumented.tsv'
    $archive = Join-Path $Output 'metrics.ndjson'
    & $inProbe @selectArgs --dll (Join-Path $adapter 'AntiPessimizerNative.dll') --output $archive `
        --arg=--iterations --arg=1000 --arg=--wait-ms --arg=0 --arg=--report "--arg=$targetReport"
    if ($LASTEXITCODE -ne 0) { throw 'The owned Delphi EXE did not complete its instrumented run.' }
    $target = Read-LabReport $targetReport
    $last = Get-Content $archive | Select-Object -Last 1 | ConvertFrom-Json
    if ($last.source -ne 'native-hook' -or $last.clock -ne 'qpc' -or $last.target.pid -ne $target.PID -or
        $last.metrics.clock_frequency_hz -ne $target.Tags['qpc_frequency'] -or
        $last.metrics.incomplete -or $last.metrics.open_call_count -ne 0 -or $last.metrics.abandoned_call_count -ne 0 -or
        ($last.PSObject.Properties['native_lost_event_count'] -and [uint64]$last.native_lost_event_count -ne 0) -or
        @($last.metrics.procedures).Count -ne 4) {
        throw 'The Delphi capture is incomplete or belongs to another process.'
    }
    foreach ($case in $cases) {
        $rows = @($last.metrics.procedures | Where-Object procedure -eq "LabWorkload.$($case.Symbol)")
        $expected = @($target.Workers | Where-Object worker -eq $case.Worker | ForEach-Object { [uint32]$_.tid } | Sort-Object)
        $observed = @($rows | ForEach-Object { [uint32]$_.thread_id } | Sort-Object)
        if ($rows.Count -ne 2 -or ($expected -join ',') -ne ($observed -join ',')) {
            throw "Delphi procedure/thread counts changed for $($case.Symbol)"
        }
        foreach ($row in $rows) {
            if ($row.call_count -ne 1 -or $row.unwound_call_count -ne 0 -or [uint64]$row.inclusive_ticks -eq 0 -or
                $row.exclusive_ticks -ne $row.inclusive_ticks -or
                $row.module_id -ne $catalog.image_id) {
                throw "Delphi calls or timing changed for $($case.Symbol)"
            }
        }
    }
    $exceptionBaseline = Join-Path $Output 'exception-baseline.tsv'
    & $image --exception-report $exceptionBaseline
    if ($LASTEXITCODE -ne 0) { throw 'The original Delphi EXE failed its nested exception run.' }
    $null = Read-ExceptionReport $exceptionBaseline

    $exceptionArgs = @('--image', $image, '--unit', 'LabWorkload',
        '--procedure', 'LabWorkload.RecursiveExceptionWork', '--procedure', 'LabWorkload.NestedExceptionWork')
    $exceptionCatalogText = & $inProbe @exceptionArgs --list --check
    if ($LASTEXITCODE -ne 0) { throw 'Delphi exception catalog preflight failed.' }
    $exceptionCatalog = ($exceptionCatalogText -join "`n") | ConvertFrom-Json
    if (@($exceptionCatalog.procedures).Count -ne 2 -or @($exceptionCatalog.checks).Count -ne 2 -or
        @($exceptionCatalog.checks | Where-Object plan_supported -ne $true).Count -ne 0 -or
        $exceptionCatalog.image_id -ne $catalog.image_id) {
        throw "Delphi exception procedures are outside the supported native subset: $($exceptionCatalog.checks | ConvertTo-Json -Compress)"
    }
    $exceptionReport = Join-Path $Output 'exception-instrumented.tsv'
    $exceptionArchive = Join-Path $Output 'exception-metrics.ndjson'
    & $inProbe @exceptionArgs --dll (Join-Path $adapter 'AntiPessimizerNative.dll') --output $exceptionArchive `
        --arg=--exception-report "--arg=$exceptionReport"
    if ($LASTEXITCODE -ne 0) { throw 'The instrumented Delphi EXE failed its nested exception run.' }
    $exceptionTarget = Read-ExceptionReport $exceptionReport
    $exceptionLast = Get-Content $exceptionArchive | Select-Object -Last 1 | ConvertFrom-Json
    if ($exceptionLast.source -ne 'native-hook' -or $exceptionLast.target.pid -ne $exceptionTarget.PID -or
        $exceptionLast.metrics.incomplete -or $exceptionLast.metrics.open_call_count -ne 0 -or
        $exceptionLast.metrics.abandoned_call_count -ne 0 -or
        ($exceptionLast.PSObject.Properties['native_lost_event_count'] -and [uint64]$exceptionLast.native_lost_event_count -ne 0) -or
        @($exceptionLast.metrics.procedures).Count -ne 2) {
        throw 'The Delphi exception capture is incomplete or belongs to another process.'
    }
    foreach ($case in @(
        @{ Name = 'LabWorkload.RecursiveExceptionWork'; Calls = 8; Unwinds = 4 },
        @{ Name = 'LabWorkload.NestedExceptionWork'; Calls = 1; Unwinds = 0 }
    )) {
        $row = @($exceptionLast.metrics.procedures | Where-Object procedure -eq $case.Name)
        if ($row.Count -ne 1 -or $row[0].thread_id -ne $exceptionTarget.TID -or
            $row[0].module_id -ne $catalog.image_id -or $row[0].call_count -ne $case.Calls -or
            $row[0].unwound_call_count -ne $case.Unwinds -or [uint64]$row[0].inclusive_ticks -eq 0 -or
            $row[0].exclusive_ticks -gt $row[0].inclusive_ticks) {
            throw "Delphi exception call counts or timing changed for $($case.Name)"
        }
    }
    Write-Host 'PASS: Delphi 10.4 owned EXE, original TD32 source, exact calls, nested typed catches and recursive unwind.'
} finally {
    Pop-Location
}
