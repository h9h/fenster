package hotkey

import (
	"fmt"

	"fenster/internal/store"
)

// Binding is one layout's live key combination.
type Binding struct {
	LayoutID string
	Hotkey   Hotkey
}

// Active returns the bindings that should be registered right now: the
// layouts whose setup fingerprint matches, in store order, each with a
// non-empty, parseable combination. Only the current setup's layouts are
// selected, which is what lets Strg+Alt+1 mean the dock layout at the desk
// and the laptop layout on the road.
//
// The returned errors describe the state of the file, not the outcome of
// this call: an entry that cannot be parsed, or a combination a second
// layout of the same setup repeats (only reachable by hand-editing, since
// assignment rejects it). Such an entry is skipped — the first in store
// order wins a duplicate — rather than registered and left to fail
// silently. The caller logs them; there is nothing a user can act on in a
// balloon.
func Active(layouts []store.Layout, fingerprint string) ([]Binding, []error) {
	var (
		out  []Binding
		errs []error
		seen = map[Hotkey]string{}
	)
	for _, l := range layouts {
		if l.Setup.Fingerprint != fingerprint || l.Hotkey == "" {
			continue
		}
		hk, err := Parse(l.Hotkey)
		if err != nil {
			errs = append(errs, fmt.Errorf("layout %q (%s): hotkey %q: %w", l.Name, l.ID, l.Hotkey, err))
			continue
		}
		if hk.IsZero() {
			// Not unreachable: l.Hotkey == "" above does not catch a
			// whitespace-only stored value, and Parse("   ") returns the
			// zero Hotkey with a nil error. A hand-edited
			// "hotkey": "   " reaches this branch and must be treated as
			// "no hotkey", same as an empty string.
			continue
		}
		if firstID, dup := seen[hk]; dup {
			errs = append(errs, fmt.Errorf("layout %q (%s): hotkey %s is already used by layout %s in this setup", l.Name, l.ID, hk.Canonical(), firstID))
			continue
		}
		seen[hk] = l.ID
		out = append(out, Binding{LayoutID: l.ID, Hotkey: hk})
	}
	return out, errs
}

// Conflict reports the layout of the given setup that already uses hk.
// exceptID is the layout being edited, so re-entering a layout's own
// current combination is not reported as a conflict with itself. A layout
// whose stored combination does not parse cannot conflict with anything and
// is skipped; Active reports it separately.
func Conflict(layouts []store.Layout, fingerprint string, hk Hotkey, exceptID string) (store.Layout, bool) {
	if hk.IsZero() {
		return store.Layout{}, false
	}
	for _, l := range layouts {
		if l.ID == exceptID || l.Setup.Fingerprint != fingerprint || l.Hotkey == "" {
			continue
		}
		other, err := Parse(l.Hotkey)
		if err != nil {
			continue
		}
		if other == hk {
			return l, true
		}
	}
	return store.Layout{}, false
}
