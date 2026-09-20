package main

import (
	"testing"
	"time"

	"fenster/internal/layout"
	"fenster/internal/store"
)

var restoreMonitors = []store.Monitor{
	{X: 0, Y: 0, W: 1920, H: 1080, Scale: 100, Primary: true},
}

// TestClampEntryClampsBothRectangles pins the snapped-window fix's restore
// path: both the restored rect and the screen rect must be clamped against
// the current monitors independently, so a layout restored on a foreign
// monitor setup lands on screen either way.
func TestClampEntryClampsBothRectangles(t *testing.T) {
	e := store.WindowEntry{
		Rect:   store.Rect{X: 9000, Y: 9000, W: 800, H: 600},
		Screen: store.Rect{X: 9500, Y: 9500, W: 700, H: 500},
	}
	rect, screen := clampEntry(e, restoreMonitors)

	if rect.X >= 9000 || rect.Y >= 9000 {
		t.Errorf("Rect not clamped: %+v", rect)
	}
	if screen.X >= 9500 || screen.Y >= 9500 {
		t.Errorf("Screen not clamped: %+v", screen)
	}
}

// TestClampEntryLeavesAnAbsentScreenAlone pins backward compatibility: an
// entry from an old layout file with no Screen (zero value) must stay zero
// rather than being "clamped" into a bogus on-screen rectangle at (0,0).
func TestClampEntryLeavesAnAbsentScreenAlone(t *testing.T) {
	e := store.WindowEntry{
		Rect: store.Rect{X: 100, Y: 100, W: 800, H: 600},
	}
	_, screen := clampEntry(e, restoreMonitors)
	if screen != (store.Rect{}) {
		t.Errorf("Screen = %+v, want zero value left untouched", screen)
	}
}

func TestSuggestName(t *testing.T) {
	now := time.Date(2026, 9, 18, 13, 40, 0, 0, time.UTC)
	got := suggestName("2 Monitore · 5120×1440 + 1710×1073", now)
	want := "2 Monitore · 18.09. 13:40"
	if got != want {
		t.Errorf("suggestName = %q, want %q", got, want)
	}
}

func TestSuggestNameWithoutASetupLabel(t *testing.T) {
	now := time.Date(2026, 9, 18, 13, 40, 0, 0, time.UTC)
	if got, want := suggestName("", now), "Layout 18.09. 13:40"; got != want {
		t.Errorf("suggestName = %q, want %q", got, want)
	}
}

func TestRestoreMessage(t *testing.T) {
	tests := []struct {
		restored, deselected, missing, minimized, failed int
		want                                             string
	}{
		// Nothing skipped.
		{7, 0, 0, 0, 0, "7 Fenster wiederhergestellt"},
		{1, 0, 0, 0, 0, "1 Fenster wiederhergestellt"},
		// Only deselected: must never read as "nicht offen" — that was I3.
		{5, 5, 0, 0, 0, "5 Fenster wiederhergestellt, 5 abgewählt"},
		// Only missing.
		{7, 0, 2, 0, 0, "7 Fenster wiederhergestellt, 2 übersprungen (nicht offen)"},
		// Both deselected and missing, in the same restore.
		{5, 3, 2, 0, 0, "5 Fenster wiederhergestellt, 3 abgewählt, 2 übersprungen (nicht offen)"},
		{5, 3, 2, 0, 1, "5 Fenster wiederhergestellt, 3 abgewählt, 2 übersprungen (nicht offen), 1 fehlgeschlagen"},
		// Zero-restored case, with and without deselected windows.
		{0, 0, 3, 0, 0, "Kein Fenster wiederhergestellt, 3 übersprungen (nicht offen)"},
		{0, 3, 3, 0, 0, "Kein Fenster wiederhergestellt, 3 abgewählt, 3 übersprungen (nicht offen)"},
		// Minimizing extraneous windows is reported separately from every
		// other count: it is something done to windows the layout does not
		// contain, not an outcome for one it does.
		{4, 0, 0, 6, 0, "4 Fenster wiederhergestellt, 6 minimiert"},
		{4, 1, 2, 6, 3, "4 Fenster wiederhergestellt, 1 abgewählt, 2 übersprungen (nicht offen), 6 minimiert, 3 fehlgeschlagen"},
		{0, 0, 0, 2, 0, "Kein Fenster wiederhergestellt, 2 minimiert"},
		// Nothing extraneous was open, so the clause is absent entirely.
		{4, 0, 0, 0, 0, "4 Fenster wiederhergestellt"},
	}
	for _, tc := range tests {
		got := restoreMessage(tc.restored, tc.deselected, tc.missing, tc.minimized, tc.failed)
		if got != tc.want {
			t.Errorf("restoreMessage(%d,%d,%d,%d,%d) = %q, want %q",
				tc.restored, tc.deselected, tc.missing, tc.minimized, tc.failed, got, tc.want)
		}
	}
}

func TestUnavailableMessage(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "Ein Hotkey ist belegt und derzeit ohne Funktion"},
		{3, "3 Hotkeys sind belegt und derzeit ohne Funktion"},
	}
	for _, tc := range tests {
		if got := unavailableMessage(tc.n); got != tc.want {
			t.Errorf("unavailableMessage(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func minimizable(title string, state store.WindowState, handle uintptr) layout.Live {
	return layout.Live{Handle: handle, Title: title, Exe: `C:\a\x.exe`, State: state, Visible: true}
}

func TestWindowsToMinimize(t *testing.T) {
	unmatched := []layout.Live{
		minimizable("normal", store.StateNormal, 10),
		minimizable("already down", store.StateMinimized, 11),
		minimizable("maximized", store.StateMaximized, 12),
	}

	got := windowsToMinimize(unmatched, true)

	// An already-minimized window is skipped: the balloon reports what this
	// restore changed, not how many windows end up minimized.
	want := []uintptr{10, 12}
	if len(got) != len(want) {
		t.Fatalf("windowsToMinimize = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("handle %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestWindowsToMinimizeIsEmptyWhenDisabled(t *testing.T) {
	unmatched := []layout.Live{minimizable("normal", store.StateNormal, 10)}
	if got := windowsToMinimize(unmatched, false); len(got) != 0 {
		t.Errorf("windowsToMinimize(.., false) = %v, want nothing", got)
	}
}

func TestWindowsToMinimizeHandlesNothingExtraneous(t *testing.T) {
	if got := windowsToMinimize(nil, true); len(got) != 0 {
		t.Errorf("windowsToMinimize(nil, true) = %v, want nothing", got)
	}
}

func TestWantsQuit(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"fenster.exe"}, false},
		{[]string{"fenster.exe", "-quit"}, true},
		{[]string{"fenster.exe", "--quit"}, true},
		{[]string{"fenster.exe", "/quit"}, true},
		{[]string{"fenster.exe", "-QUIT"}, true},
		{[]string{"fenster.exe", "-something-else"}, false},
		{[]string{"fenster.exe", "quit"}, false},
		{nil, false},
	}
	for _, tc := range tests {
		if got := wantsQuit(tc.args); got != tc.want {
			t.Errorf("wantsQuit(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
