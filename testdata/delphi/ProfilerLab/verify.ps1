# Windows PowerShell 5.1; run after compiling both configurations.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Push-Location $PSScriptRoot
try {
    New-Item -ItemType Directory -Force logs | Out-Null
    foreach ($config in @('Symbols', 'NoSymbols')) {
        $exe = Join-Path $PSScriptRoot "bin\$config\ProfilerLab.exe"
        if (!(Test-Path $exe)) { throw "Missing executable: $exe" }
        & $exe --self-test > "logs\selftest-$config.tsv"
        if ($LASTEXITCODE -ne 0) { throw "Self-test failed: $config" }
        $self = @(Import-Csv "logs\selftest-$config.tsv" -Delimiter "`t")
        if (@($self | Where-Object event -eq 'selftest_pass').Count -ne 1) {
            throw "Self-test did not finish: $config"
        }
        & $exe --iterations 1000 --wait-ms 0 > "logs\smoke-$config.tsv"
        if ($LASTEXITCODE -ne 0) { throw "Smoke run failed: $config" }
        $rows = @(Import-Csv "logs\smoke-$config.tsv" -Delimiter "`t")
        if (@($rows | Where-Object event -eq 'session_end').Count -ne 1) {
            throw "Incomplete session: $config"
        }
        $work = @($rows | Where-Object event -eq 'work_end')
        if ($work.Count -ne 4) { throw "Expected four completed workers: $config" }
        foreach ($row in $work) {
            if ($row.worker -notin @('A', 'B')) { throw 'Unexpected worker' }
            $expected = if ($row.worker -eq 'A') { '269958183' } else { '429183498' }
            if ($row.checksum -ne $expected -or $row.iterations -ne '1000') {
                throw "Wrong workload result: $config"
            }
        }
        $parallel = @($work | Where-Object phase -eq 'parallel')
        if ($parallel.Count -ne 2 -or $parallel[0].tid -eq $parallel[1].tid) {
            throw "Parallel workers must have distinct thread IDs: $config"
        }
        $invalid = Start-Process -FilePath $exe -ArgumentList @('--iterations', '0') `
            -Wait -PassThru -NoNewWindow `
            -RedirectStandardOutput "logs\invalid-$config.stdout" `
            -RedirectStandardError "logs\invalid-$config.stderr"
        if ($invalid.ExitCode -eq 0) { throw "Invalid input accepted: $config" }
        Get-FileHash $exe -Algorithm SHA256 | Format-List |
            Out-File "logs\sha256-$config.txt"
    }
    Write-Host 'Self-tests, smoke runs and invalid-input checks passed.'
} finally {
    Pop-Location
}
