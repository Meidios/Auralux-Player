package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Set before the UI and audio workers start; language changes apply on restart.
type language int

const (
	simplified language = iota
	english
	traditional
)

var displayLanguage language
var languagePreference = "system"
var localeCodes = []string{"system", "en", "zh-Hans", "zh-Hant"}

func validPreference(s string) bool {
	for _, v := range localeCodes {
		if v == s {
			return true
		}
	}
	return false
}
func windowsDisplayLanguage() string {
	// GetUserDefaultUILanguage returns the UI language, not the region/number format.
	id, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	var buf [85]uint16
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("LCIDToLocaleName").Call(id, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n > 0 {
		return syscall.UTF16ToString(buf[:])
	}
	return "en"
}
func resolveLanguage(pref, system string) language {
	if !validPreference(pref) || pref == "system" {
		pref = system
	}
	s := strings.ToLower(strings.ReplaceAll(pref, "_", "-"))
	p := strings.Split(s, "-")
	if p[0] != "zh" {
		return english
	}
	for _, v := range p {
		switch v {
		case "hans":
			return simplified
		case "hant":
			return traditional
		}
	}
	for _, v := range p {
		switch v {
		case "tw", "hk", "mo":
			return traditional
		}
	}
	return simplified
}
func loadLanguage(dir, system string) {
	b, _ := os.ReadFile(filepath.Join(dir, "language.txt"))
	pref := strings.TrimSpace(string(b))
	if !validPreference(pref) {
		pref = "system"
	}
	languagePreference = pref
	displayLanguage = resolveLanguage(pref, system)
}
// Localize menus and dialogs. The player workspace uses English labels directly.
func tr(key string) string {
	if row, ok := messages[key]; ok {
		return row[displayLanguage]
	}
	return key
}
func playlistDisplayName(p Playlist) string {
	if p.ID == defaultPlaylistID {
		return tr("Default playlist")
	}
	return p.Name
}
func (a *App) selectLanguage(code string) {
	if !validPreference(code) || code == languagePreference {
		return
	}
	dir := stateConfigDir()
	err := os.MkdirAll(dir, 0755)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "language.txt"), []byte(code+"\n"), 0644)
	}
	if err != nil {
		messageBox(a.hwnd, tr("无法保存设置：")+err.Error(), appTitle, MB_OK|MB_ICONERROR)
		return
	}
	languagePreference = code
	messageBox(a.hwnd, tr("Language saved. The change takes effect the next time you open the player."), tr("Language"), MB_OK)
}
func appendLanguageMenu(parent uintptr) {
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	labels := []string{tr("Follow Windows"), "English", "简体中文", "繁體中文"}
	for i, code := range localeCodes {
		flags := uintptr(MF_STRING)
		if languagePreference == code {
			flags |= MF_CHECKED
		}
		procAppendMenuW.Call(m, flags, uintptr(6200+i), uintptr(unsafe.Pointer(utf16Ptr(labels[i]))))
	}
	procAppendMenuW.Call(parent, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(parent, 0x10, m, uintptr(unsafe.Pointer(utf16Ptr(tr("Language")+" / Language"))))
}
