//go:build windows

package main

import "sort"

// playlistSelection keeps a runtime-only multi-row selection for the active
// playlist. It is deliberately not persisted: playlist.json still stores a
// single selected path, and any structural change of the list collapses the
// set back to one row. That keeps index bookkeeping out of the feature.
type playlistSelection struct {
	playlistID string
	rows       map[int]bool
	anchor     int
}

func (s *playlistSelection) reset() {
	s.playlistID = ""
	s.rows = nil
	s.anchor = -1
}

// clickSingle collapses the selection onto idx, the plain-click behaviour.
func (s *playlistSelection) clickSingle(playlistID string, idx int) {
	s.playlistID = playlistID
	s.rows = nil
	s.anchor = idx
}

// clickToggle adds or removes idx while keeping the rest of the set, the
// Ctrl+click behaviour. previousSelected seeds the set so that the first
// Ctrl+click extends the current single selection instead of replacing it.
func (s *playlistSelection) clickToggle(playlistID string, idx, previousSelected int) {
	if s.playlistID != playlistID {
		s.reset()
		s.playlistID = playlistID
	}
	if s.rows == nil {
		s.rows = make(map[int]bool)
		if previousSelected >= 0 && previousSelected != idx {
			s.rows[previousSelected] = true
		}
	}
	if s.rows[idx] {
		delete(s.rows, idx)
	} else {
		s.rows[idx] = true
	}
	s.anchor = idx
}

// clickRange selects the closed interval between the anchor and idx, the
// Shift+click behaviour. previousSelected is used when no anchor is known yet
// so that a Shift+click right after a programmatic selection still works.
func (s *playlistSelection) clickRange(playlistID string, idx, previousSelected int) {
	if s.playlistID != playlistID {
		s.rows = nil
		s.anchor = -1
	}
	s.playlistID = playlistID
	if s.anchor < 0 {
		s.anchor = previousSelected
	}
	if s.anchor < 0 {
		s.clickSingle(playlistID, idx)
		return
	}
	lo, hi := s.anchor, idx
	if lo > hi {
		lo, hi = hi, lo
	}
	rows := make(map[int]bool, hi-lo+1)
	for i := lo; i <= hi; i++ {
		rows[i] = true
	}
	s.rows = rows
}

// collapse reports the single member that is left once a toggle dropped the
// set back to one row, so the caller can sync Playlist.Selected and forget the
// set entirely.
func (s *playlistSelection) collapse() (int, bool) {
	if len(s.rows) == 1 {
		for idx := range s.rows {
			return idx, true
		}
	}
	return -1, false
}

func (s *playlistSelection) activeFor(playlistID string) bool {
	return s.playlistID == playlistID && len(s.rows) > 1
}

func (s *playlistSelection) containsRow(playlistID string, idx int) bool {
	return s.playlistID == playlistID && s.rows[idx]
}

// rowsFor returns the rows the user currently operates on. Without a live
// multi-row set this is exactly the historic single selection, which keeps
// every caller behaviourally identical to before when nothing is multi-selected.
func (s *playlistSelection) rowsFor(playlistID string, trackCount, fallbackSelected int) []int {
	if s.activeFor(playlistID) {
		rows := make([]int, 0, len(s.rows))
		for idx := range s.rows {
			if idx >= 0 && idx < trackCount {
				rows = append(rows, idx)
			}
		}
		if len(rows) > 1 {
			sort.Ints(rows)
			return rows
		}
	}
	if fallbackSelected >= 0 && fallbackSelected < trackCount {
		return []int{fallbackSelected}
	}
	return nil
}

// selectedRows is the single read path for "which tracks does this action
// apply to" across drag-out, delete and the context menu.
func (a *App) selectedRows() []int {
	p := a.activePlaylist()
	if p == nil {
		return nil
	}
	return a.playlistSel.rowsFor(p.ID, len(p.Tracks), p.Selected)
}

func (a *App) playlistMultiSelectActive() bool {
	p := a.activePlaylist()
	return p != nil && a.playlistSel.activeFor(p.ID)
}

// applyPlaylistRowClick routes a left click inside the playlist panel through
// the Ctrl / Shift selection rules.
func (a *App) applyPlaylistRowClick(p *Playlist, idx int) {
	switch {
	case keyDown(VK_SHIFT):
		a.playlistSel.clickRange(p.ID, idx, p.Selected)
	case keyDown(VK_CONTROL):
		a.playlistSel.clickToggle(p.ID, idx, p.Selected)
		if only, ok := a.playlistSel.collapse(); ok {
			a.playlistSel.reset()
			p.Selected = only
			return
		}
	default:
		a.playlistSel.clickSingle(p.ID, idx)
	}
	p.Selected = idx
}

// selectedTrackPaths returns the file paths of the current selection in row
// order, skipping empty entries.
func (a *App) selectedTrackPaths() []string {
	p := a.activePlaylist()
	if p == nil {
		return nil
	}
	rows := a.selectedRows()
	paths := make([]string, 0, len(rows))
	for _, idx := range rows {
		if path := p.Tracks[idx].Path; path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
