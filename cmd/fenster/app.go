package main

import (
	"fmt"
	"strings"
	"time"
)

// suggestName is the prefilled text of the save dialog. When setupLabel
// carries a monitor description (e.g. "2 Monitore · 5120×1440 + 1710×1073"),
// only the leading count/noun part is reused as the prefix, joined to the
// timestamp with the same middot the setup label itself uses. Without a
// label (an empty monitor arrangement), the generic prefix "Layout" is
// joined to the timestamp with a plain space instead.
func suggestName(setupLabel string, now time.Time) string {
	timestamp := now.Format("02.01. 15:04")
	if setupLabel == "" {
		return fmt.Sprintf("Layout %s", timestamp)
	}
	prefix := setupLabel
	if i := strings.Index(setupLabel, " · "); i > 0 {
		prefix = setupLabel[:i]
	}
	return fmt.Sprintf("%s · %s", prefix, timestamp)
}

// restoreMessage is the balloon text after a restore. deselected and missing
// are reported separately (I3): deselected counts entries excluded by an
// unticked checkbox — a deliberate choice, persisted in layouts.json — while
// missing counts entries whose window could not be found among the ones
// currently open. Folding both into one bucket, as an earlier version of
// this function did, made unticking windows the user deliberately kept
// closed indistinguishable from those windows simply not being open,
// contradicting the whole point of persisted checkmarks.
func restoreMessage(restored, deselected, missing, failed int) string {
	var b strings.Builder
	if restored == 0 {
		b.WriteString("Kein Fenster wiederhergestellt")
	} else {
		fmt.Fprintf(&b, "%d Fenster wiederhergestellt", restored)
	}
	if deselected > 0 {
		fmt.Fprintf(&b, ", %d abgewählt", deselected)
	}
	if missing > 0 {
		fmt.Fprintf(&b, ", %d übersprungen (nicht offen)", missing)
	}
	if failed > 0 {
		fmt.Fprintf(&b, ", %d fehlgeschlagen", failed)
	}
	return b.String()
}

// unavailableMessage is the balloon shown when a sync finds combinations
// Windows will not grant. It counts layouts, not key presses, and is only
// shown for hotkeys that became unavailable since the last sync — a
// standing conflict is not worth a balloon on every menu click.
func unavailableMessage(n int) string {
	if n == 1 {
		return "Ein Hotkey ist belegt und derzeit ohne Funktion"
	}
	return fmt.Sprintf("%d Hotkeys sind belegt und derzeit ohne Funktion", n)
}
