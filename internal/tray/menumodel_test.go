package tray

import (
	"strings"
	"testing"
	"time"

	"fenster/internal/layout"
	"fenster/internal/store"
)

func layoutFor(id, name, fp string, titles ...string) store.Layout {
	l := store.Layout{
		ID:      id,
		Name:    name,
		Created: time.Now(),
		Setup:   store.Setup{Fingerprint: fp, Label: "2 Monitore · 5120×1440 + 1710×1073"},
	}
	for i, title := range titles {
		l.Windows = append(l.Windows, store.WindowEntry{
			Exe: `C:\a\editor.exe`, Title: title, Ordinal: i, Include: true,
			State: store.StateNormal, Rect: store.Rect{W: 800, H: 600},
		})
	}
	return l
}

func liveWindow(title string) layout.Live {
	return layout.Live{
		Handle: 1, Title: title, Class: "C", Exe: `C:\a\editor.exe`,
		Visible: true, State: store.StateNormal, Rect: store.Rect{W: 800, H: 600},
	}
}

func find(items []Item, labelPart string) (Item, bool) {
	for _, it := range items {
		if strings.Contains(it.Label, labelPart) {
			return it, true
		}
		if child, ok := find(it.Children, labelPart); ok {
			return child, true
		}
	}
	return Item{}, false
}

// hasAdjacentSeparators reports whether any level of the menu tree — top
// level or any submenu — has two separators next to each other, which Win32
// renders as a dangling double divider. Reused by every test that builds a
// menu, so a future change cannot reintroduce the defect anywhere in the tree.
func hasAdjacentSeparators(items []Item) bool {
	for i := 1; i < len(items); i++ {
		if items[i].Separator && items[i-1].Separator {
			return true
		}
	}
	for _, it := range items {
		if hasAdjacentSeparators(it.Children) {
			return true
		}
	}
	return false
}

func TestMatchingLayoutsAreTopLevelOthersAreNested(t *testing.T) {
	in := MenuInput{
		Layouts: []store.Layout{
			layoutFor("l1", "Docked", "aaaa1111", "alpha"),
			layoutFor("l2", "Mobile", "bbbb2222", "beta"),
		},
		CurrentFingerprint: "aaaa1111",
	}

	items := BuildMenu(in)

	var topLabels []string
	for _, it := range items {
		topLabels = append(topLabels, it.Label)
	}
	joined := strings.Join(topLabels, "|")
	if !strings.Contains(joined, "Docked") {
		t.Errorf("matching layout missing from top level: %v", topLabels)
	}
	if strings.Contains(joined, "Mobile") {
		t.Errorf("non-matching layout must not be top level: %v", topLabels)
	}

	other, ok := find(items, "Andere Setups")
	if !ok {
		t.Fatal(`"Andere Setups" submenu missing`)
	}
	if _, ok := find(other.Children, "Mobile"); !ok {
		t.Errorf("non-matching layout not reachable under Andere Setups")
	}
}

func TestOtherSetupsIsOmittedWhenEverythingMatches(t *testing.T) {
	in := MenuInput{
		Layouts:            []store.Layout{layoutFor("l1", "Docked", "aaaa1111", "alpha")},
		CurrentFingerprint: "aaaa1111",
	}
	if _, ok := find(BuildMenu(in), "Andere Setups"); ok {
		t.Errorf("empty Andere Setups submenu should be omitted")
	}
}

func TestWindowEntriesShowIncludeStateAndPresence(t *testing.T) {
	l := layoutFor("l1", "Docked", "aaaa1111", "alpha", "beta")
	l.Windows[1].Include = false

	in := MenuInput{
		Layouts:            []store.Layout{l},
		CurrentFingerprint: "aaaa1111",
		Live:               []layout.Live{liveWindow("alpha")},
	}
	items := BuildMenu(in)

	alpha, ok := find(items, "alpha")
	if !ok {
		t.Fatal("window entry alpha missing")
	}
	if !alpha.Checked || alpha.Disabled {
		t.Errorf("present, included window: %+v", alpha)
	}
	if alpha.Action.Type != ActionToggleInclude || alpha.Action.LayoutID != "l1" || alpha.Action.WindowIdx != 0 {
		t.Errorf("wrong action on window entry: %+v", alpha.Action)
	}

	beta, ok := find(items, "beta")
	if !ok {
		t.Fatal("window entry beta missing")
	}
	if beta.Checked {
		t.Errorf("excluded window must not be checked: %+v", beta)
	}
	if !beta.Disabled || !strings.Contains(beta.Label, "nicht offen") {
		t.Errorf("absent window must be greyed out and marked: %+v", beta)
	}
}

func TestLayoutSubmenuHasTheManagementCommands(t *testing.T) {
	in := MenuInput{
		Layouts:            []store.Layout{layoutFor("l1", "Docked", "aaaa1111", "alpha")},
		CurrentFingerprint: "aaaa1111",
	}
	items := BuildMenu(in)

	docked, ok := find(items, "Docked")
	if !ok {
		t.Fatal("layout submenu missing")
	}
	if docked.Action.Type != ActionRestore || docked.Action.LayoutID != "l1" {
		t.Errorf("clicking the layout itself must restore it: %+v", docked.Action)
	}
	for _, want := range []string{"Alle wiederherstellen", "Mit aktuellem Stand überschreiben", "Umbenennen", "Löschen"} {
		if _, ok := find(docked.Children, want); !ok {
			t.Errorf("submenu command %q missing", want)
		}
	}
	if hasAdjacentSeparators(items) {
		t.Errorf("menu has adjacent separators: %+v", items)
	}
}

func TestLayoutWithNoWindowsHasNoDanglingSeparator(t *testing.T) {
	in := MenuInput{
		Layouts:            []store.Layout{layoutFor("l1", "Docked", "aaaa1111")}, // no titles => no windows
		CurrentFingerprint: "aaaa1111",
	}
	items := BuildMenu(in)

	if hasAdjacentSeparators(items) {
		t.Fatalf("menu has adjacent separators: %+v", items)
	}

	docked, ok := find(items, "Docked")
	if !ok {
		t.Fatal("layout submenu missing")
	}
	for _, want := range []string{"Alle wiederherstellen", "Mit aktuellem Stand überschreiben", "Umbenennen", "Löschen"} {
		if _, ok := find(docked.Children, want); !ok {
			t.Errorf("submenu command %q missing for windowless layout", want)
		}
	}
}

func TestMultipleLayoutsSharingFingerprintAreBothTopLevel(t *testing.T) {
	in := MenuInput{
		Layouts: []store.Layout{
			layoutFor("l1", "Docked", "aaaa1111", "alpha"),
			layoutFor("l2", "Second", "aaaa1111", "beta"),
		},
		CurrentFingerprint: "aaaa1111",
	}
	items := BuildMenu(in)

	if hasAdjacentSeparators(items) {
		t.Fatalf("menu has adjacent separators: %+v", items)
	}

	var topLabels []string
	for _, it := range items {
		topLabels = append(topLabels, it.Label)
	}
	joined := strings.Join(topLabels, "|")
	if !strings.Contains(joined, "Docked") || !strings.Contains(joined, "Second") {
		t.Fatalf("both layouts must be top level: %v", topLabels)
	}
	if strings.Contains(joined, "Andere Setups") {
		t.Errorf("no layout should be nested when all match the current fingerprint: %v", topLabels)
	}

	var l1, l2 Item
	for _, it := range items {
		switch it.Label {
		case "Docked":
			l1 = it
		case "Second":
			l2 = it
		}
	}
	if l1.Action.Type != ActionRestore || l1.Action.LayoutID != "l1" {
		t.Errorf("wrong action for first layout: %+v", l1.Action)
	}
	if l2.Action.Type != ActionRestore || l2.Action.LayoutID != "l2" {
		t.Errorf("wrong action for second layout: %+v", l2.Action)
	}
}

func TestGlobalCommands(t *testing.T) {
	items := BuildMenu(MenuInput{CurrentFingerprint: "aaaa1111", AutostartOn: true, AutostartAvailable: true})

	save, ok := find(items, "Aktuelles Layout speichern")
	if !ok || save.Action.Type != ActionSave {
		t.Errorf("save command missing or wrong: %+v", save)
	}
	auto, ok := find(items, "Mit Windows starten")
	if !ok || auto.Action.Type != ActionAutostart || !auto.Checked {
		t.Errorf("autostart command missing or unchecked: %+v", auto)
	}
	if _, ok := find(items, "Speicherort öffnen"); !ok {
		t.Error("open folder command missing")
	}
	quit, ok := find(items, "Beenden")
	if !ok || quit.Action.Type != ActionQuit {
		t.Errorf("quit command missing or wrong: %+v", quit)
	}
	if hasAdjacentSeparators(items) {
		t.Errorf("menu has adjacent separators: %+v", items)
	}
}

func TestAutostartDisabledWhenExecutablePathUnavailable(t *testing.T) {
	items := BuildMenu(MenuInput{CurrentFingerprint: "aaaa1111", AutostartOn: true, AutostartAvailable: false})

	auto, ok := find(items, "Mit Windows starten")
	if !ok {
		t.Fatal("autostart command missing")
	}
	if !auto.Disabled {
		t.Errorf("autostart item must be disabled when the executable path is unavailable: %+v", auto)
	}
	if auto.Checked {
		t.Errorf("autostart item must not read as checked when the executable path is unavailable: %+v", auto)
	}
}

func TestSeparatorPrecedesAndereSetups(t *testing.T) {
	in := MenuInput{
		Layouts: []store.Layout{
			layoutFor("l1", "Docked", "aaaa1111", "alpha"),
			layoutFor("l2", "Mobile", "bbbb2222", "beta"),
		},
		CurrentFingerprint: "aaaa1111",
	}
	items := BuildMenu(in)

	idx := -1
	for i, it := range items {
		if it.Label == "Andere Setups" {
			idx = i
			break
		}
	}
	if idx <= 0 {
		t.Fatalf(`"Andere Setups" not found at a valid position: %+v`, items)
	}
	if !items[idx-1].Separator {
		t.Errorf("expected a separator immediately before Andere Setups, got %+v", items[idx-1])
	}
	if hasAdjacentSeparators(items) {
		t.Errorf("menu has adjacent separators: %+v", items)
	}
}

func TestEmptyStoreStillOffersSaveAndQuit(t *testing.T) {
	items := BuildMenu(MenuInput{CurrentFingerprint: "aaaa1111"})
	if _, ok := find(items, "Aktuelles Layout speichern"); !ok {
		t.Error("save command missing")
	}
	if _, ok := find(items, "Noch keine Layouts"); !ok {
		t.Error("expected a disabled hint when there are no layouts")
	}
	if hasAdjacentSeparators(items) {
		t.Errorf("menu has adjacent separators: %+v", items)
	}
}

func TestFlattenActionsAssignsUniqueIDsSkippingSeparators(t *testing.T) {
	l := layoutFor("l1", "Docked", "aaaa1111", "alpha")
	items := BuildMenu(MenuInput{Layouts: []store.Layout{l}, CurrentFingerprint: "aaaa1111"})

	ids := FlattenActions(items)
	if len(ids) == 0 {
		t.Fatal("no actions collected")
	}
	seen := map[Action]bool{}
	for id, a := range ids {
		if id == 0 {
			t.Errorf("command id 0 is reserved for 'nothing chosen'")
		}
		if a.Type == ActionNone {
			t.Errorf("id %d maps to ActionNone", id)
		}
		if seen[a] {
			t.Errorf("action %+v assigned twice", a)
		}
		seen[a] = true
	}
}
