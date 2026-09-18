package layout

import (
	"math"
	"testing"

	"fenster/internal/store"
)

func entry(exe, title string, ordinal int) store.WindowEntry {
	return store.WindowEntry{
		Exe:     exe,
		Title:   title,
		Ordinal: ordinal,
		Rect:    store.Rect{X: 0, Y: 0, W: 800, H: 600},
		State:   store.StateNormal,
		Include: true,
	}
}

func liveAt(handle uintptr, exe, title string) Live {
	w := live(title, "C", exe)
	w.Handle = handle
	return w
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"● main.go - Editor", "main.go"},
		{"* main.go — Editor", "main.go"},
		{"◐ Go Fensterposition", "go fensterposition"},
		{"  Inbox - Outlook  ", "inbox"},
		{"Plain", "plain"},
	}
	for _, tc := range tests {
		if got := NormalizeTitle(tc.in); got != tc.want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	if got := Similarity("abc", "abc"); got != 1 {
		t.Errorf("identical strings: got %v, want 1", got)
	}
	if got := Similarity("", ""); got != 1 {
		t.Errorf("two empty strings: got %v, want 1", got)
	}
	if got := Similarity("abc", ""); got != 0 {
		t.Errorf("one empty string: got %v, want 0", got)
	}
	if got := Similarity("kitten", "sitting"); math.Abs(got-4.0/7.0) > 0.001 {
		t.Errorf("kitten/sitting: got %v, want ~0.571", got)
	}
}

func TestMatchExactTitleWins(t *testing.T) {
	entries := []store.WindowEntry{entry(`C:\a\editor.exe`, "main.go - Editor", 0)}
	windows := []Live{
		liveAt(1, `C:\a\editor.exe`, "other.go - Editor"),
		liveAt(2, `C:\a\editor.exe`, "main.go - Editor"),
	}

	plan := MatchEntries(entries, windows)
	if len(plan.Matches) != 1 || plan.Matches[0].Live.Handle != 2 {
		t.Fatalf("expected the exact title match, got %+v", plan.Matches)
	}
	if plan.Matches[0].Index != 0 {
		t.Errorf("Index = %d, want 0", plan.Matches[0].Index)
	}
}

func TestMatchFallsBackToSimilarTitle(t *testing.T) {
	entries := []store.WindowEntry{entry(`C:\a\editor.exe`, "main.go - Editor", 0)}
	windows := []Live{liveAt(1, `C:\a\editor.exe`, "● main.go - Editor")}

	plan := MatchEntries(entries, windows)
	if len(plan.Matches) != 1 || plan.Matches[0].Live.Handle != 1 {
		t.Fatalf("modified-marker title should still match, got %+v", plan.Matches)
	}
}

func TestMatchFallsBackToOrdinal(t *testing.T) {
	entries := []store.WindowEntry{
		entry(`C:\a\editor.exe`, "alpha", 0),
		entry(`C:\a\editor.exe`, "beta", 1),
	}
	windows := []Live{
		liveAt(1, `C:\a\editor.exe`, "completely different"),
		liveAt(2, `C:\a\editor.exe`, "nothing alike either"),
	}

	plan := MatchEntries(entries, windows)
	if len(plan.Matches) != 2 {
		t.Fatalf("both entries should match by ordinal, got %+v", plan.Matches)
	}
	if plan.Matches[0].Live.Handle != 1 || plan.Matches[1].Live.Handle != 2 {
		t.Errorf("ordinal order not respected: %+v", plan.Matches)
	}
}

func TestEachLiveWindowIsConsumedOnce(t *testing.T) {
	entries := []store.WindowEntry{
		entry(`C:\a\editor.exe`, "same title", 0),
		entry(`C:\a\editor.exe`, "same title", 1),
	}
	windows := []Live{liveAt(1, `C:\a\editor.exe`, "same title")}

	plan := MatchEntries(entries, windows)
	if len(plan.Matches) != 1 {
		t.Fatalf("one live window can satisfy only one entry, got %d matches", len(plan.Matches))
	}
	if len(plan.Missing) != 1 || plan.Missing[0].Ordinal != 1 {
		t.Errorf("the second entry should be reported missing, got %+v", plan.Missing)
	}
}

func TestMatchNeverCrossesExecutables(t *testing.T) {
	entries := []store.WindowEntry{entry(`C:\a\editor.exe`, "Inbox", 0)}
	windows := []Live{liveAt(1, `C:\b\mail.exe`, "Inbox")}

	plan := MatchEntries(entries, windows)
	if len(plan.Matches) != 0 || len(plan.Missing) != 1 {
		t.Errorf("entries must never match another executable: %+v", plan)
	}
}

func TestMatchIgnoresExecutablePathCase(t *testing.T) {
	entries := []store.WindowEntry{entry(`C:\A\Editor.exe`, "Editor", 0)}
	windows := []Live{liveAt(1, `c:\a\editor.exe`, "Editor")}

	if plan := MatchEntries(entries, windows); len(plan.Matches) != 1 {
		t.Errorf("executable comparison should be case-insensitive: %+v", plan)
	}
}

func TestMatchSkipsIneligibleLiveWindows(t *testing.T) {
	entries := []store.WindowEntry{entry(`C:\a\editor.exe`, "Editor", 0)}
	hidden := liveAt(1, `C:\a\editor.exe`, "Editor")
	hidden.Visible = false

	if plan := MatchEntries(entries, []Live{hidden}); len(plan.Missing) != 1 {
		t.Errorf("hidden windows are not restore targets: %+v", plan)
	}
}
