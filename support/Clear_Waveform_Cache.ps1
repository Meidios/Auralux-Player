$ErrorActionPreference = "Stop"
$root = Join-Path $env:APPDATA "AuraluxPlayer"
$found = $false
foreach ($name in @("waveforms", "spectrograms")) {
    $cache = Join-Path $root $name
    if (Test-Path -LiteralPath $cache) {
        Remove-Item -LiteralPath $cache -Recurse -Force
        $found = $true
    }
}
if ($found) {
    Write-Host "Waveform and spectrogram caches cleared."
} else {
    Write-Host "No waveform or spectrogram cache was found."
}
Read-Host "Press Enter to close"
