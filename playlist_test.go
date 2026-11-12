//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func jsonKeys(v map[string]any) []string {
	keys := make([]string, 0, len(v))
	for key := range v {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func testAudioPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newPlaylistTestApp() *App {
	return &App{
		playlists:          []Playlist{newDefaultPlaylist()},
		activePlaylistID:   defaultPlaylistID,
		playbackPlaylistID: defaultPlaylistID,
		current:            -1,
	}
}

func TestApplyPlaylistStateMigratesVersion1(t *testing.T) {
	existing := testAudioPath(t, "existing.wav")
	missing := filepath.Join(t.TempDir(), "offline.flac")
	a := newPlaylistTestApp()
	a.applyPlaylistState(playlistState{
		Paths:        []string{existing, missing, existing},
		SelectedPath: missing,
		CurrentPath:  existing,
	})

	if len(a.playlists) != 1 || a.playlists[0].ID != defaultPlaylistID || a.playlists[0].Name != "PLAYLIST" {
		t.Fatalf("legacy state did not migrate into the default playlist: %#v", a.playlists)
	}
	p := &a.playlists[0]
	if len(p.Tracks) != 2 {
		t.Fatalf("expected duplicate removal with offline path retention, got %d tracks", len(p.Tracks))
	}
	if p.Selected != 1 || !p.Tracks[1].Missing {
		t.Fatalf("offline selected track was not retained: %#v", p)
	}
	if a.restoreCurrentPath != existing {
		t.Fatalf("current path was not migrated: %q", a.restoreCurrentPath)
	}
}

func TestPlaylistStateVersion2RoundTrip(t *testing.T) {
	defaultPath := testAudioPath(t, "default.wav")
	customPath := testAudioPath(t, "custom.mp3")
	a := newPlaylistTestApp()
	a.playlists[0].Tracks = []Track{{Path: defaultPath, Name: "default"}}
	a.playlists[0].Selected = 0
	a.playlists = append(a.playlists, Playlist{ID: "custom-id", Name: "Review", Tracks: []Track{{Path: customPath, Name: "custom"}}, Selected: 0, Scroll: 3})
	a.activePlaylistID = "custom-id"
	a.playbackPlaylistID = defaultPlaylistID
	a.current = 0

	data, err := json.Marshal(a.playlistStateSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var decoded playlistState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	b := newPlaylistTestApp()
	b.applyPlaylistState(decoded)

	if b.activePlaylistID != "custom-id" || b.playbackPlaylistID != defaultPlaylistID {
		t.Fatalf("playlist contexts were not restored: active=%q playback=%q", b.activePlaylistID, b.playbackPlaylistID)
	}
	if len(b.playlists) != 2 || b.playlists[1].Name != "Review" || b.playlists[1].Scroll != 3 {
		t.Fatalf("custom playlist did not round-trip: %#v", b.playlists)
	}
	if b.restoreCurrentPath != defaultPath {
		t.Fatalf("playback track was not restored: %q", b.restoreCurrentPath)
	}
}

func TestOfflinePlaybackContextSurvivesSnapshot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "offline.flac")
	a := newPlaylistTestApp()
	a.playlists[0].Tracks = []Track{{Path: missing, Name: "offline", Missing: true}}
	a.restoreCurrentPath = missing

	state := a.playlistStateSnapshot()
	if state.CurrentPath != missing {
		t.Fatalf("offline playback context was lost: %q", state.CurrentPath)
	}

	b := newPlaylistTestApp()
	b.applyPlaylistState(state)
	if b.restoreCurrentPath != missing || !b.playlists[0].Tracks[0].Missing {
		t.Fatalf("offline playback context did not round-trip: %#v", b)
	}
}

func TestApplyPlaylistStateRepairsDefaultAndInvalidReferences(t *testing.T) {
	a := newPlaylistTestApp()
	a.applyPlaylistState(playlistState{
		Version:            2,
		ActivePlaylistID:   "missing",
		PlaybackPlaylistID: "missing",
		Playlists: []playlistEntryState{
			{ID: "custom", Name: "PLAYLIST"},
		},
	})

	if len(a.playlists) != 2 || a.playlists[0].ID != defaultPlaylistID {
		t.Fatalf("default playlist was not repaired: %#v", a.playlists)
	}
	if strings.EqualFold(a.playlists[1].Name, "PLAYLIST") {
		t.Fatalf("reserved custom name was not repaired: %q", a.playlists[1].Name)
	}
	if a.activePlaylistID != defaultPlaylistID || a.playbackPlaylistID != defaultPlaylistID {
		t.Fatalf("invalid context references did not fall back to default")
	}
}

func TestPlaylistNameValidation(t *testing.T) {
	a := newPlaylistTestApp()
	a.playlists = append(a.playlists, Playlist{ID: "review", Name: "Review", Selected: -1})

	invalid := []string{"", "   ", "playlist", "REVIEW", strings.Repeat("界", 65)}
	for _, name := range invalid {
		if _, reason := a.validatePlaylistName(name, ""); reason == "" {
			t.Errorf("expected %q to be rejected", name)
		}
	}
	if got, reason := a.validatePlaylistName("  游戏音频 QA  ", ""); reason != "" || got != "游戏音频 QA" {
		t.Fatalf("valid Unicode name was not normalized: got=%q reason=%q", got, reason)
	}
	if got, reason := a.validatePlaylistName("Review", "review"); reason != "" || got != "Review" {
		t.Fatalf("renaming without changing the current name should be valid: got=%q reason=%q", got, reason)
	}
}

func TestSamePathCanExistAcrossPlaylists(t *testing.T) {
	path := testAudioPath(t, "shared.opus")
	a := newPlaylistTestApp()
	a.applyPlaylistState(playlistState{
		Version: 2,
		Playlists: []playlistEntryState{
			{ID: defaultPlaylistID, Name: "PLAYLIST", Paths: []string{path, path}},
			{ID: "custom", Name: "Custom", Paths: []string{path}},
		},
	})
	if len(a.playlists[0].Tracks) != 1 || len(a.playlists[1].Tracks) != 1 {
		t.Fatalf("same-list dedupe or cross-list duplication policy is incorrect: %#v", a.playlists)
	}
}

func TestBoundedPlaylistIndexDoesNotWrap(t *testing.T) {
	tests := []struct {
		current int
		count   int
		steps   int
		want    int
	}{
		{current: 0, count: 3, steps: -1, want: 0},
		{current: 0, count: 3, steps: 1, want: 1},
		{current: 1, count: 3, steps: -1, want: 0},
		{current: 1, count: 3, steps: 1, want: 2},
		{current: 2, count: 3, steps: 1, want: 2},
		{current: 2, count: 3, steps: 10, want: 2},
		{current: 0, count: 0, steps: 1, want: -1},
	}
	for _, test := range tests {
		if got := boundedPlaylistIndex(test.current, test.count, test.steps); got != test.want {
			t.Errorf("boundedPlaylistIndex(%d, %d, %d) = %d, want %d", test.current, test.count, test.steps, got, test.want)
		}
	}
}

// A live multi-selection is a runtime-only concept: the persisted snapshot must
// keep describing exactly one selected row so playlist.json stays backward
// compatible with every earlier build.
func TestPlaylistSnapshotKeepsSingleSelectionSchema(t *testing.T) {
	path := testAudioPath(t, "schema.wav")
	a := newPlaylistTestApp()
	a.playlists[0].Tracks = []Track{{Path: path, Name: "schema"}}
	a.playlists[0].Selected = 0
	a.playlistSel.clickSingle(defaultPlaylistID, 0)
	a.playlistSel.clickToggle(defaultPlaylistID, 1, 0)

	data, err := json.Marshal(a.playlistStateSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if got, want := jsonKeys(raw), []string{"active_playlist_id", "playback_playlist_id", "playlists", "version"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("playlist state keys = %v, want %v", got, want)
	}
	lists, ok := raw["playlists"].([]any)
	if !ok || len(lists) != 1 {
		t.Fatalf("playlists payload is not a single entry: %#v", raw["playlists"])
	}
	entry, ok := lists[0].(map[string]any)
	if !ok {
		t.Fatalf("playlist entry is not an object: %#v", lists[0])
	}
	if got, want := jsonKeys(entry), []string{"id", "name", "paths", "selected_path"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("playlist entry keys = %v, want %v", got, want)
	}
	if entry["selected_path"] != path {
		t.Fatalf("selected_path = %v, want %q", entry["selected_path"], path)
	}
	if _, leaked := entry["selected_paths"]; leaked {
		t.Fatal("multi-selection leaked into the persisted schema")
	}
}
