package layout

import (
	"testing"

	"fenster/internal/store"
)

func live(title, class, exe string) Live {
	return Live{
		Handle:  1,
		Title:   title,
		Class:   class,
		Exe:     exe,
		Rect:    store.Rect{X: 10, Y: 20, W: 800, H: 600},
		State:   store.StateNormal,
		Visible: true,
	}
}

func TestEligible(t *testing.T) {
	tests := []struct {
		name string
		w    Live
		want bool
	}{
		{"ordinary window", live("Editor", "EditorClass", `C:\a\editor.exe`), true},
		{"invisible", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Visible = false; return w }(), false},
		{"cloaked", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Cloaked = true; return w }(), false},
		{"tool window", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Tool = true; return w }(), false},
		{"own process", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Own = true; return w }(), false},
		{"empty title", live("", "EditorClass", `C:\a\editor.exe`), false},
		{"blank title", live("   ", "EditorClass", `C:\a\editor.exe`), false},
		{"no exe", live("Editor", "EditorClass", ""), false},
		{"shell Progman", live("Program Manager", "Progman", `C:\Windows\explorer.exe`), false},
		{"shell WorkerW", live("x", "WorkerW", `C:\Windows\explorer.exe`), false},
		{"shell tray", live("x", "Shell_TrayWnd", `C:\Windows\explorer.exe`), false},
		{"shell button", live("Start", "Button", `C:\Windows\explorer.exe`), false},
		{"zero width rect", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Rect.W = 0; return w }(), false},
		{"zero height rect", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Rect.H = 0; return w }(), false},
		{"negative width rect", func() Live { w := live("Editor", "EditorClass", `C:\a\editor.exe`); w.Rect.W = -5; return w }(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Eligible(tc.w); got != tc.want {
				t.Errorf("Eligible = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCaptureFiltersAndNumbersOrdinalsPerExecutable(t *testing.T) {
	hidden := live("Hidden", "EditorClass", `C:\a\editor.exe`)
	hidden.Visible = false

	in := []Live{
		live("one - Editor", "EditorClass", `C:\a\editor.exe`),
		hidden,
		live("Inbox - Mail", "MailClass", `C:\b\mail.exe`),
		live("two - Editor", "EditorClass", `C:\a\editor.exe`),
	}

	got := Capture(in)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].Ordinal != 0 || got[0].Title != "one - Editor" {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[1].Ordinal != 0 || got[1].Exe != `C:\b\mail.exe` {
		t.Errorf("entry 1 = %+v", got[1])
	}
	if got[2].Ordinal != 1 || got[2].Title != "two - Editor" {
		t.Errorf("entry 2 = %+v, want the second editor window with ordinal 1", got[2])
	}
}

func TestCaptureCopiesGeometryStateAndDefaultsIncludeToTrue(t *testing.T) {
	w := live("Editor", "EditorClass", `C:\a\editor.exe`)
	w.State = store.StateMaximized
	w.Topmost = true
	w.Rect = store.Rect{X: -1700, Y: -1400, W: 1920, H: 1080}
	w.Screen = store.Rect{X: -1700, Y: -1400, W: 1920, H: 1080}

	got := Capture([]Live{w})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	want := store.WindowEntry{
		Exe:     `C:\a\editor.exe`,
		Class:   "EditorClass",
		Title:   "Editor",
		Ordinal: 0,
		Rect:    store.Rect{X: -1700, Y: -1400, W: 1920, H: 1080},
		Screen:  store.Rect{X: -1700, Y: -1400, W: 1920, H: 1080},
		State:   store.StateMaximized,
		Topmost: true,
		Include: true,
	}
	if got[0] != want {
		t.Errorf("Capture = %+v, want %+v", got[0], want)
	}
}

// TestCaptureCopiesScreenSeparatelyFromRect pins the snapped-window fix: a
// window snapped via Windows Snap keeps a pre-snap rcNormalPosition (Rect)
// that differs from what is actually on screen (Screen). Capture must not
// collapse the two into one value.
func TestCaptureCopiesScreenSeparatelyFromRect(t *testing.T) {
	w := live("Editor", "EditorClass", `C:\a\editor.exe`)
	w.Rect = store.Rect{X: 1512, Y: -932, W: 1022, H: 694}    // pre-snap, rcNormalPosition
	w.Screen = store.Rect{X: -46, Y: -1440, W: 3425, H: 1398} // actually on screen, snapped

	got := Capture([]Live{w})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Rect == got[0].Screen {
		t.Fatalf("Rect and Screen must be captured independently, got both = %+v", got[0].Rect)
	}
	if got[0].Rect != w.Rect {
		t.Errorf("Rect = %+v, want %+v", got[0].Rect, w.Rect)
	}
	if got[0].Screen != w.Screen {
		t.Errorf("Screen = %+v, want %+v", got[0].Screen, w.Screen)
	}
}
