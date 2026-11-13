package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestDisplayLanguageSelection(t *testing.T) {
	for _, tc := range []struct {
		pref, system string
		want         language
	}{
		{"system", "zh-CN", simplified}, {"system", "zh-SG", simplified}, {"system", "zh-TW", traditional}, {"system", "zh-HK", traditional}, {"system", "zh-MO", traditional}, {"system", "zh-Hans-HK", simplified}, {"system", "zh-Hant-CN", traditional}, {"system", "en-GB", english}, {"system", "ja-JP", english}, {"en", "zh-TW", english}, {"invalid", "zh-TW", traditional}} {
		if got := resolveLanguage(tc.pref, tc.system); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
func TestSavedLanguageAndReleaseNotes(t *testing.T) {
	orig, pref := displayLanguage, languagePreference
	defer func() { displayLanguage = orig; languagePreference = pref }()
	d := t.TempDir()
	for _, tc := range []struct{ code, body, guide string }{{"en", "Windows display language", "User guide"}, {"zh-Hant", "Windows 顯示語言", "使用說明"}, {"system", "Windows 显示语言", "使用帮助"}} {
		if e := os.WriteFile(filepath.Join(d, "language.txt"), []byte(tc.code), 0600); e != nil {
			t.Fatal(e)
		}
		loadLanguage(d, "zh-CN")
		if tr("使用帮助") != tc.guide || !strings.Contains(localNotes("1.5.81"), tc.body) {
			t.Fatal(tc)
		}
		if playlistDisplayName(Playlist{ID: "custom", Name: "使用帮助"}) != "使用帮助" {
			t.Fatal("user data was translated")
		}
	}
}
func TestCatalogFormats(t *testing.T) {
	spec := regexp.MustCompile(`%[-+# 0-9.]*[%a-zA-Z]`)
	for k, row := range messages {
		want := spec.FindAllString(k, -1)
		for i, v := range row {
			if strings.TrimSpace(v) == "" || !reflect.DeepEqual(want, spec.FindAllString(v, -1)) {
				t.Errorf("invalid translation %d: %q", i, k)
			}
		}
	}
}

func TestWorkspaceStaysEnglishWithLocalizedMenus(t *testing.T) {
	previous := displayLanguage
	defer func() { displayLanguage = previous }()
	a := App{stereoStyle: 1}
	for _, tc := range []struct {
		lang            language
		guide, playlist string
	}{{english, "User guide", "Default playlist"}, {simplified, "使用帮助", "播放列表"}, {traditional, "使用說明", "播放清單"}} {
		displayLanguage = tc.lang
		if got := []string{a.analyzerViewLabel(), a.stereoStyleLabel(), a.spectrumBandLabel(), channelLayoutLabel(2), a.deviceButtonLabel()}; !reflect.DeepEqual(got, []string{"SPECTRUM", "DOT", "BAND 96", "Stereo", "Output Device"}) {
			t.Fatalf("language %v: workspace labels %q", tc.lang, got)
		}
		if tr("使用帮助") != tc.guide || playlistDisplayName(newDefaultPlaylist()) != tc.playlist {
			t.Fatalf("language %v: menu labels did not follow the language", tc.lang)
		}
	}
}
