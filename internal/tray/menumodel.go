// Package tray builds the tray menu. The menu is first assembled as a plain
// data model, which is unit-testable, and then rendered with Win32 calls.
package tray

import (
	"fmt"
	"path/filepath"

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
	ActionDelete
	ActionAutostart
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
			items = append(items, layoutItem(l, in.Live, ""))
			matching++
			continue
		}
		others = append(others, layoutItem(l, in.Live, l.Setup.Label))
	}

	if matching == 0 {
		items = append(items, Item{Label: "Noch keine Layouts für dieses Setup", Disabled: true})
	}
	if len(others) > 0 {
		items = append(items, Item{Label: "Andere Setups", Children: others})
	}

	return append(items,
		separator,
		Item{Label: "Mit Windows starten", Checked: in.AutostartOn, Action: Action{Type: ActionAutostart}},
		Item{Label: "Speicherort öffnen", Action: Action{Type: ActionOpenFolder}},
		Item{Label: "Beenden", Action: Action{Type: ActionQuit}},
	)
}

// layoutItem builds the submenu of one layout. suffix carries the setup label
// for layouts shown under "Andere Setups".
func layoutItem(l store.Layout, live []layout.Live, suffix string) Item {
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
		Item{Label: "Löschen", Action: Action{Type: ActionDelete, LayoutID: l.ID}},
	)

	label := l.Name
	if suffix != "" {
		label = fmt.Sprintf("%s (%s)", l.Name, suffix)
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

// FlattenActions assigns Win32 command ids, starting at 1 because
// TrackPopupMenu returns 0 when the user dismisses the menu. Items that have
// children are skipped: a Win32 popup menu cannot fire a command for an item
// that opens a submenu, so no id is assigned there, matching Task 10's
// renderer.
func FlattenActions(items []Item) map[uint32]Action {
	out := map[uint32]Action{}
	var next uint32 = 1
	var walk func([]Item)
	walk = func(list []Item) {
		for _, it := range list {
			if !it.Separator && it.Action.Type != ActionNone && len(it.Children) == 0 {
				out[next] = it.Action
				next++
			}
			walk(it.Children)
		}
	}
	walk(items)
	return out
}
