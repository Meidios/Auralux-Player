//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type runtimePackage struct {
	Name     string
	URL      string
	DLL      string
	Optional bool
}

var runtimePackages = []runtimePackage{
	{"BASS playback engine", "https://www.un4seen.com/files/bass24.zip", "bass.dll", false},
	{"BASSFLAC decoder", "https://www.un4seen.com/files/bassflac24.zip", "bassflac.dll", false},
	{"BASSOPUS decoder", "https://www.un4seen.com/files/bassopus24.zip", "bassopus.dll", false},
	{"BASSloud loudness engine", "https://www.un4seen.com/files/bassloud24.zip", "bassloud.dll", false},
	{"BASSASIO low-latency output", "https://www.un4seen.com/files/bassasio14.zip", "bassasio.dll", true},
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		d, _ := os.Getwd()
		return d
	}
	return filepath.Dir(exe)
}

func ensureAudioRuntime(runtimeDir string, status func(string)) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return err
	}
	missing := false
	for _, p := range runtimePackages {
		if !validDLL(filepath.Join(runtimeDir, p.DLL)) {
			missing = true
			break
		}
	}
	if !missing {
		return nil
	}

	if status != nil {
		status("Checking audio runtime…")
	}
	client := &http.Client{Timeout: 90 * time.Second}
	optionalFailed := false
	for idx, pkg := range runtimePackages {
		target := filepath.Join(runtimeDir, pkg.DLL)
		if validDLL(target) {
			continue
		}
		if status != nil {
			status(fmt.Sprintf("Installing %s (%d/%d)…", pkg.Name, idx+1, len(runtimePackages)))
		}
		if err := installRuntimePackage(client, pkg, target, status); err != nil {
			if pkg.Optional {
				optionalFailed = true
				if status != nil {
					status("ASIO installation failed. System playback is still available.")
				}
				continue
			}
			return err
		}
	}
	if status != nil {
		if optionalFailed {
			status("Audio engine installed · ASIO unavailable")
		} else {
			status("Audio engine installed · ASIO enabled")
		}
	}
	return nil
}

func installRuntimePackage(client *http.Client, pkg runtimePackage, target string, status func(string)) error {
	req, err := http.NewRequest("GET", pkg.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AuraluxPlayer/"+appVersion+" (+Windows desktop audio player)")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Could not download %s: %w", pkg.Name, err)
	}
	data, readErr := readRuntimeDownload(resp.Body, resp.ContentLength, pkg.Name, status)
	resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("Could not read %s: %w", pkg.Name, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Could not download %s: HTTP %d", pkg.Name, resp.StatusCode)
	}
	dll, err := extractX64DLL(data, pkg.DLL)
	if err != nil {
		return fmt.Errorf("Could not install %s: %w", pkg.Name, err)
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, dll, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	return nil
}

func readRuntimeDownload(r io.Reader, total int64, name string, status func(string)) ([]byte, error) {
	const maxDownload = int64(64 << 20)
	if total > maxDownload {
		return nil, fmt.Errorf("Download too large (%d MB)", total>>20)
	}
	reader := io.LimitReader(r, maxDownload)
	buf := bytes.NewBuffer(make([]byte, 0, 2<<20))
	chunk := make([]byte, 64<<10)
	var downloaded int64
	lastUpdate := time.Time{}
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			downloaded += int64(n)
			_, _ = buf.Write(chunk[:n])
			if status != nil && total > 0 && (lastUpdate.IsZero() || time.Since(lastUpdate) >= 250*time.Millisecond) {
				percent := int(downloaded * 100 / total)
				if percent > 100 {
					percent = 100
				}
				status(fmt.Sprintf("Installing %s · Downloading %d%%…", name, percent))
				lastUpdate = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if status != nil && total > 0 {
		status(fmt.Sprintf("Installing %s · Verifying file…", name))
	}
	return buf.Bytes(), nil
}

func extractX64DLL(data []byte, dllName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("Downloaded data is not a valid ZIP: %w", err)
	}
	var fallback *zip.File
	want := strings.ToLower(dllName)
	for _, f := range zr.File {
		base := strings.ToLower(filepath.Base(strings.ReplaceAll(f.Name, "\\", "/")))
		if base != want {
			continue
		}
		lower := strings.ToLower(strings.ReplaceAll(f.Name, "\\", "/"))
		if strings.Contains(lower, "/x64/") || strings.HasPrefix(lower, "x64/") {
			return readZipDLL(f)
		}
		fallback = f
	}
	if fallback != nil {
		return readZipDLL(fallback)
	}
	return nil, fmt.Errorf("%s was not found in the archive", dllName)
}
func readZipDLL(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, 32<<20))
	if err != nil {
		return nil, err
	}
	if len(b) < 2 || b[0] != 'M' || b[1] != 'Z' {
		return nil, fmt.Errorf("DLL verification failed")
	}
	return b, nil
}
func validDLL(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && len(b) > 2 && b[0] == 'M' && b[1] == 'Z'
}
