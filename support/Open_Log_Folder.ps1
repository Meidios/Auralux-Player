$ErrorActionPreference = "Stop"
$dir = Join-Path $env:APPDATA "AuraluxPlayer"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Start-Process explorer.exe $dir
