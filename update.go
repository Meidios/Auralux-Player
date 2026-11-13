package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const appVersion = "1.5.92"
const projectURL = "https://github.com/Meidios/Auralux-Player"
const releasesURL = projectURL + "/releases"
const latestAPI = "https://api.github.com/repos/Meidios/Auralux-Player/releases/latest"
const maxSetupSize = 512 << 20

//go:embed docs/releases/v*.md
var notesFS embed.FS
var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)

func newer(a, b string) bool {
	if !stableVersion.MatchString(a) || !stableVersion.MatchString(b) {
		return false
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func noteVersions() []string {
	files, _ := notesFS.ReadDir("docs/releases")
	var all []string
	for _, f := range files {
		v := strings.TrimSuffix(strings.TrimPrefix(f.Name(), "v"), ".md")
		if stableVersion.MatchString(v) {
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool { return newer(all[i], all[j]) })
	return all
}
func noteText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	// Keep unsectioned third-party text as supplied; never display another version's notes.
	headers := map[string]language{"简体中文": simplified, "English": english, "繁體中文": traditional}
	sections := map[language][]string{}
	var current language
	seen := false
	for _, line := range strings.Split(s, "\n") {
		if lang, ok := headers[strings.TrimSpace(line)]; ok {
			current = lang
			seen = true
			continue
		}
		if seen {
			sections[current] = append(sections[current], line)
		}
	}
	if seen {
		s = ""
		for _, lang := range []language{displayLanguage, simplified, english, traditional} {
			if v := strings.TrimSpace(strings.Join(sections[lang], "\n")); v != "" {
				s = v
				break
			}
		}
	}
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if s == "" {
		return tr("此版本未提供更新说明。")
	}
	return s
}
func localNotes(v string) string {
	b, _ := notesFS.ReadFile("docs/releases/v" + v + ".md")
	return noteText(string(b))
}

type updatePrefs struct {
	DisableAuto bool
	Skipped     string
	LastRun     string
}

func loadUpdatePrefs(dir string) updatePrefs {
	var p updatePrefs
	if b, e := os.ReadFile(filepath.Join(dir, "updates.json")); e == nil {
		if json.Unmarshal(b, &p) != nil {
			p = updatePrefs{}
		}
	}
	return p
}
func (p updatePrefs) save(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "updates-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), filepath.Join(dir, "updates.json"))
}
func recordLaunch(dir, v string) (updatePrefs, string, error) {
	p := loadUpdatePrefs(dir)
	old := p.LastRun
	if old == "" {
		if b, e := os.ReadFile(filepath.Join(dir, "previous-version.txt")); e == nil {
			old = strings.TrimSpace(string(b))
		}
	}
	if p.LastRun != v {
		p.LastRun = v
		if err := p.save(dir); err != nil {
			return p, "", err
		}
	}
	// Consume the installer's one-time fallback only after a durable baseline.
	// Resetting preferences later must not replay the original upgrade.
	_ = os.Remove(filepath.Join(dir, "previous-version.txt"))
	if newer(v, old) {
		return p, old, nil
	}
	return p, "", nil
}

type releaseAsset struct {
	Name   string
	URL    string `json:"browser_download_url"`
	Size   int64
	Digest string
	State  string
}
type stableRelease struct {
	Tag               string `json:"tag_name"`
	Body              string
	Draft, Prerelease bool
	Assets            []releaseAsset
}

func (r stableRelease) version() string { return strings.TrimPrefix(r.Tag, "v") }
func (r stableRelease) page() string    { return releasesURL + "/tag/" + r.Tag }
func (r stableRelease) setup() (releaseAsset, bool) {
	if !stableVersion.MatchString(r.version()) || r.Draft || r.Prerelease {
		return releaseAsset{}, false
	}
	name := "AuraluxPlayer-" + r.version() + "-windows-x64-setup.exe"
	for _, a := range r.Assets {
		h, e := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:"))
		if a.Name == name && a.State == "uploaded" && a.URL == releasesURL+"/download/"+r.Tag+"/"+name && a.Size > 0 && a.Size <= maxSetupSize && strings.HasPrefix(a.Digest, "sha256:") && e == nil && len(h) == 32 {
			return a, true
		}
	}
	return releaseAsset{}, false
}
func allowRedirect(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Host {
	case "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func updateClient(download bool) *http.Client {
	timeout := 15 * time.Second
	if download {
		timeout = 15 * time.Minute
	}
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !download || len(via) > 5 || !allowRedirect(req.URL) {
			return errors.New(tr("不接受此下载重定向"))
		}
		return nil
	}}
}
func fetchRelease(ctx context.Context, client *http.Client) (stableRelease, error) {
	var r stableRelease
	req, e := http.NewRequestWithContext(ctx, "GET", latestAPI, nil)
	if e != nil {
		return r, e
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AuraluxPlayer/"+appVersion)
	res, e := client.Do(req)
	if e != nil {
		return r, e
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return r, nil
	}
	if res.StatusCode != 200 {
		return r, fmt.Errorf("GitHub: HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil {
		return r, e
	}
	if len(b) > 1<<20 {
		return r, errors.New(tr("版本信息过大"))
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if r.Draft || r.Prerelease {
		return stableRelease{}, nil
	}
	if !stableVersion.MatchString(r.version()) {
		return r, errors.New(tr("无法识别正式版本号"))
	}
	return r, nil
}
func (p updatePrefs) offer(r stableRelease, manual bool) bool {
	return !r.Draft && !r.Prerelease && newer(r.version(), appVersion) && (manual || (!p.DisableAuto && r.version() != p.Skipped))
}

type progressWriter struct {
	n      int64
	update func(int64)
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.n += int64(len(b))
	w.update(w.n)
	return len(b), nil
}
func fetchSetup(ctx context.Context, c *http.Client, r stableRelease, dir string, progress func(int64)) (path string, err error) {
	a, ok := r.setup()
	if !ok {
		return "", errors.New(tr("没有可校验的 x64 安装包"))
	}
	req, e := http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	if e != nil {
		return "", e
	}
	res, e := c.Do(req)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf(tr("下载失败：HTTP %d"), res.StatusCode)
	}
	if res.ContentLength >= 0 && res.ContentLength != a.Size {
		return "", errors.New(tr("下载大小不符"))
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	f, e := os.CreateTemp(dir, "Auralux-*-setup.exe")
	if e != nil {
		return "", e
	}
	name := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(name)
		}
	}()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h, &progressWriter{update: progress}), io.LimitReader(res.Body, a.Size+1))
	if e != nil {
		return "", e
	}
	if n != a.Size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.Digest[7:]) {
		return "", errors.New(tr("安装包大小或 SHA-256 不符，已丢弃下载"))
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	return name, nil
}
