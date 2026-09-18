// Package monitors turns a monitor arrangement into a stable fingerprint used
// to decide which layouts fit the current setup. Matching is deliberately
// loose: only geometry counts, never device or adapter identity, so swapping a
// monitor for an identical model keeps layouts matching.
package monitors

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"fenster/internal/store"
)

// Fingerprint returns 8 hex characters identifying the arrangement.
func Fingerprint(ms []store.Monitor) string {
	sum := sha256.Sum256([]byte(canonical(ms)))
	return hex.EncodeToString(sum[:])[:8]
}

// Label returns a German description for menus, e.g.
// "2 Monitore · 5120×1440 + 1710×1073".
func Label(ms []store.Monitor) string {
	if len(ms) == 0 {
		return "Kein Monitor erkannt"
	}
	sorted := normalize(ms)
	sizes := make([]string, len(sorted))
	for i, m := range sorted {
		sizes[i] = fmt.Sprintf("%d×%d", m.W, m.H)
	}
	noun := "Monitore"
	if len(sorted) == 1 {
		noun = "Monitor"
	}
	return fmt.Sprintf("%d %s · %s", len(sorted), noun, strings.Join(sizes, " + "))
}

// Describe returns the setup record stored with a layout.
func Describe(ms []store.Monitor) store.Setup {
	out := make([]store.Monitor, len(ms))
	copy(out, ms)
	return store.Setup{Fingerprint: Fingerprint(ms), Label: Label(ms), Monitors: out}
}

// canonical renders the arrangement as a deterministic string: coordinates
// relative to the primary monitor, sorted top-to-bottom then left-to-right,
// the primary marked with a leading asterisk.
func canonical(ms []store.Monitor) string {
	sorted := normalize(ms)
	parts := make([]string, len(sorted))
	for i, m := range sorted {
		prefix := ""
		if m.Primary {
			prefix = "*"
		}
		parts[i] = fmt.Sprintf("%s%dx%d+%d+%d@%d", prefix, m.W, m.H, m.X, m.Y, m.Scale)
	}
	return strings.Join(parts, ";")
}

// normalize copies the monitors, translates them so the primary monitor's
// top-left corner is the origin, and sorts them by position. If no monitor
// is flagged primary, falls back to the topmost-leftmost monitor.
func normalize(ms []store.Monitor) []store.Monitor {
	out := make([]store.Monitor, len(ms))
	copy(out, ms)

	// A total order: every field that can differ between two monitors is
	// compared, ending in Primary, so no two distinct monitors can ever tie.
	// sort.Slice is not stable, so leaving any tie possible (e.g. two
	// monitors sharing Y, X and W but differing only in H, as the old
	// three-key comparator did) let the canonical string — and therefore the
	// fingerprint — come out differently between runs on identical
	// hardware, silently breaking layout matching with nothing in the UI to
	// explain it.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		if out[i].W != out[j].W {
			return out[i].W < out[j].W
		}
		if out[i].H != out[j].H {
			return out[i].H < out[j].H
		}
		if out[i].Scale != out[j].Scale {
			return out[i].Scale < out[j].Scale
		}
		// false < true, so a non-primary monitor sorts before a primary one
		// in the (only realistic) case that everything else still ties.
		return !out[i].Primary && out[j].Primary
	})

	var originX, originY int32
	hasPrimary := false
	// Use primary monitor if found, otherwise use topmost-leftmost.
	for _, m := range out {
		if m.Primary {
			originX, originY = m.X, m.Y
			hasPrimary = true
			break
		}
	}
	if !hasPrimary && len(out) > 0 {
		// No primary monitor; use first (topmost-leftmost after sort).
		originX, originY = out[0].X, out[0].Y
	}

	for i := range out {
		out[i].X -= originX
		out[i].Y -= originY
	}
	return out
}
