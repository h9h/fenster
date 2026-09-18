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
}

func TestGlobalCommands(t *testing.T) {
	items := BuildMenu(MenuInput{CurrentFingerprint: "aaaa1111", AutostartOn: true})

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
}

func TestEmptyStoreStillOffersSaveAndQuit(t *testing.T) {
	items := BuildMenu(MenuInput{CurrentFingerprint: "aaaa1111"})
	if _, ok := find(items, "Aktuelles Layout speichern"); !ok {
		t.Error("save command missing")
	}
	if _, ok := find(items, "Noch keine Layouts"); !ok {
		t.Error("expected a disabled hint when there are no layouts")
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
