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
		restored, skipped, failed int
		want                      string
	}{
		{7, 0, 0, "7 Fenster wiederhergestellt"},
		{1, 0, 0, "1 Fenster wiederhergestellt"},
		{7, 2, 0, "7 Fenster wiederhergestellt, 2 übersprungen (nicht offen)"},
		{7, 2, 1, "7 Fenster wiederhergestellt, 2 übersprungen (nicht offen), 1 fehlgeschlagen"},
		{0, 3, 0, "Kein Fenster wiederhergestellt, 3 übersprungen (nicht offen)"},
	}
	for _, tc := range tests {
		if got := restoreMessage(tc.restored, tc.skipped, tc.failed); got != tc.want {
			t.Errorf("restoreMessage(%d,%d,%d) = %q, want %q", tc.restored, tc.skipped, tc.failed, got, tc.want)
		}
	}
}
