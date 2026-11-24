param(
    [Parameter(Mandatory)][string]$Output,
    [Parameter(Mandatory)][ValidateRange(6, 7200)][int]$Seconds
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Read-EnduranceReport([string]$Path, [int]$Duration) {
    $rows = @(Import-Csv -Path $Path -Delimiter "`t")
    if ($rows.Count -ne $Duration + 2) { throw "Missing Delphi resource observations in $Path" }
    $samples = for ($i = 0; $i -lt $rows.Count; $i++) {
        $row = $rows[$i]
        $kind = if ($i -eq 0) { 'endurance_begin' } elseif ($i -eq $Duration + 1) { 'endurance_end' } else { 'endurance_sample' }
        $count = [uint64]([Math]::Min($i, $Duration) * 30)
        if ($row.event -ne $kind -or $row.phase -ne 'delphi_endurance' -or $row.worker -ne 'A' -or
            [uint64]$row.iterations -ne $count -or [uint64]$row.checksum -ne $count * 269958183 -or
            [uint32]$row.pid -eq 0 -or [uint32]$row.tid -eq 0 -or
            $row.pid -ne $rows[0].pid -or $row.tid -ne $rows[0].tid -or
            $row.detail -notmatch '^frequency=(\d+);call_ticks=(\d+);private_bytes=(\d+);working_set_bytes=(\d+);handles=(\d+)$') {
            throw "Invalid Delphi endurance observation in $Path at row $i"
        }
        [pscustomobject]@{
            qpc = [uint64]$row.qpc; calls = $count; frequency = [uint64]$Matches[1];
            call_ticks = [uint64]$Matches[2]; private_bytes = [uint64]$Matches[3];
            working_set_bytes = [uint64]$Matches[4]; handles = [uint64]$Matches[5]
        }
    }
    for ($i = 1; $i -lt $samples.Count; $i++) {
        if ($samples[$i].qpc -lt $samples[$i-1].qpc -or $samples[$i].call_ticks -lt $samples[$i-1].call_ticks -or
            $samples[$i].frequency -ne $samples[0].frequency) { throw "Delphi clocks went backwards in $Path" }
    }
    $first, $last = $samples[0], $samples[-1]
    if ($first.frequency -eq 0 -or $last.call_ticks -eq 0 -or $last.qpc -le $first.qpc -or
        $last.qpc - $first.qpc -lt ($Duration - 1.0 / 30) * $last.frequency) {
        throw "Invalid Delphi clock in $Path"
    }
    return @{
        pid = [uint32]$rows[0].pid; tid = [uint32]$rows[0].tid; first_qpc = $first.qpc; last_qpc = $last.qpc;
        calls = $last.calls; clock_hz = $last.frequency; call_ticks = $last.call_ticks;
        calls_per_second = $last.calls * [double]$last.frequency / ($last.qpc - $first.qpc);
        avg_call_ns = $last.call_ticks * 1e9 / $last.frequency / $last.calls;
        first_private_bytes = $first.private_bytes; final_private_bytes = $last.private_bytes;
        peak_private_bytes = ($samples | Measure-Object -Property private_bytes -Maximum).Maximum;
        first_handles = $first.handles; final_handles = $last.handles;
        peak_handles = ($samples | Measure-Object -Property handles -Maximum).Maximum
    }
}

$Output = (Resolve-Path $Output).Path
$repository = Split-Path $PSScriptRoot
$image = Join-Path $repository 'testdata/delphi/ProfilerLab/bin/Symbols/ProfilerLab.exe'
$instrument = Join-Path $Output 'instrument.exe'
$adapter = Join-Path $Output 'adapter/AntiPessimizerNative.dll'
$control = Join-Path $Output 'endurance-baseline.tsv'
$target = Join-Path $Output 'endurance-instrumented.tsv'
$archive = Join-Path $Output 'endurance-metrics.ndjson'
& $image --endurance-report $control --duration-sec $Seconds
if ($LASTEXITCODE -ne 0) { throw 'The original Delphi endurance run failed.' }
$baseline = Read-EnduranceReport $control $Seconds
& $instrument --image $image --unit LabWorkload --procedure LabWorkload.CPUHotspotA --dll $adapter --output $archive `
    --timeout "$($Seconds + 90)s" --arg=--endurance-report "--arg=$target" --arg=--duration-sec "--arg=$Seconds"
if ($LASTEXITCODE -ne 0) { throw 'The instrumented Delphi endurance run failed.' }
$observed = Read-EnduranceReport $target $Seconds

$previous = $observed.first_qpc
$last = $null
$liveSnapshots = 0
$maxGap = [uint64]0
Get-Content -Path $archive | ForEach-Object {
    $record = $_ | ConvertFrom-Json
    $tick = [uint64]$record.observed_at_ticks
    if ($last -and ($tick -lt [uint64]$last.observed_at_ticks -or $record.sequence -le $last.sequence)) {
        throw 'Delphi publication order changed.'
    }
    if ($record.metrics.incomplete -or ($record.PSObject.Properties['native_lost_event_count'] -and [uint64]$record.native_lost_event_count -ne 0)) {
        throw 'The Delphi endurance capture lost events.'
    }
    if ($tick -ge $observed.first_qpc -and $tick -le $observed.last_qpc) {
        $liveSnapshots++
        $maxGap = [Math]::Max($maxGap, $tick - $previous)
        $previous = $tick
    }
    $last = $record
}
$maxGap = [Math]::Max($maxGap, $observed.last_qpc - $previous)
if (!$last -or $last.source -ne 'native-hook' -or $last.clock -ne 'qpc' -or $last.target.pid -ne $observed.pid -or
    $last.metrics.clock_frequency_hz -ne $observed.clock_hz -or $last.metrics.open_call_count -ne 0 -or
    $last.metrics.abandoned_call_count -ne 0 -or @($last.metrics.procedures).Count -ne 1 -or $liveSnapshots -eq 0) {
    throw 'Delphi endurance metrics are missing or incomplete.'
}
$row = @($last.metrics.procedures)[0]
if ($row.procedure -ne 'LabWorkload.CPUHotspotA' -or $row.thread_id -ne $observed.tid -or
    $row.call_count -ne $observed.calls -or $row.unwound_call_count -ne 0 -or
    [uint64]$row.inclusive_ticks -eq 0 -or $row.exclusive_ticks -ne $row.inclusive_ticks) {
    throw 'Wrong procedure totals after Delphi endurance capture.'
}
[ordered]@{
    seconds_per_mode = $Seconds; baseline = $baseline; instrumented = $observed;
    live_snapshots = $liveSnapshots; maximum_gap_ms = $maxGap * 1000.0 / $observed.clock_hz;
    recorded_calls = $row.call_count; native_lost_events = 0
} | ConvertTo-Json -Depth 8 | Set-Content -Path (Join-Path $Output 'endurance.json') -Encoding utf8
