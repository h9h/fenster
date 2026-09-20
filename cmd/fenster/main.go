// Command fenster is a Windows tray application that saves and restores
// window layouts, filtered by the current monitor setup.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fenster/internal/autostart"
	"fenster/internal/hotkey"
	"fenster/internal/layout"
	"fenster/internal/monitors"
	"fenster/internal/store"
	"fenster/internal/tray"
	"fenster/internal/win32"
)

//go:embed icon.ico
var embeddedIcon []byte

func main() {
	// Win32 windows and message loops are thread-affine; without this the Go
	// scheduler could migrate this goroutine to a different OS thread and
	// silently break the message loop and every window handle created here.
	runtime.LockOSThread()

	win32.EnableDPIAwareness()

	release, alreadyRunning, err := win32.AcquireSingleInstance(`Local\fenster-single-instance`)
	if err != nil {
		// Failure to even attempt the check is not fatal: proceed as if this
		// were the only instance rather than refusing to start over
		// something that cannot be verified. There is no log output target
		// yet at this point, so this failure is otherwise unreported.
		log.Printf("single instance: %v", err)
	}
	if alreadyRunning {
		win32.Alert("fenster", "fenster läuft bereits.")
		return
	}
	defer release()

	storePath, err := store.DefaultPath()
	if err != nil {
		win32.Alert("fenster", fmt.Sprintf("Speicherort konnte nicht ermittelt werden:\n%v", err))
		return
	}
	appDir := filepath.Dir(storePath)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		win32.Alert("fenster", fmt.Sprintf("Datenverzeichnis konnte nicht angelegt werden:\n%v", err))
		return
	}

	logPath := filepath.Join(appDir, "fenster.log")
	if logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
		// Not fatal: the build has no console (-H=windowsgui), so a failed
		// log file is a diagnostics loss, not a reason to refuse to start.
		log.SetOutput(io.Discard)
	} else {
		defer logFile.Close()
		log.SetOutput(logFile)
	}

	iconPath := filepath.Join(appDir, "fenster.ico")
	if _, err := os.Stat(iconPath); os.IsNotExist(err) {
		if err := os.WriteFile(iconPath, embeddedIcon, 0o644); err != nil {
			log.Printf("writing icon: %v", err)
		}
	}
	iconHandle, err := win32.LoadIconFromFile(iconPath)
	if err != nil {
		log.Printf("LoadIconFromFile: %v", err)
		iconHandle = win32.StockIcon()
	}

	st := store.New(storePath)
	recoveredPath, err := st.Load()
	if err != nil {
		win32.Alert("fenster", fmt.Sprintf("Layouts konnten nicht geladen werden:\n%v", err))
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Printf("os.Executable: %v", err)
	}

	app := &application{
		store:     st,
		storePath: storePath,
		exePath:   exePath,
	}

	msgWindow, err := win32.NewMessageWindow("fensterMessageWindow", win32.Callbacks{
		TrayClick:        app.onTrayClick,
		TaskbarRecreated: app.onTaskbarRecreated,
		DisplayChange:    app.onDisplayChange,
		Hotkey:           app.onHotkey,
	})
	if err != nil {
		win32.Alert("fenster", fmt.Sprintf("Anwendungsfenster konnte nicht erzeugt werden:\n%v", err))
		return
	}
	app.msgWindow = msgWindow
	app.hotkeys = newHotkeyManager(windowHotkeys{hwnd: msgWindow.Handle()})

	trayIcon, err := win32.NewTrayIcon(msgWindow.Handle(), iconHandle, "fenster")
	if err != nil {
		win32.Alert("fenster", fmt.Sprintf("Tray-Symbol konnte nicht erzeugt werden:\n%v", err))
		return
	}
	app.trayIcon = trayIcon

	if recoveredPath != "" {
		app.notify(fmt.Sprintf(
			"Die Layout-Datei war beschädigt und wurde nach %q verschoben. fenster startet mit einem leeren Speicher.",
			recoveredPath))
	}

	// Register the hotkeys of the setup we are starting on. Deliberately
	// after the tray icon exists: a combination already taken by another
	// application is reported by balloon, and a balloon needs an icon.
	app.syncHotkeys()

	msgWindow.Run()
}

// application holds everything the tray click handler and the action
// dispatch need across calls.
type application struct {
	store     *store.Store
	storePath string
	exePath   string
	msgWindow *win32.MessageWindow
	trayIcon  *win32.TrayIcon
	hotkeys   *hotkeyManager

	// busy is a re-entrancy guard for onTrayClick and onHotkey. InputBox's
	// nested message loop pumps every message on the thread, including the
	// tray icon's own wmTrayIcon and WM_HOTKEY, so a click or a hotkey press
	// while a dialog (or any other action) is already in progress would
	// otherwise dispatch straight back in and open a second menu, or a
	// second restore, on top of the first — which, for InputBox
	// specifically, orphaned the outer dialog outright. Setting this on
	// entry and clearing it via defer makes such a re-entrant call a no-op
	// instead. onDisplayChange deliberately ignores this guard — not
	// because the sync it triggers touches no UI (it can show a balloon via
	// notify), but because nothing on the sync path (EnumMonitors,
	// store.Layouts, RegisterHotKey/UnregisterHotKey, Shell_NotifyIconW)
	// pumps the thread's message queue the way InputBox and Menu.Track do,
	// so onDisplayChange cannot nest a second dialog or menu even while one
	// is already open. Adding anything modal (win32.Alert or similar) to
	// the sync path would break that and needs this guard revisited.
	busy bool
}

// notify shows a balloon and logs if even that fails, so that a balloon
// failure is at least visible in the log rather than silently swallowed. It
// mirrors reportError's nil check on trayIcon: notify can run from inside a
// syscall.NewCallback (via the tray click and window message plumbing),
// where a Go panic terminates the whole process — which would remove the
// tray icon, the one thing no failure is allowed to do.
func (a *application) notify(text string) {
	if a.trayIcon == nil {
		return
	}
	if err := a.trayIcon.Balloon("fenster", text); err != nil {
		log.Printf("Balloon: %v", err)
	}
}

// onTaskbarRecreated re-adds the tray icon after Explorer rebuilds the
// notification area (a Windows update, an Explorer crash, or a manual
// restart all trigger this). Without it, fenster would keep running with no
// visible icon and no way to reach it short of Task Manager.
func (a *application) onTaskbarRecreated() {
	if a.trayIcon == nil {
		return
	}
	if err := a.trayIcon.Readd(); err != nil {
		log.Printf("TrayIcon.Readd after TaskbarCreated: %v", err)
	}
}

// syncHotkeys re-registers the global hotkeys for the monitor setup in use
// right now. It is called at startup, after every store mutation and on
// every display change; a sync costs a handful of syscalls, which is why
// there is no list of "mutations that do not need one" — such a list is a
// list a future mutation gets added to the wrong side of.
func (a *application) syncHotkeys() {
	if a.hotkeys == nil {
		return
	}
	mons, err := win32.EnumMonitors()
	if err != nil {
		// Without the monitor list there is no fingerprint and therefore no
		// way to tell which hotkeys should be live. Leaving the previous
		// registrations in place is the better failure: they were right a
		// moment ago.
		log.Printf("syncHotkeys: EnumMonitors: %v", err)
		return
	}
	if newly := a.hotkeys.sync(a.store.Layouts(), monitors.Fingerprint(mons)); newly > 0 {
		a.notify(unavailableMessage(newly))
	}
}

// onDisplayChange re-registers hotkeys after the monitor arrangement
// changed, so the layouts of the setup now in use take over. It ignores the
// busy guard on purpose — not because syncHotkeys touches no UI (it does,
// via notify's balloon), but because nothing it calls (EnumMonitors,
// store.Layouts, RegisterHotKey/UnregisterHotKey, Shell_NotifyIconW) pumps
// the thread's message queue, unlike InputBox and Menu.Track, which pump
// everything and can in fact deliver WM_DISPLAYCHANGE into this very
// function mid-dialog. Because this call itself never pumps, it cannot
// nest a second dialog or a second menu on top of whatever busy is already
// guarding. If the sync path ever grows a call that does pump (win32.Alert
// or any other modal), that call could re-enter and this reasoning — and
// the guard it justifies — would need to be revisited.
func (a *application) onDisplayChange() {
	a.syncHotkeys()
}

// onHotkey restores the layout behind a pressed global hotkey. It runs the
// same actionRestore the menu's "Alle wiederherstellen" runs — not a copy
// of it — so a hotkey restore cannot drift from a menu restore in matching,
// clamping, deselected windows or the balloon it shows.
func (a *application) onHotkey(id int32) {
	if a.busy {
		return
	}
	a.busy = true
	defer func() { a.busy = false }()

	if a.hotkeys == nil {
		// Cannot happen in practice: no message pump runs before main
		// assigns this field. Guarded anyway because this runs from
		// syscall.NewCallback, where a nil dereference panics and takes the
		// tray icon down with the whole process.
		return
	}

	layoutID, ok := a.hotkeys.layoutFor(id)
	if !ok {
		// A press that arrived after the sync that unregistered its hotkey.
		// Windows allows that race; it is nothing to alarm the user about.
		log.Printf("WM_HOTKEY for unknown id %d", id)
		return
	}

	infos, err := win32.EnumWindowsInfo()
	if err != nil {
		a.reportError("Fenster konnten nicht ermittelt werden", err)
		return
	}
	mons, err := win32.EnumMonitors()
	if err != nil {
		a.reportError("Monitore konnten nicht ermittelt werden", err)
		return
	}
	a.actionRestore(layoutID, mons, toLive(infos))
}

// onTrayClick builds and shows the tray menu, dispatches whatever the user
// chose, and — instead of recursing — loops to reopen the menu as long as the
// dispatched action asks for it (currently only a checkbox toggle does, so
// the user can keep ticking windows without the menu closing between
// clicks). Each iteration is an independent menu build, since the set of
// live windows and monitors may have changed while the previous menu was
// open.
func (a *application) onTrayClick() {
	if a.busy {
		return
	}
	a.busy = true
	defer func() { a.busy = false }()

	for a.showMenuOnce() {
	}
}

// showMenuOnce builds and shows the tray menu once, dispatches the chosen
// action, and reports whether the menu should be reopened immediately.
func (a *application) showMenuOnce() (reopen bool) {
	infos, err := win32.EnumWindowsInfo()
	if err != nil {
		a.reportError("Fenster konnten nicht ermittelt werden", err)
		return false
	}
	mons, err := win32.EnumMonitors()
	if err != nil {
		a.reportError("Monitore konnten nicht ermittelt werden", err)
		return false
	}

	live := toLive(infos)
	fingerprint := monitors.Fingerprint(mons)

	// A failed os.Executable() at startup leaves exePath empty; querying or
	// toggling autostart with an empty path would always read back
	// unchecked and, if toggled on, write an empty value into the registry.
	// Skipping the query and marking the item unavailable instead keeps the
	// menu honest about what it cannot do.
	autostartAvailable := a.exePath != ""
	var autostartOn bool
	if autostartAvailable {
		var err error
		autostartOn, err = autostart.Default().Enabled(a.exePath)
		if err != nil {
			log.Printf("autostart.Enabled: %v", err)
		}
	}

	// Guarded the same way as onHotkey: unreachable before main assigns
	// a.hotkeys, but this too runs from syscall.NewCallback, where a nil
	// dereference would panic and take the tray icon down with it. A nil
	// map is a safe, empty Unavailable for BuildMenu — reading from it
	// never panics.
	var unavailable map[string]bool
	if a.hotkeys != nil {
		unavailable = a.hotkeys.unavailableIDs()
	}

	items := tray.BuildMenu(tray.MenuInput{
		Layouts:            a.store.Layouts(),
		CurrentFingerprint: fingerprint,
		Live:               live,
		Unavailable:        unavailable,
		AutostartOn:        autostartOn,
		AutostartAvailable: autostartAvailable,
		MinimizeOthers:     a.store.MinimizeOthers(),
	})
	menu, actions := tray.Render(items)
	id := menu.Track(a.msgWindow.Handle())
	menu.Destroy()

	if id == 0 {
		return false
	}
	action, ok := actions[id]
	if !ok {
		return false
	}
	return a.dispatch(action, mons, live)
}

// dispatch runs the command behind one chosen menu action and reports
// whether the menu should be reopened immediately afterwards.
func (a *application) dispatch(action tray.Action, mons []store.Monitor, live []layout.Live) (reopen bool) {
	switch action.Type {
	case tray.ActionSave:
		a.actionSave(mons, live)
	case tray.ActionRestore:
		a.actionRestore(action.LayoutID, mons, live)
	case tray.ActionToggleInclude:
		reopen = a.actionToggleInclude(action.LayoutID, action.WindowIdx)
	case tray.ActionOverwrite:
		a.actionOverwrite(action.LayoutID, mons, live)
	case tray.ActionRename:
		a.actionRename(action.LayoutID)
	case tray.ActionHotkey:
		a.actionHotkey(action.LayoutID)
	case tray.ActionDelete:
		a.actionDelete(action.LayoutID)
	case tray.ActionAutostart:
		a.actionToggleAutostart()
	case tray.ActionMinimizeOthers:
		reopen = a.actionToggleMinimizeOthers()
	case tray.ActionOpenFolder:
		a.actionOpenFolder()
	case tray.ActionQuit:
		a.actionQuit()
	default:
		// Should be unreachable: every tray.ActionType is handled above. Log
		// rather than silently no-op, so a future action type added to the
		// model without a matching case here does not vanish without a trace.
		log.Printf("dispatch: unhandled action type %v", action.Type)
	}
	// Re-register after every action rather than after a curated list of
	// the mutating ones: a sync is a handful of syscalls, and a curated
	// list is where the next action gets filed on the wrong side. Quit is
	// the one exception — actionQuit has already released everything, and
	// re-registering on the way out would undo that.
	if action.Type != tray.ActionQuit {
		a.syncHotkeys()
	}
	return reopen
}

func (a *application) actionSave(mons []store.Monitor, live []layout.Live) {
	label := monitors.Label(mons)
	name, ok := win32.InputBox("Layout speichern", "Name:", suggestName(label, time.Now()))
	if !ok {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		a.reportError("Layout speichern", fmt.Errorf("kein Name angegeben"))
		return
	}

	windows := layout.Capture(live)
	now := time.Now()
	l := store.Layout{
		ID:      store.NewID(),
		Name:    name,
		Created: now,
		Updated: now,
		Setup:   monitors.Describe(mons),
		Windows: windows,
	}

	a.store.Add(l)
	if err := a.store.Save(); err != nil {
		// Roll back so the in-memory store stays consistent with what is
		// actually on disk.
		if rbErr := a.store.Delete(l.ID); rbErr != nil {
			log.Printf("rollback add %q after failed save: %v", l.ID, rbErr)
		}
		a.reportError("Layout konnte nicht gespeichert werden", err)
		return
	}
	a.notify(fmt.Sprintf("Layout %q gespeichert (%d Fenster)", name, len(windows)))
}

func (a *application) actionRestore(id string, mons []store.Monitor, live []layout.Live) {
	l, ok := a.store.Get(id)
	if !ok {
		a.reportError("Wiederherstellen fehlgeschlagen", fmt.Errorf("Layout %q nicht gefunden", id))
		return
	}

	plan := layout.MatchEntries(l.Windows, live)

	// deselected and missing are counted separately (I3): an entry excluded
	// by an unticked checkbox is a deliberate user choice, not evidence that
	// its window is not running, and lumping both into one "skipped" bucket
	// made the balloon claim windows were "nicht offen" for windows the user
	// had simply unticked.
	// Extraneous windows go first, so the layout's own windows are placed
	// onto an already-cleared desktop and end up in front. Minimizing
	// afterwards would leave focus wherever the last minimize put it.
	minimized := a.minimizeExtraneous(plan.Unmatched)

	restored, deselected, missing, failed := 0, 0, 0, 0
	for _, m := range plan.Matches {
		if !m.Entry.Include {
			deselected++
			continue
		}
		rect, screen := clampEntry(m.Entry, mons)
		if err := win32.ApplyPlacement(m.Live.Handle, rect, screen, m.Entry.State, m.Entry.Topmost); err != nil {
			log.Printf("restoring %q %q: %v", m.Entry.Exe, m.Entry.Title, err)
			failed++
			continue
		}
		restored++
	}
	missing = len(plan.Missing)

	a.notify(restoreMessage(restored, deselected, missing, minimized, failed))
}

// minimizeExtraneous minimizes the open windows the layout does not account
// for and reports how many were actually changed. It is a no-op when the
// option is off.
func (a *application) minimizeExtraneous(unmatched []layout.Live) int {
	handles := windowsToMinimize(unmatched, a.store.MinimizeOthers())
	for _, h := range handles {
		win32.MinimizeWindow(h)
	}
	return len(handles)
}

// windowsToMinimize decides which extraneous windows to minimize, separated
// from the Win32 call that does it so the decision is testable without a
// desktop — the same split the rest of this codebase uses.
//
// A window that is already minimized is skipped rather than minimized again:
// it is not counted, because the balloon reports what this restore changed,
// not how many windows happen to be minimized afterwards.
func windowsToMinimize(unmatched []layout.Live, enabled bool) []uintptr {
	if !enabled {
		return nil
	}
	var handles []uintptr
	for _, w := range unmatched {
		if w.State == store.StateMinimized {
			continue
		}
		handles = append(handles, w.Handle)
	}
	return handles
}

// clampEntry computes the two rectangles a restore applies for one saved
// entry, each clamped against the current monitors independently so a
// layout restored on a foreign monitor setup still lands on screen.
//
// The restored rect is always clamped. The screen rect is only clamped when
// it is present and valid (W>0 and H>0); an absent Screen (the zero value,
// from a layout saved before that field existed) is left untouched rather
// than "clamped" into a bogus on-screen rectangle at (0,0) — Clamp has no
// way to tell a deliberate zero-size rect from a merely-absent one, so that
// distinction is made here instead.
func clampEntry(e store.WindowEntry, mons []store.Monitor) (rect, screen store.Rect) {
	rect = layout.Clamp(e.Rect, mons)
	screen = e.Screen
	if screen.W > 0 && screen.H > 0 {
		screen = layout.Clamp(screen, mons)
	}
	return rect, screen
}

// actionToggleInclude flips one window's persisted checkbox and reports
// whether the menu should be reopened so the user can continue toggling.
func (a *application) actionToggleInclude(id string, windowIdx int) bool {
	l, ok := a.store.Get(id)
	if !ok {
		a.reportError("Fenster konnte nicht geändert werden", fmt.Errorf("Layout %q nicht gefunden", id))
		return false
	}
	if windowIdx < 0 || windowIdx >= len(l.Windows) {
		a.reportError("Fenster konnte nicht geändert werden", fmt.Errorf("ungültiger Fensterindex %d", windowIdx))
		return false
	}
	current := l.Windows[windowIdx].Include

	if err := a.store.SetInclude(id, windowIdx, !current); err != nil {
		a.reportError("Fenster konnte nicht geändert werden", err)
		return false
	}
	if err := a.store.Save(); err != nil {
		if rbErr := a.store.SetInclude(id, windowIdx, current); rbErr != nil { // roll back
			log.Printf("rollback SetInclude %q[%d] after failed save: %v", id, windowIdx, rbErr)
		}
		a.reportError("Änderung konnte nicht gespeichert werden", err)
		return false
	}

	// Ask the caller to reopen the menu so the user can continue toggling.
	return true
}

func (a *application) actionOverwrite(id string, mons []store.Monitor, live []layout.Live) {
	old, ok := a.store.Get(id)
	if !ok {
		a.reportError("Überschreiben fehlgeschlagen", fmt.Errorf("Layout %q nicht gefunden", id))
		return
	}

	updated := old
	updated.Setup = monitors.Describe(mons)
	updated.Windows = layout.Capture(live)
	updated.Updated = time.Now()

	if err := a.store.Replace(id, updated); err != nil {
		a.reportError("Überschreiben fehlgeschlagen", err)
		return
	}
	if err := a.store.Save(); err != nil {
		if rbErr := a.store.Replace(id, old); rbErr != nil { // roll back
			log.Printf("rollback overwrite %q after failed save: %v", id, rbErr)
		}
		a.reportError("Überschreiben fehlgeschlagen", err)
		return
	}
	a.notify(fmt.Sprintf("Layout %q aktualisiert (%d Fenster)", updated.Name, len(updated.Windows)))
}

func (a *application) actionRename(id string) {
	l, ok := a.store.Get(id)
	if !ok {
		a.reportError("Umbenennen fehlgeschlagen", fmt.Errorf("Layout %q nicht gefunden", id))
		return
	}

	name, ok := win32.InputBox("Layout umbenennen", "Name:", l.Name)
	if !ok {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" || name == l.Name {
		return
	}

	old := l
	l.Name = name
	l.Updated = time.Now()

	if err := a.store.Replace(id, l); err != nil {
		a.reportError("Umbenennen fehlgeschlagen", err)
		return
	}
	if err := a.store.Save(); err != nil {
		if rbErr := a.store.Replace(id, old); rbErr != nil { // roll back
			log.Printf("rollback rename %q after failed save: %v", id, rbErr)
		}
		a.reportError("Umbenennen fehlgeschlagen", err)
		return
	}
	a.notify(fmt.Sprintf("Layout umbenannt in %q", name))
}

// actionHotkey assigns, changes or clears one layout's hotkey. The
// registration is attempted before anything is persisted, so a combination
// Windows will not grant never reaches layouts.json.
func (a *application) actionHotkey(id string) {
	l, ok := a.store.Get(id)
	if !ok {
		a.reportError("Hotkey konnte nicht geändert werden", fmt.Errorf("Layout %q nicht gefunden", id))
		return
	}
	current, _ := hotkey.Parse(l.Hotkey) // an unparseable stored value echoes as empty

	// The combination is captured, not typed. Typing let a user enter a
	// perfectly valid "Ctrl+Alt+1" on a keyboard that cannot produce Alt at
	// all — it stored, it registered, and it then never fired, with nothing
	// anywhere reporting a fault. Capturing means what cannot be pressed
	// cannot be saved.
	mods, vk, ok, cleared := win32.CaptureHotkey(
		"Hotkey", "Tastenkombination drücken:", current.Label(), describeCapture)
	if !ok {
		return
	}
	var hk hotkey.Hotkey
	if !cleared {
		var err error
		hk, err = hotkey.FromKeys(hotkeyMods(mods), vk)
		if err != nil {
			// The dialog already refuses to enable OK for anything invalid,
			// so reaching here means the rules disagreed with themselves.
			a.reportError("Hotkey", err)
			return
		}
	}
	// Nothing would change. Worth its own branch rather than falling
	// through: re-probing a combination this layout already holds would
	// collide with fenster's own registration and be reported as "another
	// application owns it". Comparing the canonical text too, not just the
	// parsed value, means clearing an unparseable stored value still counts
	// as a change and is written.
	if hk == current && hk.Canonical() == l.Hotkey {
		return
	}

	if !hk.IsZero() {
		if other, dup := hotkey.Conflict(a.store.Layouts(), l.Setup.Fingerprint, hk, l.ID); dup {
			a.reportError("Hotkey", fmt.Errorf("%s ist bereits mit %q belegt", hk.Label(), other.Name))
			return
		}
	}

	// Whether the combination can actually be granted is only answerable
	// while this layout's setup is the current one — that is the only time
	// its hotkey is live. For a layout under "Andere Setups" the check is
	// deferred to the sync that happens when that setup returns.
	mons, err := win32.EnumMonitors()
	if err != nil {
		a.reportError("Monitore konnten nicht ermittelt werden", err)
		return
	}
	isCurrentSetup := l.Setup.Fingerprint == monitors.Fingerprint(mons)

	if !hk.IsZero() && isCurrentSetup {
		if err := a.hotkeys.probe(win32Mods(hk.Mods), hk.Key); err != nil {
			if errors.Is(err, win32.ErrHotkeyInUse) {
				a.reportError("Hotkey", fmt.Errorf("%s wird bereits von einer anderen Anwendung verwendet", hk.Label()))
			} else {
				a.reportError("Hotkey konnte nicht registriert werden", err)
			}
			return
		}
	}

	old := l
	l.Hotkey = hk.Canonical()
	l.Updated = time.Now()

	if err := a.store.Replace(id, l); err != nil {
		a.reportError("Hotkey konnte nicht geändert werden", err)
		return
	}
	if err := a.store.Save(); err != nil {
		if rbErr := a.store.Replace(id, old); rbErr != nil { // roll back
			log.Printf("rollback hotkey %q after failed save: %v", id, rbErr)
		}
		a.reportError("Hotkey konnte nicht gespeichert werden", err)
		return
	}

	switch {
	case hk.IsZero():
		a.notify(fmt.Sprintf("Hotkey für %q entfernt", l.Name))
	case isCurrentSetup:
		a.notify(fmt.Sprintf("Hotkey für %q: %s", l.Name, hk.Label()))
	default:
		a.notify(fmt.Sprintf("Hotkey für %q: %s (aktiv, sobald dieses Setup verwendet wird)", l.Name, hk.Label()))
	}
}

func (a *application) actionDelete(id string) {
	l, ok := a.store.Get(id)
	if !ok {
		a.reportError("Löschen fehlgeschlagen", fmt.Errorf("Layout %q nicht gefunden", id))
		return
	}

	if !win32.Confirm("Layout löschen", fmt.Sprintf("%q wirklich löschen?", l.Name)) {
		return
	}

	// Snapshot the exact storage order so a failed Save can be rolled back to
	// look exactly as it did before: Store's public API has no "insert at
	// index", only Add (append), so simply re-adding l after a failed delete
	// would silently move it to the end of the list.
	order := append([]store.Layout(nil), a.store.Layouts()...)

	if err := a.store.Delete(id); err != nil {
		a.reportError("Löschen fehlgeschlagen", err)
		return
	}
	if err := a.store.Save(); err != nil {
		a.restoreOrder(order)
		a.reportError("Löschen fehlgeschlagen", err)
		return
	}
	a.notify(fmt.Sprintf("Layout %q gelöscht", l.Name))
}

// restoreOrder replaces the store's current layouts with order, so that a
// failed mutation can be rolled back to its exact original position rather
// than just its presence. It only uses Store's existing public API.
func (a *application) restoreOrder(order []store.Layout) {
	var currentIDs []string
	for _, l := range a.store.Layouts() {
		currentIDs = append(currentIDs, l.ID)
	}
	for _, id := range currentIDs {
		if err := a.store.Delete(id); err != nil {
			// A double fault: the delete rollback itself failed. Logging is
			// the only way this is ever visible, since there is no further
			// rollback to fall back to.
			log.Printf("restoreOrder: deleting %q: %v", id, err)
		}
	}
	for _, l := range order {
		a.store.Add(l)
	}
}

func (a *application) actionToggleAutostart() {
	if a.exePath == "" {
		a.reportError("Mit Windows starten", fmt.Errorf("Pfad der ausführbaren Datei konnte nicht ermittelt werden"))
		return
	}

	entry := autostart.Default()
	on, err := entry.Enabled(a.exePath)
	if err != nil {
		a.reportError("Autostart konnte nicht geprüft werden", err)
		return
	}

	if on {
		if err := entry.Disable(); err != nil {
			a.reportError("Autostart konnte nicht deaktiviert werden", err)
			return
		}
		a.notify("Mit Windows starten: deaktiviert")
		return
	}

	if err := entry.Enable(a.exePath); err != nil {
		a.reportError("Autostart konnte nicht aktiviert werden", err)
		return
	}
	a.notify("Mit Windows starten: aktiviert")
}

// actionToggleMinimizeOthers flips the "minimize windows that are not part
// of the layout" option and reports whether the menu should reopen, the same
// way the per-window checkboxes do: this is a checkbox, and a Win32 popup
// menu closes on every click, so reopening is what makes a checkbox feel
// like one.
func (a *application) actionToggleMinimizeOthers() bool {
	current := a.store.MinimizeOthers()
	a.store.SetMinimizeOthers(!current)
	if err := a.store.Save(); err != nil {
		a.store.SetMinimizeOthers(current) // roll back to match the file
		a.reportError("Einstellung konnte nicht gespeichert werden", err)
		return false
	}
	return true
}

func (a *application) actionOpenFolder() {
	if err := win32.OpenInExplorer(a.storePath); err != nil {
		a.reportError("Speicherort konnte nicht geöffnet werden", err)
	}
}

func (a *application) actionQuit() {
	// Windows releases a process's hotkeys when it exits, so this is
	// belt-and-braces; it costs nothing and keeps the lifecycle symmetric.
	if a.hotkeys != nil {
		a.hotkeys.sync(nil, "")
	}
	if err := a.trayIcon.Remove(); err != nil {
		log.Printf("TrayIcon.Remove: %v", err)
	}
	a.msgWindow.Quit()
}

// reportError logs and, when a tray icon already exists, shows a balloon so
// that no error path in a -H=windowsgui build (no console, no stdout) is
// ever silent.
func (a *application) reportError(context string, err error) {
	log.Printf("%s: %v", context, err)
	if a.trayIcon == nil {
		return
	}
	if bErr := a.trayIcon.Balloon("fenster – Fehler", fmt.Sprintf("%s: %v", context, err)); bErr != nil {
		log.Printf("Balloon: %v", bErr)
	}
}

// toLive converts the raw Win32 window list into the domain's Live type.
func toLive(infos []win32.WindowInfo) []layout.Live {
	pid := win32.CurrentPID()
	out := make([]layout.Live, len(infos))
	for i, w := range infos {
		out[i] = layout.Live{
			Handle:  w.Handle,
			Title:   w.Title,
			Class:   w.Class,
			Exe:     w.Exe,
			Rect:    w.Rect,
			Screen:  w.Screen,
			State:   w.State,
			Topmost: w.Topmost,
			Visible: w.Visible,
			Cloaked: w.Cloaked,
			Tool:    w.Tool,
			Own:     w.PID == pid,
		}
	}
	return out
}
