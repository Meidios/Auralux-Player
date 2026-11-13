param([string]$OutputDir = (Join-Path $PSScriptRoot 'dist'), [string]$Compiler, [string]$Go)
$ErrorActionPreference = 'Stop'
if (!$Go) {
    $command = Get-Command go -ErrorAction SilentlyContinue
    if ($command) { $Go = $command.Source } else { $Go = Join-Path $PSScriptRoot '..\.build-tools\go\bin\go.exe' }
}
if (!$Compiler) {
    $Compiler = @((Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'), (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'), (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')) | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
}
if (!$Compiler) { throw 'Install Inno Setup 6.5 or newer, or specify -Compiler.' }
$version = [regex]::Match((Get-Content (Join-Path $PSScriptRoot 'update.go') -Raw), '(?m)^const appVersion = "(\d+\.\d+\.\d+)"').Groups[1].Value
if (!$version -or !(Test-Path (Join-Path $PSScriptRoot "docs\releases\v$version.md"))) { throw 'Version or matching release notes missing.' }
$payload = New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) ('AuraluxBuild-' + [guid]::NewGuid()))
$out = [IO.Directory]::CreateDirectory($OutputDir).FullName
Push-Location $PSScriptRoot
try {
    $env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
    $exe = Join-Path $payload.FullName 'AuraluxPlayer.exe'
    & $Go build -trimpath -buildvcs=false '-ldflags=-H=windowsgui -s -w' -o $exe .
    if ($LASTEXITCODE) { throw 'Go build failed.' }
    & "$PSScriptRoot\Embed_Windows_Icon.ps1" -ExePath $exe -IconPath "$PSScriptRoot\Auralux.ico"
    & $Compiler '/Qp' "/DAppVersion=$version" "/DPayload=$($payload.FullName)" "/O$($payload.FullName)" "$PSScriptRoot\installer\AuraluxPlayer.iss"
    if ($LASTEXITCODE) { throw 'Inno Setup failed.' }
    $name = "AuraluxPlayer-$version-windows-x64-setup.exe"
    Copy-Item -LiteralPath (Join-Path $payload.FullName $name) -Destination (Join-Path $out $name) -Force
    $hash = (Get-FileHash -LiteralPath (Join-Path $out $name)).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText((Join-Path $out "$name.sha256"), "$hash  $name" + [Environment]::NewLine)
    Write-Output (Join-Path $out $name)
} finally {
    Pop-Location
    if (Test-Path -LiteralPath $exe) { Remove-Item -LiteralPath $exe }
    if ($name -and (Test-Path -LiteralPath (Join-Path $payload.FullName $name))) { Remove-Item -LiteralPath (Join-Path $payload.FullName $name) }
    if (!$payload.GetFileSystemInfos().Count) { $payload.Delete() }
}
