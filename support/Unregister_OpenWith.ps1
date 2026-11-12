$ErrorActionPreference = 'Stop'
$appKey = 'HKCU:\Software\Classes\Applications\AuraluxPlayer.exe'
$progId = 'AuraluxPlayer.AudioFile'
$progKey = "HKCU:\Software\Classes\$progId"
$capRoot = 'HKCU:\Software\AuraluxPlayer'
$registeredApps = 'HKCU:\Software\RegisteredApplications'
$extensions = '.wav','.wave','.aif','.aiff','.flac','.mp3','.ogg','.m4a','.aac','.wma','.opus'

if (Test-Path $appKey) {
    Remove-Item -Path $appKey -Recurse -Force
}
if (Test-Path $progKey) {
    Remove-Item -Path $progKey -Recurse -Force
}
if (Test-Path $capRoot) {
    Remove-Item -Path $capRoot -Recurse -Force
}
if (Test-Path $registeredApps) {
    Remove-ItemProperty -Path $registeredApps -Name 'Auralux Player' -ErrorAction SilentlyContinue
}
foreach ($ext in $extensions) {
    $openWithProgids = "HKCU:\Software\Classes\$ext\OpenWithProgids"
    if (Test-Path $openWithProgids) {
        Remove-ItemProperty -Path $openWithProgids -Name $progId -ErrorAction SilentlyContinue
    }
}

if (-not ('ShellNotifyRemove' -as [type])) {
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class ShellNotifyRemove {
    [DllImport("shell32.dll")]
    public static extern void SHChangeNotify(uint eventId, uint flags, IntPtr item1, IntPtr item2);
}
'@
}
[ShellNotifyRemove]::SHChangeNotify(0x08000000, 0, [IntPtr]::Zero, [IntPtr]::Zero)
Write-Host '已从 Windows“打开方式/默认应用”列表移除 Auralux Player 注册信息。'
