param([string]$Output = (Join-Path $env:RUNNER_TEMP 'ap-reference-delphi'))
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force $Output | Out-Null
$Output = (Resolve-Path $Output).Path
$archive = Join-Path $Output 'AntiPessimizer-v0.3.zip'
$uri = 'https://github.com/Hoshoyo/AntiPessimizer/releases/download/v0.3/AntiPessimizer.zip'
Invoke-WebRequest -Uri $uri -OutFile $archive -MaximumRetryCount 2 -RetryIntervalSec 2
if ((Get-Item $archive).Length -ne 1065718 -or (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne 'b86572ca883bd3e4ddf6fdce3df70947a553154a4eeaefdc4a4e7d42357a39a8') {
    throw 'Pinned original release archive changed; refuse extraction and execution'
}
Expand-Archive -Path $archive -DestinationPath (Join-Path $Output 'extracted') -Force
$dll = Join-Path $Output 'extracted/AntiPessimizerDLL.dll'
if ((Get-FileHash $dll -Algorithm SHA256).Hash.ToLowerInvariant() -ne '1c1bf9288a3bfd05303f2b9a86d29693c0a449e716f575a25a59059f1c0d02b5') {
    throw 'Pinned original Delphi DLL changed; refuse all execution'
}
$env:AP_REFERENCE_DELPHI_DLL = $dll
$env:AP_REFERENCE_DELPHI_REPORT = Join-Path $Output 'reference-delphi.json'
$env:AP_REFERENCE_DELPHI_EXCEPTION_REPORT = Join-Path $Output 'reference-delphi-exceptions.json'
try {
    go test -v -count=1 -timeout=45s ./internal/instrumentation/x64 -run '^TestReferenceDelphi(ReadOnlyRoutinesPreserveNativeResults|StaticPlanCoverage)$'
    if ($LASTEXITCODE -ne 0) { throw 'Reviewed original Delphi routines changed results after relocation' }
    go test -v -count=1 -timeout=45s ./internal/instrumentation/native -run '^TestReferenceDelphiExceptionHandlerPreservesCatchAndReraise$'
    if ($LASTEXITCODE -ne 0) { throw 'Original Delphi handler, native events or Go accounting changed behavior' }
} finally {
    Remove-Item Env:AP_REFERENCE_DELPHI_DLL -ErrorAction SilentlyContinue
    Remove-Item Env:AP_REFERENCE_DELPHI_REPORT -ErrorAction SilentlyContinue
    Remove-Item Env:AP_REFERENCE_DELPHI_EXCEPTION_REPORT -ErrorAction SilentlyContinue
}
