package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockReleaseHTTP func(*http.Request) (*http.Response, error)

func (f mockReleaseHTTP) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(code int, data []byte) *http.Client {
	return &http.Client{Transport: mockReleaseHTTP(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: code, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: -1}, nil
	})}
}
func fixtureRelease(b []byte) stableRelease {
	const v = "1.10.0"
	name := "AuraluxPlayer-" + v + "-windows-x64-setup.exe"
	return stableRelease{Tag: "v" + v, Assets: []releaseAsset{{Name: name, State: "uploaded", URL: releasesURL + "/download/v" + v + "/" + name, Size: int64(len(b)), Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(b))}}}
}
func TestUpdateCheckAcrossLaunches(t *testing.T) {
	d := t.TempDir()
	p, old, e := recordLaunch(d, appVersion)
	if e != nil || old != "" || p.DisableAuto {
		t.Fatal(p, old, e)
	}
	ctx := context.Background()
	for _, status := range []int{404, 429, 200} {
		r, e := fetchRelease(ctx, fixtureClient(status, []byte(`{"tag_name":"v1.10.0"}`)))
		switch status {
		case 404:
			if e != nil || r.Tag != "" {
				t.Fatal(r, e)
			}
		case 429:
			if e == nil {
				t.Fatal("rate limit treated as success")
			}
		case 200:
			if e != nil || !p.offer(r, false) {
				t.Fatal("later launch did not discover release", e)
			}
		}
		p, _, _ = recordLaunch(d, appVersion)
	}
	r := fixtureRelease([]byte("package"))
	p.Skipped = r.version()
	if e := p.save(d); e != nil {
		t.Fatal(e)
	}
	p = loadUpdatePrefs(d)
	if p.offer(r, false) || !p.offer(r, true) {
		t.Fatal("skip must affect auto checks only")
	}
	r.Tag = "v2.0.0"
	if !p.offer(r, false) {
		t.Fatal("skip leaked to following version")
	}
	p.DisableAuto = true
	if p.offer(r, false) || !p.offer(r, true) {
		t.Fatal("manual checks disabled")
	}
	for _, data := range []string{`{"tag_name":"v9.0.0","prerelease":true}`, `{"tag_name":"v9.0.0","draft":true}`, `{"tag_name":"v1.0.0"}`} {
		r, e := fetchRelease(ctx, fixtureClient(200, []byte(data)))
		if e != nil || p.offer(r, true) {
			t.Fatal(r, e)
		}
	}
	for _, tag := range []string{"v1.beta", "v1.5.80-rc.1", "v1.5.80/../bad"} {
		if _, e := fetchRelease(ctx, fixtureClient(200, []byte(fmt.Sprintf(`{"tag_name":%q}`, tag)))); e == nil {
			t.Fatal(tag)
		}
	}
}
func TestUpdateDownloadIntegrityAndCancel(t *testing.T) {
	data := bytes.Repeat([]byte("test setup"), 8192)
	r := fixtureRelease(data)
	for _, mode := range []string{"good", "truncated", "oversize", "corrupt", "cancel", "wrong-source", "missing-digest", "wrong-arch", "http-error"} {
		t.Run(mode, func(t *testing.T) {
			asset := r.Assets[0]
			b := append([]byte(nil), data...)
			code := 200
			switch mode {
			case "truncated":
				b = b[:len(b)-1]
			case "oversize":
				b = append(b, 1)
			case "corrupt":
				b[1] ^= 1
			case "wrong-source":
				asset.URL = "https://example.com/setup.exe"
			case "missing-digest":
				asset.Digest = ""
			case "wrong-arch":
				asset.Name = "arm64-setup.exe"
			case "http-error":
				code = 503
			}
			rr := r
			rr.Assets = []releaseAsset{asset}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := t.TempDir()
			path, e := fetchSetup(ctx, fixtureClient(code, b), rr, d, func(int64) {
				if mode == "cancel" {
					cancel()
				}
			})
			if mode == "good" {
				got, readErr := os.ReadFile(path)
				if e != nil || readErr != nil || !bytes.Equal(got, data) {
					t.Fatal(path, e, readErr)
				}
				return
			}
			if e == nil || path != "" {
				t.Fatal("unverified path returned", path, e)
			}
			files, _ := os.ReadDir(d)
			if len(files) != 0 {
				t.Fatal("partial download retained")
			}
		})
	}
	for _, s := range []string{"https://release-assets.githubusercontent.com.evil.test/f", "http://objects.githubusercontent.com/f", "https://user@release-assets.githubusercontent.com/f", "https://release-assets.githubusercontent.com:123/f"} {
		u, _ := url.Parse(s)
		if allowRedirect(u) {
			t.Fatal(s)
		}
	}
	u, _ := url.Parse("https://release-assets.githubusercontent.com/file")
	if !allowRedirect(u) {
		t.Fatal("expected GitHub asset redirect")
	}
}
func TestUpdateOfflineBaseline(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "previous-version.txt"), []byte("1.5.79"), 0600); err != nil {
		t.Fatal(err)
	}
	for i, step := range []struct{ v, w string }{{appVersion, "1.5.79"}, {appVersion, ""}, {"1.0.0", ""}, {"1.0.0", ""}, {appVersion, "1.0.0"}} {
		p, old, e := recordLaunch(d, step.v)
		if e != nil || old != step.w || p.LastRun != step.v {
			t.Fatal(i, old, e)
		}
	}
	if _, old, _ := recordLaunch(t.TempDir(), appVersion); old != "" {
		t.Fatal("false first-install success")
	}
	if !strings.Contains(localNotes("1.5.80"), "SHA-256") || strings.Contains(localNotes("1.5.79"), "SHA-256") || noteText("") != tr("此版本未提供更新说明。") {
		t.Fatal("incorrect release notes")
	}
}
