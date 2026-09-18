package main

import (
	"testing"
	"time"
)

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
		restored, deselected, missing, failed int
		want                                  string
	}{
		// Nothing skipped.
		{7, 0, 0, 0, "7 Fenster wiederhergestellt"},
		{1, 0, 0, 0, "1 Fenster wiederhergestellt"},
		// Only deselected: must never read as "nicht offen" — that was I3.
		{5, 5, 0, 0, "5 Fenster wiederhergestellt, 5 abgewählt"},
		// Only missing.
		{7, 0, 2, 0, "7 Fenster wiederhergestellt, 2 übersprungen (nicht offen)"},
		// Both deselected and missing, in the same restore.
		{5, 3, 2, 0, "5 Fenster wiederhergestellt, 3 abgewählt, 2 übersprungen (nicht offen)"},
		{5, 3, 2, 1, "5 Fenster wiederhergestellt, 3 abgewählt, 2 übersprungen (nicht offen), 1 fehlgeschlagen"},
		// Zero-restored case, with and without deselected windows.
		{0, 0, 3, 0, "Kein Fenster wiederhergestellt, 3 übersprungen (nicht offen)"},
		{0, 3, 3, 0, "Kein Fenster wiederhergestellt, 3 abgewählt, 3 übersprungen (nicht offen)"},
	}
	for _, tc := range tests {
		got := restoreMessage(tc.restored, tc.deselected, tc.missing, tc.failed)
		if got != tc.want {
			t.Errorf("restoreMessage(%d,%d,%d,%d) = %q, want %q",
				tc.restored, tc.deselected, tc.missing, tc.failed, got, tc.want)
		}
	}
}
