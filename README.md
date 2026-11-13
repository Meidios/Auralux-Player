# Auralux Player

[简体中文](#简体中文) · [English](#english)

## 简体中文

Auralux Player 是一款面向声音设计师和音乐制作人的 Windows x64 音频播放器。把一组音效或混音拖入播放列表，就能在试听时同时观察波形、频谱、响度和立体声声场。当前源码版本为 **1.5.92**。

### 试听与分析

- **连续试听：** 支持多播放列表、列表内排序，以及将选中的文件拖到其他应用。播放器会先预缓冲下一条音频，再切换播放流；新流启动时不加淡入，保留打击声等素材的起始瞬态。
- **频率与空间：** 在双声道波形中定位，结合 FFT 频谱与声谱图查看频率成分随时间的变化。声场图和相位相关度可用于检查立体声宽度及声道间的相位关系。
- **电平与响度：** 查看 Sample Peak（dBFS）、True Peak（dBTP）、VU、瞬时 / 短时 / 积分响度（LUFS）和响度范围（LRA）。可切换到实时输入进行分析。
- **格式与设备：** 支持 WAV、AIFF、MP3、Ogg Vorbis、FLAC 和 Opus；AAC、M4A、WMA 的支持取决于系统解码器。可选择 Windows 系统输出或兼容设备的 ASIO 驱动。

### 安装、帮助与升级

从 [1.5.92 发布页](https://github.com/Meidios/Auralux-Player/releases/tag/v1.5.92) 下载 Windows x64 安装包，或按下方步骤从源码构建。播放音量首次启动时为 100%，之后会记住上次的音量和静音状态。菜单、帮助和更新窗口会随播放器切换深浅色主题。

运行 Windows x64 安装包即可为当前用户安装，无需管理员权限。要从便携版迁移并保留播放列表和布局，先退出播放器，再在向导中选择原 EXE 所在目录。首次播放仍需要下载音频运行库；无网络时可复用已有的 `runtime`。

仪表、播放控件、文件信息和状态提示固定使用英文。菜单、帮助和更新等对话框默认跟随 Windows 显示语言，支持简体中文、繁體中文和 English；其他语言回退到英文。点击左上角波形 Logo，在「语言」中选择后，重新打开播放器即可生效。安装完成页可以选择打开 Windows 默认应用设置，为需要的音频格式指定播放器。跳过会保留原来的默认应用。

点击左上角波形 Logo 打开帮助菜单，可访问检查更新、启动检查开关、离线更新记录和关于窗口；按 F1 打开使用帮助。启动时仅检查正式版，不会自动安装。下载前可查看目标版本的说明，选择稍后或跳过；安装包须通过大小和 SHA-256 校验。

### 从源码构建

在 Windows 10/11 x64 上安装 [Go 1.23 或更高版本](https://go.dev/dl/)，确保 PowerShell 可用。在仓库根目录运行：

```powershell
.\Build_Windows_x64.bat
.\AuraluxPlayer.exe
```

构建脚本会在仓库根目录生成 EXE，并将 `Auralux.ico` 嵌入程序。首次启动请使用可写目录：播放器会从 Un4seen 官方服务器下载缺少的 64 位 BASS 运行库，存放在 EXE 旁的 `runtime\` 目录。编译和默认测试不需要这些 DLL；离线播放前请预先准备好运行库。

**生成安装包：** 安装 Inno Setup 6.5 或更高的 6.x 版本后运行 `.\Build_Setup.ps1`。脚本会校验安装包的 SHA-256，并将摘要打印到终端。安装包保存在 `dist\`。参见[发布说明](docs/packaging.md)。

**运行测试：** `go test -vet=off ./...`。在 Go 1.23.12 上，默认的 `go test ./...` 会在执行测试前报告 `stereo_line_test.go` 中一条增益断言为 `suspect or`。上述命令跳过静态检查并运行测试；需要音频设备或外部素材的可选测试默认跳过。

播放器源码使用 [MIT 许可证](LICENSE)。BASS 及其扩展使用各自的许可，未随源码分发；详见[第三方说明](THIRD_PARTY_NOTICES.md)。

## English

Auralux Player is a Windows x64 audio player for sound designers and music producers. Drop a set of sound effects or mixes into a playlist to audition them while watching their waveforms, spectra, loudness and stereo image. The current source version is **1.5.92**.

### Auditioning and analysis

- **Audition a sequence:** Organize files in multiple playlists, reorder them, or drag selected files into another application. The player prebuffers the next sound before switching streams and starts it without a fade-in, preserving the attack of percussive material.
- **Inspect frequency and phase:** Seek in the stereo waveform, use the FFT spectrum and spectrogram to examine how frequency content changes over time, and check stereo width and phase relationships with the stereo scope and correlation meter.
- **Measure levels and loudness:** Read sample peak (dBFS), true peak (dBTP), VU, momentary, short-term and integrated loudness (LUFS), and loudness range (LRA). Switch the analyzer to a live input to meter an incoming signal.
- **Choose a format and output:** Play WAV, AIFF, MP3, Ogg Vorbis, FLAC and Opus. AAC, M4A and WMA support depends on the system codecs. Use Windows audio output or the ASIO driver for a compatible device.

### Installation, help and updates

Download the Windows x64 installer from the [1.5.92 release page](https://github.com/Meidios/Auralux-Player/releases/tag/v1.5.92), or build it from source using the steps below. Playback starts at 100% volume and remembers the last level and mute state. Menus, Help and update dialogs follow the light or dark theme selected in the player.

Run the Windows x64 setup wizard to install for the current user without administrator privileges. To keep playlists and layout when moving from a portable copy, close the player and select its existing EXE directory in the wizard. First playback needs the audio runtime; an existing `runtime` folder can be reused offline.

Meters, playback controls, file information and status messages stay in English. Menus and dialogs, including Help and Updates, follow your Windows display language and fall back to English. To override the menu language, click the waveform logo in the upper left, choose **Language**, then reopen the player. On the final setup page you may also open Windows Default Apps to choose the audio formats to associate. Leaving this option unchecked preserves existing defaults.

Click the waveform logo in the upper left to check for stable releases, change startup checks, read offline release notes or open About. F1 opens the usage guide. Read the target release notes before choosing to download, postpone, or skip. Setup starts only after the size and SHA-256 digest match.

### Build from source

On Windows 10/11 x64, install [Go 1.23 or later](https://go.dev/dl/) and make sure Windows PowerShell is available. From the repository root, run:

```powershell
.\Build_Windows_x64.bat
.\AuraluxPlayer.exe
```

The build script writes the EXE to the repository root and embeds `Auralux.ico`. Start the player from a writable directory: on first launch it downloads missing 64-bit BASS libraries from Un4seen into `runtime\` beside the EXE. The build and default tests do not need these DLLs. For offline playback, provision the runtime libraries beforehand.

**Build the installer:** Install Inno Setup 6.5 or a newer 6.x version and run `.\Build_Setup.ps1`. The script verifies the installer copy with SHA-256 and prints the digest. The installer is saved in `dist\`. See the [packaging notes](docs/packaging.md).

**Run the tests:** `go test -vet=off ./...`. On Go 1.23.12, the default `go test ./...` stops before running the tests because vet flags a gain assertion in `stereo_line_test.go` as `suspect or`. The command above skips vet and runs the tests; optional tests that need audio hardware or external media are skipped by default.

The player source is [MIT-licensed](LICENSE). BASS and its add-ons have separate licences and are not distributed with the source. See the [third-party notices](THIRD_PARTY_NOTICES.md).
