# Auralux Player

[简体中文](#简体中文) · [English](#english)

## 简体中文

Auralux Player 是一款面向声音设计师和音乐制作人的 Windows x64 音频播放器。把一组音效或混音拖入播放列表，就能在试听时同时观察波形、频谱、响度和立体声声场。当前源码版本为 **1.5.79**。

### 试听与分析

- **连续试听：** 支持多播放列表、列表内排序，以及将选中的文件拖到其他应用。1.5.79 会先预缓冲下一条音频，再切换播放流；新流启动时不加淡入，保留打击声等素材的起始瞬态。
- **频率与空间：** 在双声道波形中定位，结合 FFT 频谱与声谱图查看频率成分随时间的变化。声场图和相位相关度可用于检查立体声宽度及声道间的相位关系。
- **电平与响度：** 查看 Sample Peak（dBFS）、True Peak（dBTP）、VU、瞬时 / 短时 / 积分响度（LUFS）和响度范围（LRA）。可切换到实时输入进行分析。
- **格式与设备：** 支持 WAV、AIFF、MP3、Ogg Vorbis、FLAC 和 Opus；AAC、M4A、WMA 的支持取决于系统解码器。可选择 Windows 系统输出或兼容设备的 ASIO 驱动。

### 从源码构建

在 Windows 10/11 x64 上安装 [Go 1.23 或更高版本](https://go.dev/dl/)，确保 PowerShell 可用。在仓库根目录运行：

```powershell
.\Build_Windows_x64.bat
.\AuraluxPlayer.exe
```

构建脚本会在仓库根目录生成 EXE，并将 `Auralux.ico` 嵌入程序。首次启动请使用可写目录：播放器会从 Un4seen 官方服务器下载缺少的 64 位 BASS 运行库，存放在 EXE 旁的 `runtime\` 目录。编译和默认测试不需要这些 DLL；离线播放前请预先准备好运行库。

**运行测试：** `go test -vet=off ./...`。在 Go 1.23.12 上，默认的 `go test ./...` 会在执行测试前报告 `stereo_line_test.go` 中一条增益断言为 `suspect or`。上述命令跳过静态检查并运行测试；需要音频设备或外部素材的可选测试默认跳过。

播放器源码使用 [MIT 许可证](LICENSE)。BASS 及其扩展使用各自的许可，未随源码分发；详见[第三方说明](THIRD_PARTY_NOTICES.md)。

## English

Auralux Player is a Windows x64 audio player for sound designers and music producers. Drop a set of sound effects or mixes into a playlist to audition them while watching their waveforms, spectra, loudness and stereo image. The current source version is **1.5.79**.

### Auditioning and analysis

- **Audition a sequence:** Organize files in multiple playlists, reorder them, or drag selected files into another application. Version 1.5.79 prebuffers the next sound before switching streams and starts it without a fade-in, preserving the attack of percussive material.
- **Inspect frequency and phase:** Seek in the stereo waveform, use the FFT spectrum and spectrogram to examine how frequency content changes over time, and check stereo width and phase relationships with the stereo scope and correlation meter.
- **Measure levels and loudness:** Read sample peak (dBFS), true peak (dBTP), VU, momentary, short-term and integrated loudness (LUFS), and loudness range (LRA). Switch the analyzer to a live input to meter an incoming signal.
- **Choose a format and output:** Play WAV, AIFF, MP3, Ogg Vorbis, FLAC and Opus. AAC, M4A and WMA support depends on the system codecs. Use Windows audio output or the ASIO driver for a compatible device.

### Build from source

On Windows 10/11 x64, install [Go 1.23 or later](https://go.dev/dl/) and make sure Windows PowerShell is available. From the repository root, run:

```powershell
.\Build_Windows_x64.bat
.\AuraluxPlayer.exe
```

The build script writes the EXE to the repository root and embeds `Auralux.ico`. Start the player from a writable directory: on first launch it downloads missing 64-bit BASS libraries from Un4seen into `runtime\` beside the EXE. The build and default tests do not need these DLLs. For offline playback, provision the runtime libraries beforehand.

**Run the tests:** `go test -vet=off ./...`. On Go 1.23.12, the default `go test ./...` stops before running the tests because vet flags a gain assertion in `stereo_line_test.go` as `suspect or`. The command above skips vet and runs the tests; optional tests that need audio hardware or external media are skipped by default.

The player source is [MIT-licensed](LICENSE). BASS and its add-ons have separate licences and are not distributed with the source. See the [third-party notices](THIRD_PARTY_NOTICES.md).
