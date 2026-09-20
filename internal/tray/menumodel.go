// Package tray builds the tray menu. The menu is first assembled as a plain
// data model, which is unit-testable, and then rendered with Win32 calls.
package tray

import (
	"fmt"
	"path/filepath"
	"strings"

	"fenster/internal/hotkey"
	"fenster/internal/layout"
	"fenster/internal/store"
)

// ActionType is what a menu click should do.
type ActionType int

const (
	ActionNone ActionType = iota
	ActionSave
	ActionRestore
	ActionToggleInclude
	ActionOverwrite
	ActionRename
	ActionHotkey
	ActionDelete
	ActionAutostart
	ActionMinimizeOthers
	ActionOpenFolder
	ActionQuit
)

// Action is the command behind one menu item.
type Action struct {
	Type      ActionType
	LayoutID  string
	WindowIdx int
}

// Item is one entry of the menu model.
type Item struct {
	Label     string
	Separator bool
	Checked   bool
	Disabled  bool
	Action    Action
	Children  []Item
}

// MenuInput is everything the menu is built from.
type MenuInput struct {
	Layouts            []store.Layout
	CurrentFingerprint string
	Live               []layout.Live
	AutostartOn        bool
	// AutostartAvailable is false when the running executable's own path
	// could not be determined (a failed os.Executable()). Toggling autostart
	// without a real path would write an empty value into the registry and
	// then always read back as unchecked, so the menu item is greyed out and
	// forced unchecked instead.
	AutostartAvailable bool
	// MinimizeOthers is the current state of the "minimize windows that are
	// not part of the layout" option. Unlike AutostartOn it has no
	// "available" companion: the option is a value in layouts.json, so
	// there is no external state that could make it unreadable.
	MinimizeOthers bool
	// Unavailable holds the IDs of layouts whose hotkey is currently
	// refused by Windows because another application owns the combination.
	// Supplied by the hotkey manager so BuildMenu stays a pure function of
	// its input rather than querying anything itself.
	Unavailable map[string]bool
}

var separator = Item{Separator: true}

// BuildMenu assembles the tray menu: layouts of the current setup at the top
// level, everything else under "Andere Setups", then the global commands.
func BuildMenu(in MenuInput) []Item {
	items := []Item{
		{Label: "Aktuelles Layout speichern…", Action: Action{Type: ActionSave}},
		separator,
	}

	var others []Item
	matching := 0
	for _, l := range in.Layouts {
		if l.Setup.Fingerprint == in.CurrentFingerprint {
			items = append(items, layoutItem(l, in.Live, "", true, in.Unavailable[l.ID]))
			matching++
			continue
		}
		others = append(others, layoutItem(l, in.Live, l.Setup.Label, false, in.Unavailable[l.ID]))
	}

	if matching == 0 {
		items = append(items, Item{Label: "Noch keine Layouts für dieses Setup", Disabled: true})
	}
	if len(others) > 0 {
		// A separator between the matching layouts (or their disabled
		// placeholder) and "Andere Setups" mirrors the design's menu sketch;
		// it is only emitted here, alongside the submenu it introduces,
		// rather than unconditionally, so an absent "Andere Setups" cannot
		// leave it sitting right next to the separator appended below and
		// render as two adjacent dividers.
		items = append(items, separator, Item{Label: "Andere Setups", Children: others})
	}

	return append(items,
		separator,
		Item{
			Label:    "Mit Windows starten",
			Checked:  in.AutostartOn && in.AutostartAvailable,
			Disabled: !in.AutostartAvailable,
			Action:   Action{Type: ActionAutostart},
		},
		Item{
			Label:   "Andere Fenster minimieren",
			Checked: in.MinimizeOthers,
			Action:  Action{Type: ActionMinimizeOthers},
		},
		Item{Label: "Speicherort öffnen", Action: Action{Type: ActionOpenFolder}},
		Item{Label: "Beenden", Action: Action{Type: ActionQuit}},
	)
}

// layoutItem builds the submenu of one layout. suffix carries the setup
// label for layouts shown under "Andere Setups". current says whether this
// layout belongs to the monitor setup in use, which decides whether its
// hotkey is shown on the row itself: a layout under "Andere Setups" has no
// live registration, and advertising a key that does nothing would be a
// lie. unavailable marks a hotkey Windows currently refuses.
func layoutItem(l store.Layout, live []layout.Live, suffix string, current, unavailable bool) Item {
	present := map[int]bool{}
	plan := layout.MatchEntries(l.Windows, live)
	for _, m := range plan.Matches {
		present[m.Index] = true
	}

	children := []Item{
		{Label: "Alle wiederherstellen", Action: Action{Type: ActionRestore, LayoutID: l.ID}},
	}
	// The separator before the window list is only emitted when there is a
	// window list to separate from the management commands below; otherwise
	// it would sit right next to the trailing separator and render as two
	// adjacent dividers.
	if len(l.Windows) > 0 {
		children = append(children, separator)
	}
	for i, w := range l.Windows {
		label := fmt.Sprintf("%s — %s", filepath.Base(w.Exe), w.Title)
		if !present[i] {
			label += "  (nicht offen)"
		}
		children = append(children, Item{
			Label:    label,
			Checked:  w.Include,
			Disabled: !present[i],
			Action:   Action{Type: ActionToggleInclude, LayoutID: l.ID, WindowIdx: i},
		})
	}
	children = append(children,
		separator,
		Item{Label: "Mit aktuellem Stand überschreiben", Action: Action{Type: ActionOverwrite, LayoutID: l.ID}},
		Item{Label: "Umbenennen…", Action: Action{Type: ActionRename, LayoutID: l.ID}},
		Item{Label: hotkeyEntryLabel(l, unavailable), Action: Action{Type: ActionHotkey, LayoutID: l.ID}},
		Item{Label: "Löschen", Action: Action{Type: ActionDelete, LayoutID: l.ID}},
	)

	label := l.Name
	if suffix != "" {
		label = fmt.Sprintf("%s (%s)", l.Name, suffix)
	}
	// Strip tabs before the accelerator column is appended: a Win32 popup
	// menu right-aligns everything after the first tab, so a name
	// containing one could otherwise forge a binding that does not exist.
	label = strings.ReplaceAll(label, "\t", " ")
	if current {
		if acc := hotkeyAccelerator(l, unavailable); acc != "" {
			label += "\t" + acc
		}
	}
	// Action is still populated on the layout row itself even though it has
	// children: the model stays a complete description of "what a click on
	// this row should do." FlattenActions ignores it below, because a Win32
	// popup menu item that owns a submenu cannot itself carry a command id
	// (clicking it opens the submenu instead of firing a command) — Task 10's
	// renderer follows the same rule, so the two traversals agree on ids.
	return Item{
		Label:    label,
		Action:   Action{Type: ActionRestore, LayoutID: l.ID},
		Children: children,
	}
}

// isActionable reports whether it should be assigned a Win32 command id: it
// carries a real action, it is not a separator, and it has no children. An
// item with children is a submenu owner — a Win32 popup menu item that owns
// a submenu cannot itself fire a command, clicking it always opens the
// submenu instead — which is exactly why layoutItem places "Alle
// wiederherstellen" as the first entry inside the submenu rather than
// relying on the parent row's own (otherwise unreachable) Action.
// render_windows.go's Render calls both this function and FlattenActions
// directly, rather than keeping a second, independently maintained
// traversal, so the ids it wires onto the real Win32 menu can never drift
// from the map it hands back to its caller.
func isActionable(it Item) bool {
	return !it.Separator && it.Action.Type != ActionNone && len(it.Children) == 0
}

// FlattenActions assigns Win32 command ids, starting at 1 because
// TrackPopupMenu returns 0 when the user dismisses the menu, to every
// actionable item in the tree in depth-first order.
func FlattenActions(items []Item) map[uint32]Action {
	out := map[uint32]Action{}
	var next uint32 = 1
	var walk func([]Item)
	walk = func(list []Item) {
		for _, it := range list {
			if isActionable(it) {
				out[next] = it.Action
				next++
			}
			walk(it.Children)
		}
	}
	walk(items)
	return out
}

// parsedHotkey is the layout's stored combination, or false when it has
// none or the stored text does not parse. An unparseable value is simply
// not shown; internal/hotkey's Active reports it to the log instead, and a
// menu is the wrong place to explain a malformed file.
func parsedHotkey(l store.Layout) (hotkey.Hotkey, bool) {
	if l.Hotkey == "" {
		return hotkey.Hotkey{}, false
	}
	hk, err := hotkey.Parse(l.Hotkey)
	if err != nil || hk.IsZero() {
		return hotkey.Hotkey{}, false
	}
	return hk, true
}

// hotkeyAccelerator is what goes into the row's right-aligned accelerator
// column, empty when there is nothing to show.
func hotkeyAccelerator(l store.Layout, unavailable bool) string {
	hk, ok := parsedHotkey(l)
	if !ok {
		return ""
	}
	if unavailable {
		return hk.Label() + " (belegt)"
	}
	return hk.Label()
}

// hotkeyEntryLabel is the submenu row that opens the assignment dialog. The
// trailing ellipsis follows the same convention as "Umbenennen…": this
// entry opens a dialog rather than acting immediately.
func hotkeyEntryLabel(l store.Layout, unavailable bool) string {
	hk, ok := parsedHotkey(l)
	if !ok {
		return "Hotkey …"
	}
	if unavailable {
		return fmt.Sprintf("Hotkey: %s (belegt) …", hk.Label())
	}
	return fmt.Sprintf("Hotkey: %s …", hk.Label())
}
