#ifndef AppVersion
  #error Build with Build_Setup.ps1 so all versions match the player.
#endif
[Setup]
AppId={{BC7A82B5-8CB5-412E-B42C-A695FAC09F35}
AppName=Auralux Player
AppVersion={#AppVersion}
AppPublisher=Meidios
AppPublisherURL=https://github.com/Meidios/Auralux-Player
AppUpdatesURL=https://github.com/Meidios/Auralux-Player/releases
DefaultDirName={localappdata}\Programs\Auralux Player
DefaultGroupName=Auralux Player
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
WizardStyle=modern
SetupIconFile=..\Auralux.ico
UninstallDisplayIcon={app}\AuraluxPlayer.exe
OutputBaseFilename=AuraluxPlayer-{#AppVersion}-windows-x64-setup
VersionInfoVersion={#AppVersion}.0
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
AppMutex=Local\AuraluxPlayer.SingleInstance
LanguageDetectionMethod=uilanguage
UsePreviousLanguage=no
ChangesAssociations=yes
[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "chinesesimplified"; MessagesFile: "languages\ChineseSimplified.isl"
Name: "chinesetraditional"; MessagesFile: "languages\ChineseTraditional.isl"
[CustomMessages]
english.ChooseAppDefaults=Open Auralux Player’s settings to choose default audio formats
chinesesimplified.ChooseAppDefaults=打开 Auralux Player 专页，选择默认用它播放的音频格式
chinesetraditional.ChooseAppDefaults=開啟 Auralux Player 專頁，選擇預設由它播放的音訊格式
english.ChooseSystemDefaults=Open Windows Default apps to select Auralux Player
chinesesimplified.ChooseSystemDefaults=打开 Windows 默认应用设置，在列表中选择 Auralux Player
chinesetraditional.ChooseSystemDefaults=開啟 Windows 預設應用程式設定，在清單中選擇 Auralux Player
[Tasks]
Name: desktopicon; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
[Files]
Source: "{#Payload}\AuraluxPlayer.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"
Source: "..\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"
Source: "..\assets\AuraluxAudio.ico"; DestDir: "{app}\assets"
Source: "..\support\*.ps1"; DestDir: "{app}\support"; Flags: ignoreversion
[Icons]
Name: "{autoprograms}\Auralux Player"; Filename: "{app}\AuraluxPlayer.exe"
Name: "{autodesktop}\Auralux Player"; Filename: "{app}\AuraluxPlayer.exe"; Tasks: desktopicon
[Registry]
; Use installer-owned keys, independent of the optional portable Open With script.
#define ProgId "Meidios.AuraluxPlayer.Audio"
#define RegisteredApp "Meidios.AuraluxPlayer"
#define CapKey "Software\Meidios\AuraluxPlayer\Capabilities"
Root: HKCU; Subkey: "Software\Classes\{#ProgId}"; ValueType: string; ValueData: "Auralux Audio"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\{#ProgId}\DefaultIcon"; ValueType: string; ValueData: """{app}\assets\AuraluxAudio.ico"",0"
Root: HKCU; Subkey: "Software\Classes\{#ProgId}\shell\open\command"; ValueType: string; ValueData: """{app}\AuraluxPlayer.exe"" ""%1"""
Root: HKCU; Subkey: "{#CapKey}"; ValueType: string; ValueName: "ApplicationName"; ValueData: "Auralux Player"; Flags: uninsdeletekey
Root: HKCU; Subkey: "{#CapKey}"; ValueType: string; ValueName: "ApplicationDescription"; ValueData: "Auralux Player"
Root: HKCU; Subkey: "{#CapKey}"; ValueType: string; ValueName: "ApplicationIcon"; ValueData: """{app}\AuraluxPlayer.exe"",0"
Root: HKCU; Subkey: "Software\RegisteredApplications"; ValueType: string; ValueName: "{#RegisteredApp}"; ValueData: "{#CapKey}"; Flags: uninsdeletevalue
#sub RegisterAudioExt
Root: HKCU; Subkey: "{#CapKey}\FileAssociations"; ValueType: string; ValueName: ".{#Ext}"; ValueData: "{#ProgId}"
Root: HKCU; Subkey: "Software\Classes\.{#Ext}\OpenWithProgids"; ValueType: string; ValueName: "{#ProgId}"; ValueData: ""; Flags: uninsdeletevalue
#endsub
#define public Ext "wav"
#emit RegisterAudioExt
#undef Ext
#define public Ext "wave"
#emit RegisterAudioExt
#undef Ext
#define public Ext "aif"
#emit RegisterAudioExt
#undef Ext
#define public Ext "aiff"
#emit RegisterAudioExt
#undef Ext
#define public Ext "flac"
#emit RegisterAudioExt
#undef Ext
#define public Ext "mp3"
#emit RegisterAudioExt
#undef Ext
#define public Ext "ogg"
#emit RegisterAudioExt
#undef Ext
#define public Ext "m4a"
#emit RegisterAudioExt
#undef Ext
#define public Ext "aac"
#emit RegisterAudioExt
#undef Ext
#define public Ext "wma"
#emit RegisterAudioExt
#undef Ext
#define public Ext "opus"
#emit RegisterAudioExt
#undef Ext
[Run]
Filename: "{app}\AuraluxPlayer.exe"; Description: "{cm:LaunchProgram,Auralux Player}"; Flags: nowait postinstall skipifsilent
Filename: "{code:DefaultsURI}"; Description: "{code:DefaultsLabel}"; Flags: shellexec nowait postinstall unchecked skipifsilent
[Code]
var OldVersion: String;
function DefaultsURI(Param: String): String;
var V: TWindowsVersion; UBR: Cardinal;
begin
  Result := 'ms-settings:defaultapps';
  GetWindowsVersionEx(V);
  UBR := 0;
  RegQueryDWordValue(HKLM64, 'SOFTWARE\Microsoft\Windows NT\CurrentVersion', 'UBR', UBR);
  if (V.Build >= 22631) or ((V.Build = 22621) and (UBR >= 1555)) or
    ((V.Build = 22000) and (UBR >= 1817)) then
    Result := Result + '?registeredAppUser={#RegisteredApp}';
end;
function DefaultsLabel(Param: String): String;
begin
  if DefaultsURI('') = 'ms-settings:defaultapps' then
    Result := CustomMessage('ChooseSystemDefaults')
  else
    Result := CustomMessage('ChooseAppDefaults');
end;
procedure CurStepChanged(CurStep: TSetupStep);
var Exe, Config, Hash: String; MS, LS: Cardinal;
begin
  Config := ExpandConstant('{app}\runtime\config');
  if CurStep = ssInstall then begin
    Exe := ExpandConstant('{app}\AuraluxPlayer.exe');
    if FileExists(Exe) and not FileExists(Config + '\updates.json') then begin
      if GetVersionNumbers(Exe, MS, LS) and (MS <> 0) then
        OldVersion := Format('%d.%d.%d', [MS shr 16, MS and $FFFF, LS shr 16])
      else begin
        { The two known 1.5.79 portable binaries have no VERSIONINFO resource. }
        Hash := GetSHA256OfFile(Exe);
        if (CompareText(Hash, '4847fc1be5c8a9c38d5994759b286554a45f60730a9911568610e47055b09cbc') = 0) or
           (CompareText(Hash, '645115bd9ce4caffad7c0978bd8031fd5708da6b23a265e45d1d8a701e82e8c1') = 0) then OldVersion := '1.5.79';
      end;
    end;
  end;
  if (CurStep = ssPostInstall) and (OldVersion <> '') then begin
    ForceDirectories(Config);
    SaveStringToFile(Config + '\previous-version.txt', AnsiString(OldVersion), False);
  end;
end;
