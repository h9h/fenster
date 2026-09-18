package tray

import "fenster/internal/win32"

// Render turns the menu model into a Win32 menu plus the map from command id
// to action. Ids are assigned in the same traversal order as FlattenActions.
// A layout row that owns a submenu is not made clickable here: Win32 cannot
// fire a command for an item that opens a submenu (clicking it always opens
// the submenu instead), which is exactly why the model places "Alle
// wiederherstellen" as the first entry inside that submenu. The parent row's
// own Action field is therefore deliberately ignored below.
func Render(items []Item) (*win32.Menu, map[uint32]Action) {
	actions := map[uint32]Action{}
	var next uint32 = 1

	var build func([]Item) *win32.Menu
	build = func(list []Item) *win32.Menu {
		m := win32.NewMenu()
		for _, it := range list {
			switch {
			case it.Separator:
				m.AddSeparator()
			case len(it.Children) > 0:
				m.AddSubmenu(it.Label, build(it.Children), it.Disabled)
			default:
				id := uint32(0)
				if it.Action.Type != ActionNone {
					id = next
					actions[id] = it.Action
					next++
				}
				m.AddItem(id, it.Label, it.Checked, it.Disabled)
			}
		}
		return m
	}

	return build(items), actions
}
