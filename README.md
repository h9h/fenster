# fenster

A Windows tray application that saves and restores window layouts — the
position, size, show state (normal/minimized/maximized) and always-on-top
flag of your open windows — and offers only the layouts that match the
monitor setup you are currently on.

It is for anyone who regularly reconnects a laptop to different docks or
monitor arrangements and is tired of manually re-arranging windows every
time.

## Building and running

Requirements: Go 1.22+, Windows. No third-party modules — `go.mod` declares
no dependencies beyond the standard library, and this is deliberate: the
program runs unattended in the background with access to every open window,
the registry and the file system, so keeping its entire code auditable
without pulling in supply-chain risk from arbitrary packages was treated as
a hard constraint throughout.

```
go build -ldflags="-H=windowsgui -s -w" -o fenster.exe ./cmd/fenster
```

`-H=windowsgui` suppresses the console window; `-s -w` strip debug info for a
smaller binary. Run the resulting `fenster.exe` directly; it adds itself to
the notification area and has no window of its own.

Only one instance runs at a time — starting a second copy shows
`fenster läuft bereits.` and exits.

## Where data lives

Everything is under `%APPDATA%\fenster\`:

- `layouts.json` — the saved layouts. Written atomically (temp file +
  rename), so a crash mid-write cannot leave a half-written file in place.
- `fenster.log` — plain-text log. The build has no console
  (`-H=windowsgui`), so this file is the only place errors are visible; every
  failure path either logs, shows a balloon, or both.
- `fenster.ico` — the tray icon, extracted from the binary's embedded icon on
  first run if not already present. If it cannot be loaded, fenster falls
  back to a stock Windows icon rather than failing to start.

If Explorer restarts while fenster is running — a Windows update, an
Explorer crash, or a manual restart all do this — it rebuilds the
notification area from scratch and every icon that was there before is
gone. fenster listens for the `TaskbarCreated` broadcast Explorer sends when
this happens and re-adds its own icon automatically, so no single Explorer
restart over weeks of uptime can strand the process with no visible icon.

## The tray menu

Both left- and right-click open the same menu. Its structure, top to bottom:

- **"Aktuelles Layout speichern…"** — opens an input dialog prefilled with a
  suggested name (derived from the monitor setup and the current time),
  captures every eligible open window, and adds a new top-level layout.
- One row per saved layout that matches the **current** monitor setup. If
  none match, a disabled placeholder reads
  "Noch keine Layouts für dieses Setup".
- **"Andere Setups"** — a submenu holding every layout saved under a
  *different* monitor setup than the current one.
- **"Mit Windows starten"** — checked when fenster is registered to start at
  logon (see Autostart below).
- **"Speicherort öffnen"** — opens Explorer with `layouts.json` selected (or
  its folder, if the file does not exist yet).
- **"Beenden"** — removes the tray icon and exits.

Each layout row expands into a submenu:

- **"Alle wiederherstellen"** restores every included window of that layout.
  This is the row's own restore action, placed as the first submenu entry
  rather than on the row itself, because a Win32 popup menu item that owns a
  submenu cannot also fire a command — clicking it always opens the submenu.
  There is no way to make the parent row itself clickable.
- One entry per saved window, each with a persisted checkbox (see below), and
  greyed out with the suffix `(nicht offen)` when that window is not
  currently open (see Window matching below).
- **"Mit aktuellem Stand überschreiben"** replaces the layout's saved windows
  with the current live set, keeping the same name and id.
- **"Umbenennen…"** opens the input dialog prefilled with the current name.
- **"Löschen"** asks for confirmation, then removes the layout.

## Snapped windows

Every saved window entry stores two rectangles, not one:

- The **restored rectangle** — `WINDOWPLACEMENT.rcNormalPosition` — the size
  and position the window returns to when it is neither minimized nor
  maximized.
- The **screen rectangle** — `GetWindowRect` — what the window actually
  occupies on screen at save time.

For an ordinary window the two are identical. They diverge for a window
snapped with Windows Snap (Win+arrow, or Snap Layouts): Windows deliberately
keeps `rcNormalPosition` as the *pre-snap* rectangle, so that dragging a
snapped window away from its snap zone restores it to its former size —
while `showCmd` still reads `SW_SHOWNORMAL`. Capturing only the restored
rectangle, as an earlier version of fenster did, therefore restored a
snapped window at the size and position it had *before* it was snapped, not
where it visibly was. fenster now saves both and, on restore, positions a
normal-state window at the screen rectangle when one was captured, falling
back to the restored rectangle for layouts saved before this existed or for
a window whose screen rectangle could not be read. Minimized and maximized
windows are unaffected and always use the restored rectangle, since a
minimized window's screen rectangle is the meaningless off-screen
`(-32000, -32000)` Windows reports for any minimized window.

**Known limitation:** Windows has no public API to put a window back into a
snap *group*. A restored window lands on the correct area of the screen, but
Windows does not consider it snapped afterwards — its neighbours will not
resize if the restored window is resized or moved again, the way real
snap-group members do. fenster does not attempt to work around this (in
particular, it does not synthesize Win+arrow keystrokes); getting the
geometry right on restore is the fix, re-establishing snap-group membership
is not something a normal application can do.

**A second, separate consequence:** the "drag away to get your old size
back" behaviour described above for a native snap does not survive a
fenster restore either. Positioning the window at the screen rectangle
resyncs `rcNormalPosition` to that same rectangle, so the restored window
has no remembered pre-snap size left to return to — dragging it away from
the screen area it was restored to just keeps it at that size, not its
original pre-snap one. The next time this layout is saved or overwritten,
`Rect` and `Screen` are captured equal to each other, so the original
pre-snap size is gone for good after one restore cycle.

## Monitor fingerprint

A layout is tagged with a fingerprint of the monitor arrangement it was
captured on: each monitor's resolution, its position relative to the primary
monitor, and its DPI scaling percentage. Monitors are sorted into a
canonical order first, so the fingerprint does not depend on enumeration
order.

Deliberately **not** part of the fingerprint: which physical monitor it is —
no device name, adapter id, or serial number is used. Two consequences follow
directly from that:

- Replacing a monitor with an identical (or geometry-identical) model keeps
  every layout saved for that setup working, because nothing about the old
  monitor's identity was ever recorded.
- Two different docking stations, or two different desks, that happen to
  produce the same monitor count, resolutions, relative positions and DPI
  scaling are indistinguishable to fenster and will share the same layouts.

## Window matching

When restoring, each saved window entry is matched to a currently open
window of the same executable in three passes, applied in order over
whatever is still unmatched after the previous pass:

1. **Exact title match.**
2. **Similar title match** — titles are normalized (case, whitespace, common
   modification markers, an app-name suffix) and compared with a
   normalized Levenshtein similarity; the closest match above a fixed
   threshold wins. This tolerates the everyday churn of titles that include a
   document name, a URL, or an "unsaved changes" marker.
3. **N-th window of the same executable**, by the order the entry and the
   live windows were originally enumerated in — this is the fallback for
   windows whose titles no longer resemble the saved one at all.

A live window is only ever claimed by one saved entry. An entry that finds no
candidate in any pass is reported as missing; in the menu, its row is greyed
out (disabled) and suffixed `(nicht offen)`, and restoring counts it as
skipped rather than failed, reported in the balloon as
`<n> übersprungen (nicht offen)`.

## Persisted checkmarks

Unticking a window in a layout's submenu writes that choice straight to
`layouts.json` — it is not a transient, per-restore selection that resets
the next time you open the menu. This follows directly from how a Win32
popup menu works: it closes after every single click, so there is no way to
tick several boxes and *then* confirm in one sitting. Persisting the
checkbox is what makes toggling several windows off, one click at a time,
actually usable — the menu reopens immediately after each toggle instead of
staying closed, but the state itself lives in the file, not in memory.

A window excluded this way is reported in the restore balloon as
`<n> abgewählt`, distinct from `<n> übersprungen (nicht offen)`: one is a
deliberate choice recorded by the user, the other means the window simply
was not found running. Folding both into a single count would make
deliberately unticking windows indistinguishable from them being closed.

## Autostart

"Mit Windows starten" toggles a `REG_SZ` value under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value name `fenster`,
pointing at the current executable's full path in quotes. Unticking it
deletes the value. No third-party install mechanism, scheduled task, or
service is used. If the running executable's own path could not be
determined at startup, the menu item is greyed out instead of silently
reading as unchecked and writing an empty value on click.

## Corrupted data recovery

If `layouts.json` fails to parse on startup, it is renamed to
`layouts.json.broken-<timestamp>` (the original content is preserved, not
lost), fenster starts with an empty layout list, and a balloon names the
renamed file. A file written by a newer schema version than this build
understands is left untouched and startup fails with an explicit error
instead of guessing at its structure.

## Logging

All log output goes to `%APPDATA%\fenster\fenster.log`. Nothing is printed to
a console — there isn't one, by design (`-H=windowsgui`) — so this file, plus
the balloons shown for user-facing failures, is the only diagnostic surface.

## Testing

```
go vet ./...
go test ./...
go test -tags win32integration ./internal/win32/ -v
```

The first two run everywhere and cover the pure domain logic (window
matching, off-screen clamping, the monitor fingerprint, the menu data model,
the store's load/save/corruption handling) without needing a desktop. The
`win32integration` tag additionally exercises real Win32 calls — window
enumeration, monitor enumeration, moving an actual window, building a real
menu — and therefore only runs on Windows with a graphical session.

Everything that needs live human interaction with the tray icon, the popup
menu and the input dialog (there is no automated UI-driving test for those)
is covered instead by the manual checklist in
[`docs/manual-acceptance.md`](docs/manual-acceptance.md).
