//go:build windows

package main

import (
	"reflect"
	"testing"
)

func playlistSelectionApp(id string, trackCount int, selected int) *App {
	p := Playlist{ID: id, Name: "PLAYLIST", Selected: selected}
	for i := 0; i < trackCount; i++ {
		p.Tracks = append(p.Tracks, Track{Path: "track" + string(rune('a'+i)) + ".wav"})
	}
	return &App{playlists: []Playlist{p}, activePlaylistID: id, playbackPlaylistID: id, current: -1}
}

func TestPlaylistSelectionWithoutMultiSelectionMatchesSingleSelection(t *testing.T) {
	a := playlistSelectionApp("pl", 6, 4)
	if got, want := a.selectedRows(), []int{4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedRows() = %v, want %v", got, want)
	}
	a.playlistSel.clickSingle("pl", 2)
	if a.playlistMultiSelectActive() {
		t.Fatalf("a plain click must not report a multi-selection")
	}
	a.playlists[0].Selected = 2
	if got, want := a.selectedRows(), []int{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedRows() after single click = %v, want %v", got, want)
	}
}

func TestPlaylistSelectionToggleSeedsAndRemoves(t *testing.T) {
	var s playlistSelection
	s.clickSingle("pl", 1)
	// The first Ctrl+click must extend the existing single selection instead of
	// replacing it.
	s.clickToggle("pl", 4, 1)
	if !s.activeFor("pl") {
		t.Fatalf("two toggled rows should be an active multi-selection")
	}
	if got, want := s.rowsFor("pl", 10, 4), []int{1, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rowsFor() = %v, want %v", got, want)
	}
	if !s.containsRow("pl", 1) || s.containsRow("pl", 4) == false {
		t.Fatalf("toggled rows are not reported as members")
	}

	s.clickToggle("pl", 4, 4)
	if only, ok := s.collapse(); !ok || only != 1 {
		t.Fatalf("collapse() = %d, %v; want 1, true", only, ok)
	}
}

func TestPlaylistSelectionToggleKeepsRowsSorted(t *testing.T) {
	var s playlistSelection
	s.clickSingle("pl", 7)
	s.clickToggle("pl", 2, 7)
	s.clickToggle("pl", 5, 2)
	if got, want := s.rowsFor("pl", 10, 5), []int{2, 5, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rowsFor() = %v, want %v", got, want)
	}
}

func TestPlaylistSelectionRangeForwardAndBackward(t *testing.T) {
	forward := playlistSelectionApp("pl", 10, 3)
	forward.playlistSel.clickSingle("pl", 3)
	forward.playlistSel.clickRange("pl", 6, 3)
	forward.playlists[0].Selected = 6
	if got, want := forward.selectedRows(), []int{3, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("forward range = %v, want %v", got, want)
	}

	backward := playlistSelectionApp("pl", 10, 6)
	backward.playlistSel.clickSingle("pl", 6)
	backward.playlistSel.clickRange("pl", 2, 6)
	backward.playlists[0].Selected = 2
	if got, want := backward.selectedRows(), []int{2, 3, 4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backward range = %v, want %v", got, want)
	}

	// The anchor stays on the last plain-clicked row, so a follow-up
	// Shift+click moves the range instead of drifting from the newest row.
	backward.playlistSel.clickRange("pl", 4, 2)
	backward.playlists[0].Selected = 4
	if got, want := backward.selectedRows(), []int{4, 5, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("re-aimed range = %v, want %v", got, want)
	}
}

func TestPlaylistSelectionOwnershipMismatchInvalidates(t *testing.T) {
	var s playlistSelection
	s.clickSingle("a", 1)
	s.clickToggle("a", 3, 1)
	if !s.activeFor("a") {
		t.Fatalf("selection for playlist a should be active")
	}
	if s.activeFor("b") || s.containsRow("b", 1) {
		t.Fatalf("selection leaked into another playlist")
	}
	if got, want := s.rowsFor("b", 10, 4), []int{4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rowsFor() for another playlist = %v, want %v", got, want)
	}

	// A toggle from a different playlist must start over rather than reuse rows.
	s.clickToggle("b", 6, 6)
	if s.containsRow("b", 1) || s.containsRow("a", 3) {
		t.Fatalf("stale rows survived a playlist switch: %#v", s.rows)
	}
}

func TestPlaylistSelectionRowsFilterOutOfRange(t *testing.T) {
	a := playlistSelectionApp("pl", 5, 1)
	a.playlistSel.clickSingle("pl", 1)
	a.playlistSel.clickToggle("pl", 4, 1)
	a.playlistSel.clickToggle("pl", 99, 4)
	if got, want := a.selectedRows(), []int{1, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("out-of-range row was not filtered: %v", got)
	}

	// Only out-of-range rows left: fall back to the single selection.
	a.playlists[0].Selected = 4
	a.playlistSel.clickSingle("pl", 88)
	a.playlistSel.clickToggle("pl", 99, 88)
	if got, want := a.selectedRows(), []int{4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedRows() = %v, want %v", got, want)
	}
}

func TestPlaylistSelectionResetFallsBackToAnchor(t *testing.T) {
	a := playlistSelectionApp("pl", 8, 5)
	a.playlistSel.clickSingle("pl", 3)
	a.playlistSel.clickToggle("pl", 6, 3)
	if !a.playlistMultiSelectActive() {
		t.Fatalf("expected a multi-selection before reset")
	}
	a.playlistSel.reset()
	if a.playlistMultiSelectActive() {
		t.Fatalf("reset() left a live multi-selection")
	}
	if got, want := a.selectedRows(), []int{5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedRows() after reset = %v, want %v", got, want)
	}
}

func TestPlaylistSelectionSelectedRowsOnEmptyApp(t *testing.T) {
	var a App
	if got := a.selectedRows(); got != nil {
		t.Fatalf("selectedRows() = %v, want nil", got)
	}
	if a.playlistMultiSelectActive() {
		t.Fatalf("empty app must not report a multi-selection")
	}
}

func TestSelectedTrackPathsFollowsSelectionOrder(t *testing.T) {
	a := playlistSelectionApp("pl", 5, 0)
	a.playlistSel.clickSingle("pl", 3)
	a.playlistSel.clickToggle("pl", 1, 3)
	a.playlists[0].Selected = 1
	got := a.selectedTrackPaths()
	want := []string{a.playlists[0].Tracks[1].Path, a.playlists[0].Tracks[3].Path}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedTrackPaths() = %v, want %v", got, want)
	}
}
