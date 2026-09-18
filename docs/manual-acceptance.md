# Manual acceptance checklist

`go vet`, `go test` and the win32 integration tests only cover the domain
logic (matching, clamping, the menu data model, the store). Everything that
needs an actual desktop — the tray icon, the popup menu, the input dialog,
the registry, a real window being moved — has no automated test and must be
walked through by hand after any change that touches `cmd/fenster` or
`internal/win32`.

Run this on a normal Windows desktop session (not RDP without a graphical
session), with a few ordinary applications open (e.g. Notepad, a browser, File
Explorer) so there is something to save and restore.

Two places to look when a step does not behave as expected:

- **Log file**: `%APPDATA%\fenster\fenster.log`. Every error path in
  `cmd/fenster` logs before or instead of showing a balloon, so this file is
  the first place to check for a silent failure.
- **Data file**: `%APPDATA%\fenster\layouts.json`. Inspect it directly to see
  exactly what was persisted — layout list order, `include` flags, the
  `setup.fingerprint`/`setup.label` a layout was saved under, etc.

Build the binary under test first:

```
go build -ldflags="-H=windowsgui -s -w" -o fenster.exe ./cmd/fenster
```

Then start `.\fenster.exe` and work through the list below.

## Checklist

1. **Tray icon and menu open.** Start the app. A tray icon appears in the
   notification area. Left-click it: the menu opens. Right-click it: the
   same menu opens (fenster treats both clicks identically).
   *Failure*: check `fenster.log` for `LoadIconFromFile` or `NewTrayIcon`
   errors.

2. **Save a layout.** Click "Aktuelles Layout speichern…". The input dialog
   opens prefilled with a name built from the monitor setup and the current
   time (e.g. `2 Monitore · 18.09. 17:30`). Confirm with OK. `%APPDATA%\
   fenster\layouts.json` is created (if it did not exist yet) and a balloon
   reports `Layout "<name>" gespeichert (<n> Fenster)`.

3. **Layout appears in the menu.** Reopen the tray menu. The saved layout is
   a top-level entry (its own row, above "Andere Setups"/"Mit Windows
   starten"). Opening its submenu shows "Alle wiederherstellen" at the top,
   then one checked entry per captured window.

4. **Restore moves windows back.** Move two of the saved windows elsewhere
   on screen, then open the layout's submenu and click "Alle
   wiederherstellen" (Win32 cannot make the layout row itself clickable when
   it owns a submenu, so this is the only way to fire the restore). Both
   windows return to their saved position and size. The balloon reads
   `<n> Fenster wiederhergestellt` with a count matching the number of
   windows actually moved.

5. **Maximized state round-trips.** Maximize one of the saved windows on a
   specific monitor, then in the layout's submenu click "Mit aktuellem Stand
   überschreiben" to overwrite it, move/restore the window elsewhere, then
   restore the layout. The window comes back maximized, on the same monitor
   it was maximized on.

6. **Minimized state round-trips.** Minimize one of the saved windows,
   overwrite the layout (as in step 5: "Mit aktuellem Stand überschreiben" in
   its submenu), restore it after un-minimizing and moving the window. The
   window comes back minimized.

7. **Always-on-top round-trips.** Set a window to always-on-top (e.g. via its
   own UI, or a small utility), save/overwrite a layout with it in that
   state, change it, then restore. The window keeps its always-on-top flag
   after restore.

8. **Closed window shows as absent.** Close one of the applications captured
   in a layout. Reopen the tray menu: that window's entry in the layout's
   submenu is greyed out (disabled) and its label ends in
   `(nicht offen)`. Restore the layout: the balloon's "übersprungen (nicht
   offen)" count includes it.

9. **Unchecking a window persists and is respected.** In a layout's submenu,
   click a still-open window's entry to untick it. The menu reopens
   immediately (same click experience as before — the toggle does not close
   the tray menu). Reopen the menu again later: the entry is still unticked.
   Restore the layout: that window is left untouched (not moved), it does not
   count towards "wiederhergestellt", and it is reported separately as
   "abgewählt" — not lumped in with "übersprungen (nicht offen)", which is
   reserved for windows that are not actually running.

10. **Setup filtering.** Change the monitor arrangement (unplug a monitor,
    change its resolution, or change its DPI scaling). Reopen the tray menu:
    the layout saved under the old arrangement is no longer a top-level
    entry; it now appears inside the "Andere Setups" submenu, labeled with
    its own setup description in parentheses. Save a new layout while in this
    new arrangement, then restore the original arrangement (e.g. plug the
    monitor back in): both the original layout (top-level again) and the new
    one (now under "Andere Setups") are visible, each in the right place.
    *Check*: `layouts.json` — each layout's `setup.fingerprint` should differ
    between the two arrangements even though both are geometry-only, no
    device names.

11. **Restoring a layout from "Andere Setups" leaves nothing off-screen.**
    With the monitor arrangement changed as in step 10, restore the layout
    that belongs to the *other* arrangement (the one currently filed under
    "Andere Setups"). Every window that gets restored ends up fully or at
    least partly on one of the currently connected monitors — none is left
    positioned entirely off the visible desktop. If a window's saved
    position no longer overlaps any current monitor, it is moved onto the
    nearest one instead (and shrunk if it does not fit), rather than staying
    off-screen.

12. **Rename, overwrite, delete survive a restart.**
    - "Umbenennen…" opens the input dialog prefilled with the current name;
      confirming a new name updates the layout's label in the menu
      immediately and shows `Layout umbenannt in "<name>"`.
    - "Mit aktuellem Stand überschreiben" replaces the layout's window list
      with the current live windows and shows
      `Layout "<name>" aktualisiert (<n> Fenster)`.
    - "Löschen" asks for confirmation ("<name>" wirklich löschen?, Ja/Nein);
      confirming removes the layout and shows `Layout "<name>" gelöscht`;
      declining leaves it untouched.
    Restart `fenster.exe` after each of these and confirm the menu still
    reflects the change (i.e. it was actually persisted, not just held in
    memory). *Check*: `layouts.json` reflects the new name/windows/absence
    directly.

13. **Autostart registry entry.** Click "Mit Windows starten" (it is
    unchecked if no entry exists yet). A balloon shows
    `Mit Windows starten: aktiviert`. Verify with:
    ```
    reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v fenster
    ```
    The value should point at the running `fenster.exe`'s full path in
    quotes. Reopen the menu: the entry is now checked. Click it again: the
    balloon shows `Mit Windows starten: deaktiviert`, and the `reg query`
    above now fails with "wurde nicht gefunden" (value gone).

14. **"Speicherort öffnen".** Click it. A Windows Explorer window opens with
    `layouts.json` selected (or, if the file does not exist yet, its
    containing folder opens instead).

15. **Single instance.** With `fenster.exe` already running, start a second
    copy. A message box titled "fenster" reads
    `fenster läuft bereits.`, and the second process exits immediately
    (check with `tasklist` that only one `fenster.exe` remains).

16. **Corrupted JSON recovery.** Quit fenster (see step 17). Open
    `%APPDATA%\fenster\layouts.json` in a text editor and break its JSON
    syntax (e.g. delete a closing brace), save, then start `fenster.exe`
    again. A file named `layouts.json.broken-<timestamp>` appears next to
    it (original content preserved there), the app starts with an empty
    layout list, and a balloon explains that the file was corrupted and
    moved, naming the new file. *Check*: `fenster.log` has no error for this
    case — it is handled, not fatal.

17. **Clean shutdown.** Click "Beenden". The tray icon disappears
    immediately and the process exits (confirm with `tasklist`). No error
    balloon or log entry should appear for a normal exit.

18. **Input dialog keyboard behaviour.** Open the "Aktuelles Layout
    speichern…" (or "Umbenennen…") dialog. Press **Enter** while focus is in
    the text field: this confirms the dialog exactly like clicking OK (the
    typed name is used). Reopen the dialog and press **Escape**: this
    cancels exactly like clicking "Abbrechen" — no layout is saved/renamed
    and the store is unchanged. *Check*: `layouts.json` is untouched after
    the Escape case.

## Reporting results

For each item, record pass/fail and, on failure, the relevant excerpt from
`fenster.log` or the differing part of `layouts.json`. These 18 items are
interactive and cannot be verified by an agent; they require a human with a
real desktop session and are not covered by `go test`.
