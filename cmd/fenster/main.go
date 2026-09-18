// Command fenster is a Windows tray application that saves and restores
// window layouts, filtered by the current monitor setup.
package main

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fenster/internal/autostart"
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

	msgWindow, err := win32.NewMessageWindow("fensterMessageWindow", app.onTrayClick)
	if err != nil {
		win32.Alert("fenster", fmt.Sprintf("Anwendungsfenster konnte nicht erzeugt werden:\n%v", err))
		return
	}
	app.msgWindow = msgWindow

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
}

// notify shows a balloon and logs if even that fails, so that a balloon
// failure is at least visible in the log rather than silently swallowed.
func (a *application) notify(text string) {
	if err := a.trayIcon.Balloon("fenster", text); err != nil {
		log.Printf("Balloon: %v", err)
	}
}

// onTrayClick builds and shows the tray menu, then dispatches whatever the
// user chose. It is also called recursively by actionToggleInclude to reopen
// the menu after a checkbox toggle.
func (a *application) onTrayClick() {
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

	live := toLive(infos)
	fingerprint := monitors.Fingerprint(mons)

	autostartOn, err := autostart.Default().Enabled(a.exePath)
	if err != nil {
		log.Printf("autostart.Enabled: %v", err)
	}

	items := tray.BuildMenu(tray.MenuInput{
		Layouts:            a.store.Layouts(),
		CurrentFingerprint: fingerprint,
		Live:               live,
		AutostartOn:        autostartOn,
	})
	menu, actions := tray.Render(items)
	id := menu.Track(a.msgWindow.Handle())
	menu.Destroy()

	if id == 0 {
		return
	}
	action, ok := actions[id]
	if !ok {
		return
	}
	a.dispatch(action, mons, live)
}

// dispatch runs the command behind one chosen menu action.
func (a *application) dispatch(action tray.Action, mons []store.Monitor, live []layout.Live) {
	switch action.Type {
	case tray.ActionSave:
		a.actionSave(mons, live)
	case tray.ActionRestore:
		a.actionRestore(action.LayoutID, mons, live)
	case tray.ActionToggleInclude:
		a.actionToggleInclude(action.LayoutID, action.WindowIdx)
	case tray.ActionOverwrite:
		a.actionOverwrite(action.LayoutID, mons, live)
	case tray.ActionRename:
		a.actionRename(action.LayoutID)
	case tray.ActionDelete:
		a.actionDelete(action.LayoutID)
	case tray.ActionAutostart:
		a.actionToggleAutostart()
	case tray.ActionOpenFolder:
		a.actionOpenFolder()
	case tray.ActionQuit:
		a.actionQuit()
	}
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
		a.store.Delete(l.ID)
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

	restored, skipped, failed := 0, 0, 0
	for _, m := range plan.Matches {
		if !m.Entry.Include {
			skipped++
			continue
		}
		rect := layout.Clamp(m.Entry.Rect, mons)
		if err := win32.ApplyPlacement(m.Live.Handle, rect, m.Entry.State, m.Entry.Topmost); err != nil {
			log.Printf("restoring %q %q: %v", m.Entry.Exe, m.Entry.Title, err)
			failed++
			continue
		}
		restored++
	}
	skipped += len(plan.Missing)

	a.notify(restoreMessage(restored, skipped, failed))
}

func (a *application) actionToggleInclude(id string, windowIdx int) {
	l, ok := a.store.Get(id)
	if !ok {
		a.reportError("Fenster konnte nicht geändert werden", fmt.Errorf("Layout %q nicht gefunden", id))
		return
	}
	if windowIdx < 0 || windowIdx >= len(l.Windows) {
		a.reportError("Fenster konnte nicht geändert werden", fmt.Errorf("ungültiger Fensterindex %d", windowIdx))
		return
	}
	current := l.Windows[windowIdx].Include

	if err := a.store.SetInclude(id, windowIdx, !current); err != nil {
		a.reportError("Fenster konnte nicht geändert werden", err)
		return
	}
	if err := a.store.Save(); err != nil {
		a.store.SetInclude(id, windowIdx, current) // roll back
		a.reportError("Änderung konnte nicht gespeichert werden", err)
		return
	}

	// Reopen the menu so the user can continue toggling.
	a.onTrayClick()
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
		a.store.Replace(id, old) // roll back
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
		a.store.Replace(id, old) // roll back
		a.reportError("Umbenennen fehlgeschlagen", err)
		return
	}
	a.notify(fmt.Sprintf("Layout umbenannt in %q", name))
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
		a.store.Delete(id)
	}
	for _, l := range order {
		a.store.Add(l)
	}
}

func (a *application) actionToggleAutostart() {
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

func (a *application) actionOpenFolder() {
	if err := win32.OpenInExplorer(a.storePath); err != nil {
		a.reportError("Speicherort konnte nicht geöffnet werden", err)
	}
}

func (a *application) actionQuit() {
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
