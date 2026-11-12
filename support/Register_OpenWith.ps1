$ErrorActionPreference = 'Stop'

$support = Split-Path -Parent $MyInvocation.MyCommand.Path
$base = Split-Path -Parent $support
$exe = Join-Path $base 'AuraluxPlayer.exe'
$fileIcon = Join-Path $base 'assets\AuraluxAudio.ico'
if (-not (Test-Path $exe)) {
    throw "未找到 AuraluxPlayer.exe：$exe"
}
if (-not (Test-Path $fileIcon)) {
    throw "未找到音频文件图标：$fileIcon"
}

# Keep the application identity using the EXE's own icon.
$appKey = 'HKCU:\Software\Classes\Applications\AuraluxPlayer.exe'
$commandKey = Join-Path $appKey 'shell\open\command'
$iconKey = Join-Path $appKey 'DefaultIcon'
$supportedKey = Join-Path $appKey 'SupportedTypes'

New-Item -Path $appKey -Force | Out-Null
New-Item -Path (Join-Path $appKey 'shell') -Force | Out-Null
New-Item -Path (Join-Path $appKey 'shell\open') -Force | Out-Null
New-Item -Path $commandKey -Force | Out-Null
New-Item -Path $iconKey -Force | Out-Null
New-Item -Path $supportedKey -Force | Out-Null
New-ItemProperty -Path $appKey -Name 'FriendlyAppName' -Value 'Auralux Player' -PropertyType String -Force | Out-Null
Set-Item -Path $commandKey -Value ('"{0}" "%1"' -f $exe)
Set-Item -Path $iconKey -Value ('"{0}",0' -f $exe)

# Dedicated audio-file ProgID. This lets Explorer show a file icon that is
# visually distinct from the Auralux application icon.
$progId = 'AuraluxPlayer.AudioFile'
$progKey = "HKCU:\Software\Classes\$progId"
$progIconKey = Join-Path $progKey 'DefaultIcon'
$progCommandKey = Join-Path $progKey 'shell\open\command'
New-Item -Path $progKey -Force | Out-Null
New-Item -Path $progIconKey -Force | Out-Null
New-Item -Path (Join-Path $progKey 'shell') -Force | Out-Null
New-Item -Path (Join-Path $progKey 'shell\open') -Force | Out-Null
New-Item -Path $progCommandKey -Force | Out-Null
Set-Item -Path $progKey -Value 'Auralux Audio File'
Set-Item -Path $progIconKey -Value ('"{0}",0' -f $fileIcon)
Set-Item -Path $progCommandKey -Value ('"{0}" "%1"' -f $exe)

$extensions = '.wav','.wave','.aif','.aiff','.flac','.mp3','.ogg','.m4a','.aac','.wma','.opus'
foreach ($ext in $extensions) {
    New-ItemProperty -Path $supportedKey -Name $ext -Value '' -PropertyType String -Force | Out-Null
    $openWithProgids = "HKCU:\Software\Classes\$ext\OpenWithProgids"
    New-Item -Path $openWithProgids -Force | Out-Null
    New-ItemProperty -Path $openWithProgids -Name $progId -Value '' -PropertyType String -Force | Out-Null
}

# Register as a proper Default Programs application so Windows can associate
# the extensions with the dedicated file ProgID rather than the EXE icon.
$capKey = 'HKCU:\Software\AuraluxPlayer\Capabilities'
$fileAssocKey = Join-Path $capKey 'FileAssociations'
New-Item -Path $capKey -Force | Out-Null
New-Item -Path $fileAssocKey -Force | Out-Null
New-ItemProperty -Path $capKey -Name 'ApplicationName' -Value 'Auralux Player' -PropertyType String -Force | Out-Null
New-ItemProperty -Path $capKey -Name 'ApplicationDescription' -Value 'Auralux Player audio player and analyzer' -PropertyType String -Force | Out-Null
foreach ($ext in $extensions) {
    New-ItemProperty -Path $fileAssocKey -Name $ext -Value $progId -PropertyType String -Force | Out-Null
}
$registeredApps = 'HKCU:\Software\RegisteredApplications'
New-Item -Path $registeredApps -Force | Out-Null
New-ItemProperty -Path $registeredApps -Name 'Auralux Player' -Value 'Software\AuraluxPlayer\Capabilities' -PropertyType String -Force | Out-Null

if (-not ('ShellNotify' -as [type])) {
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class ShellNotify {
    [DllImport("shell32.dll")]
    public static extern void SHChangeNotify(uint eventId, uint flags, IntPtr item1, IntPtr item2);
}
'@
}
[ShellNotify]::SHChangeNotify(0x08000000, 0, [IntPtr]::Zero, [IntPtr]::Zero)

Write-Host '已将 Auralux Player 注册到 Windows“打开方式/默认应用”列表。'
Write-Host '应用本身继续使用蓝色波形图标；关联音频文件使用独立的文件图标。'
Write-Host '如已设置过默认播放器，请在 Windows 默认应用中重新选择一次 Auralux Player 以刷新关联。'
