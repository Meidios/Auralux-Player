$ErrorActionPreference = "Stop"
$support = Split-Path -Parent $MyInvocation.MyCommand.Path
$root = Split-Path -Parent $support
$runtime = Join-Path $root "runtime"
New-Item -ItemType Directory -Force -Path $runtime | Out-Null

$packages = @(
    @{ Name="BASS";     Url="https://www.un4seen.com/files/bass24.zip";     Dll="bass.dll" },
    @{ Name="BASSFLAC"; Url="https://www.un4seen.com/files/bassflac24.zip"; Dll="bassflac.dll" },
    @{ Name="BASSOPUS"; Url="https://www.un4seen.com/files/bassopus24.zip"; Dll="bassopus.dll" },
    @{ Name="BASSloud"; Url="https://www.un4seen.com/files/bassloud24.zip"; Dll="bassloud.dll" },
    @{ Name="BASSASIO"; Url="https://www.un4seen.com/files/bassasio14.zip"; Dll="bassasio.dll" }
)

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("AuraluxRuntime_" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $temp | Out-Null
try {
    foreach ($pkg in $packages) {
        $target = Join-Path $runtime $pkg.Dll
        if (Test-Path $target) {
            Write-Host ("[OK] " + $pkg.Dll + " already exists")
            continue
        }
        Write-Host ("Downloading " + $pkg.Name + " from the official server...")
        $zipPath = Join-Path $temp ($pkg.Name + ".zip")
        $extract = Join-Path $temp $pkg.Name
        Invoke-WebRequest -Uri $pkg.Url -OutFile $zipPath -UseBasicParsing
        Expand-Archive -Path $zipPath -DestinationPath $extract -Force
        $dll = Get-ChildItem -Path $extract -Recurse -File -Filter $pkg.Dll |
            Where-Object { $_.FullName -match "[\\/]x64[\\/]" } |
            Select-Object -First 1
        if (-not $dll) {
            throw ("64-bit " + $pkg.Dll + " was not found in the official package")
        }
        Copy-Item $dll.FullName $target -Force
        Write-Host ("[OK] Installed " + $pkg.Dll)
    }
    Write-Host ""
    Write-Host "Auralux audio runtime installation completed."
}
finally {
    Remove-Item $temp -Recurse -Force -ErrorAction SilentlyContinue
}
Read-Host "Press Enter to close"
