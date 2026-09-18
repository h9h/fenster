package tray

import "fenster/internal/win32"

// Render turns the menu model into a Win32 menu plus the map from command id
// to action. The map itself comes straight from FlattenActions — the same
// function menumodel_test.go exercises — instead of a second, hand-written
// traversal that used to duplicate its id-assignment rule and therefore
// could silently drift from it. The tree-building walk below still has to
// visit every item, in the same order, to call AddItem/AddSeparator/
// AddSubmenu; the id it wires onto each AddItem call is re-derived with the
// same isActionable predicate FlattenActions uses, so the two counters are
// guaranteed to agree rather than merely happening to. A layout row that
// owns a submenu is not made clickable here: Win32 cannot fire a command for
// an item that opens a submenu (clicking it always opens the submenu
// instead), which is exactly why the model places "Alle wiederherstellen" as
// the first entry inside that submenu. The parent row's own Action field is
// therefore deliberately ignored below.
func Render(items []Item) (*win32.Menu, map[uint32]Action) {
	actions := FlattenActions(items)

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
				if isActionable(it) {
					id = next
					next++
				}
				m.AddItem(id, it.Label, it.Checked, it.Disabled)
			}
		}
		return m
	}

	return build(items), actions
}
