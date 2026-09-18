// Package layout holds the domain logic: which windows are worth saving, how a
// saved entry is matched back to a running window, and how a target rectangle
// is kept on screen. It works on plain data so it can be tested without a
// desktop.
package layout

import (
	"strings"

	"fenster/internal/store"
)

// Live is the runtime view of one top-level window.
type Live struct {
	Handle  uintptr
	Title   string
	Class   string
	Exe     string
	Rect    store.Rect // restored rectangle, even when the window is maximized
	Screen  store.Rect // actual on-screen rectangle (GetWindowRect); differs from Rect for a snapped window
	State   store.WindowState
	Topmost bool
	Visible bool
	Cloaked bool
	Tool    bool // WS_EX_TOOLWINDOW
	Own     bool // belongs to our own process
}

// shellClasses are desktop and taskbar windows that must never be captured.
var shellClasses = map[string]bool{
	"Progman":       true,
	"WorkerW":       true,
	"Shell_TrayWnd": true,
	"Button":        true,
}

// Eligible reports whether a window belongs in a saved layout.
func Eligible(w Live) bool {
	switch {
	case !w.Visible, w.Cloaked, w.Tool, w.Own:
		return false
	case strings.TrimSpace(w.Title) == "":
		return false
	case w.Exe == "":
		return false
	case shellClasses[w.Class]:
		return false
	case w.Rect.W <= 0 || w.Rect.H <= 0:
		// Both GetWindowPlacement and the GetWindowRect fallback failed in
		// win32.describeWindow, leaving a zero rectangle. Saving that would
		// later restore a real window to a 0x0 rect at the origin.
		return false
	}
	return true
}

// Capture turns the eligible live windows into saved entries, numbering the
// windows of each executable in enumeration order.
func Capture(live []Live) []store.WindowEntry {
	ordinals := map[string]int{}
	out := make([]store.WindowEntry, 0, len(live))
	for _, w := range live {
		if !Eligible(w) {
			continue
		}
		key := strings.ToLower(w.Exe)
		out = append(out, store.WindowEntry{
			Exe:     w.Exe,
			Class:   w.Class,
			Title:   w.Title,
			Ordinal: ordinals[key],
			Rect:    w.Rect,
			Screen:  w.Screen,
			State:   w.State,
			Topmost: w.Topmost,
			Include: true,
		})
		ordinals[key]++
	}
	return out
}
