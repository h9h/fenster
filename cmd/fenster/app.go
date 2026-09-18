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

// restoreMessage is the balloon text after a restore.
func restoreMessage(restored, skipped, failed int) string {
	var b strings.Builder
	if restored == 0 {
		b.WriteString("Kein Fenster wiederhergestellt")
	} else {
		fmt.Fprintf(&b, "%d Fenster wiederhergestellt", restored)
	}
	if skipped > 0 {
		fmt.Fprintf(&b, ", %d übersprungen (nicht offen)", skipped)
	}
	if failed > 0 {
		fmt.Fprintf(&b, ", %d fehlgeschlagen", failed)
	}
	return b.String()
}
