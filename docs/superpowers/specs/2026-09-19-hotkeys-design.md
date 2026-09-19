# fenster — Applying Layouts by Hotkey

Design document, 2026-09-19.

## Purpose

Restore a saved layout by pressing a key combination, without opening the
tray menu. The v1 design listed global hotkeys as explicitly out of scope;
this document adds them.

## Scope

In scope:

- One optional hotkey per layout. Pressing it does exactly what the layout's
  "Alle wiederherstellen" entry does.
- Assigning, changing and clearing that hotkey from the layout's submenu.
- Only the layouts matching the current monitor setup have their hotkeys
  registered, refreshed when the display arrangement changes.
- Honest reporting when a combination is already owned by another
  application.

Out of scope: a hotkey for saving or overwriting a layout, a hotkey that
opens the tray menu, chords (two-step combinations), per-window hotkeys,
and capturing a combination by pressing it (the combination is typed).

## Constraints

Unchanged from v1 and load-bearing here:

- **No third-party modules.** Standard library only; all Win32 access through
  `syscall.NewLazyDLL`.
- UI strings are German. Code, comments, commits and documentation are
  English.
- Single-threaded UI: the message loop goroutine is `runtime.LockOSThread`ed.
  This matters for hotkeys specifically — a hotkey registered by a thread can
  only be unregistered by that same thread.

## Decisions

Four choices shape everything below. Each was taken deliberately; the
alternatives are recorded so a later reader can see they were considered.

**Per layout, not global.** A hotkey belongs to a layout and restores it. A
separate "save the current arrangement" hotkey was considered and dropped:
saving needs a name, a name needs a dialog, and a dialog defeats the point of
not touching the menu.

**Typed, not captured.** The combination is typed into the existing
`win32.InputBox`. A capture dialog ("press the keys now") is nicer to use but
means a second hand-rolled dialog in `internal/win32`, reachable only by the
manual checklist. Typing reuses a dialog that already exists and puts the
whole interpretation into a pure, table-tested parser.

**Only the current setup is registered.** This mirrors what the menu already
does: fenster offers the layouts that match the monitor arrangement you are
on. `Strg+Alt+1` can therefore mean the dock layout at the desk and the
laptop layout on the road. A consequence worth stating plainly: uniqueness
only has to hold *within* one setup, and a layout under "Andere Setups" can
never be triggered by a key.

**Rejected at assignment, marked later.** Assigning tries the registration
first and refuses to persist a combination Windows will not grant. A
conflict that only appears later — the other application starts after
fenster — cannot be prevented and is therefore surfaced rather than hidden.

**Full re-sync over incremental diff.** Every change unregisters all
hotkeys and registers the current set from scratch. Registering a couple of
dozen hotkeys costs microseconds; an incremental diff would have to carry
per-id failure bookkeeping across syncs, which is state that can drift from
the truth. A low-level keyboard hook (`WH_KEYBOARD_LL`) was rejected
outright: it would let fenster bind combinations Windows reserves, at the
price of a background process reading every keystroke on the machine.

## Data model

`store.Layout` gains one field:

```go
// Hotkey is the global key combination that restores this layout, in the
// canonical English text form produced by hotkey.Hotkey.Canonical (e.g.
// "Ctrl+Alt+1"). Empty means no hotkey. Text rather than a numeric
// modifier/virtual-key pair because layouts.json is a file the user is
// invited to open ("Speicherort öffnen"), and because it keeps raw Win32
// constants out of the file format — the same reason WindowState is
// "minimized" and not an SW_ number.
//
// A value this build cannot parse is treated as absent and logged; it is
// never silently rewritten, so a typo in a hand-edited file can be
// corrected by hand rather than being destroyed on the next save.
Hotkey string `json:"hotkey,omitempty"`
```

`SchemaVersion` stays **1**, on the same reasoning that adding
`WindowEntry.Screen` did not bump it: a file written before this field
existed decodes with an empty `Hotkey`, which every reader treats as "no
hotkey", and a file written after it exists decodes fine on an older binary,
which ignores the unknown `"hotkey"` key. The change is backward and forward
compatible. The `SchemaVersion` comment is extended to say so.

## `internal/hotkey` — the pure package

No Win32, no build tag, no platform assumptions; it therefore runs under
`go test ./...` everywhere, like `internal/layout` and `internal/monitors`.

```go
// Mod is a bitmask of modifier keys.
type Mod uint32

const (
    ModAlt Mod = 1 << iota
    ModCtrl
    ModShift
    ModWin
)

// Hotkey is a modifier combination plus one key. The zero value is "no
// hotkey". Key is a Win32 virtual-key code: that is what identifies a key
// to RegisterHotKey, and inventing a private enum in front of it would buy
// nothing but a translation table.
type Hotkey struct {
    Mods Mod
    Key  uint32
}
```

### Parsing

`Parse(s string) (Hotkey, error)` is deliberately lenient about how a human
types a combination and strict about what it means:

- Separators: `+`, `-`, and runs of spaces, in any mix.
- Case-insensitive.
- Modifiers: `Strg`, `Ctrl`, `Control`, `Steuerung`; `Alt`; `Umschalt`,
  `Shift`; `Win`, `Windows`.
- Keys: `A`–`Z`, `0`–`9`, `F1`–`F24`, the four arrow keys
  (`Links`/`Left`, `Rechts`/`Right`, `Hoch`/`Up`, `Runter`/`Down`),
  `Pos1`/`Home`, `Ende`/`End`, `Bild↑`/`BildAuf`/`PageUp`,
  `Bild↓`/`BildAb`/`PageDown`, `Einfg`/`Insert`, `Entf`/`Delete`,
  `Leertaste`/`Space`, `Esc`/`Escape`, `Tab`, `Eingabe`/`Enter`/`Return`.
- Exactly one non-modifier key is required.
- **At least one modifier is required.** A bare `F5` would be swallowed
  system-wide, from every application, for as long as fenster runs; that is
  not a thing a user can reasonably ask for by accident. The error says so.
- The empty string parses to the zero `Hotkey` with no error: that is how
  "clear this binding" is expressed.

Errors are German, user-facing strings (they go straight into a balloon) and
name the offending token.

### Formatting

- `Canonical() string` — the storage form: English, `+`-separated, modifiers
  in the fixed order Ctrl, Alt, Shift, Win. Fixed order means the same
  combination always produces the same string, so a comparison between two
  bindings is a comparison of two `Hotkey` values, never of two spellings.
- `Label() string` — the display form for the menu: German (`Strg`,
  `Umschalt`), same order.

`Parse(h.Canonical()) == h` and `Parse(h.Label()) == h` for every valid `h`;
both are round-trip tested.

### Selection and conflicts

```go
// Binding is one layout's live hotkey.
type Binding struct {
    LayoutID string
    Hotkey   Hotkey
}

// Active returns the bindings that should be registered right now: the
// layouts whose setup fingerprint matches, in store order, with a parseable
// non-empty hotkey. A layout whose stored string does not parse is skipped
// and named in the returned []error, so the caller can log it. When two
// layouts of the same setup carry the same combination — only reachable by
// hand-editing the file, since assignment rejects it — the first in store
// order wins and the second is reported as an error, rather than both being
// registered and the second silently failing.
func Active(layouts []store.Layout, fingerprint string) ([]Binding, []error)

// Conflict reports the layout of this setup that already uses hk, ignoring
// the layout being edited (exceptID), so re-assigning a layout its own
// current combination is not a conflict with itself.
func Conflict(layouts []store.Layout, fingerprint string, hk Hotkey, exceptID string) (store.Layout, bool)
```

`internal/hotkey` imports `internal/store`, which is allowed and
uncontroversial: `store` has no dependencies precisely so every other
package may import it.

## `internal/win32` — registration and delivery

New file `hotkey_windows.go`:

```go
// ErrHotkeyInUse is returned when Windows reports the combination is
// already registered by another window or process
// (ERROR_HOTKEY_ALREADY_REGISTERED, 1409). Callers distinguish this from a
// genuine failure to decide whether to blame the user's choice or the
// system, so it is a sentinel rather than something to match on text.
var ErrHotkeyInUse = errors.New("hotkey already in use")

func RegisterHotKey(hwnd uintptr, id int32, mods, vk uint32) error
func UnregisterHotKey(hwnd uintptr, id int32) error
```

`RegisterHotKey` OR-s in `MOD_NOREPEAT` (0x4000) unconditionally: without
it, holding the combination down repeats the restore for as long as the keys
are held, and a restore is not an operation that should run forty times a
second. `id` stays inside the documented 0x0000–0xBFFF range for a
window-owned hotkey.

### `MessageWindow` callbacks

`NewMessageWindow` currently takes two bare `func()` parameters and now needs
two more (`WM_HOTKEY`, `WM_DISPLAYCHANGE`). Four positional callbacks is
where that signature stops being readable at the call site, so it becomes:

```go
// Callbacks are the events a MessageWindow reports to its owner. Every one
// is invoked from inside Run's DispatchMessageW, i.e. on the owner's own
// goroutine; a nil field means the message is ignored. A named struct
// rather than positional parameters because a reader of the call site
// should not have to count func() arguments to know which is which.
type Callbacks struct {
    TrayClick        func()
    TaskbarRecreated func()
    DisplayChange    func()
    Hotkey           func(id int32)
}

func NewMessageWindow(className string, cb Callbacks) (*MessageWindow, error)
```

This is a targeted change to code the feature already has to touch, not an
unrelated refactor: both new messages are delivered through this one window
proc.

In `messageWindowProc`:

- `WM_HOTKEY` (0x0312) — the id is the low word of `wParam`. Routed through
  the existing `msgWndState` map, exactly like `wmTrayIcon`.
- `WM_DISPLAYCHANGE` (0x007E) — routed the same way, then passed on to
  `DefWindowProcW`.

## `cmd/fenster/hotkeys.go` — the manager

```go
// hotkeyBackend is the Win32 surface the manager needs, extracted as an
// interface so the whole sync algorithm — id allocation, failure
// bookkeeping, unregistering what is gone — is exercised by unit tests with
// a fake, on any machine, without a desktop or a real message window.
type hotkeyBackend interface {
    Register(id int32, mods, vk uint32) error
    Unregister(id int32) error
}

type hotkeyManager struct {
    backend    hotkeyBackend
    registered map[int32]string // id -> layout ID, only successful ones
    unavailable map[string]bool // layout IDs whose registration is currently refused
}

// sync makes the registered set match the layouts of the current setup. It
// unregisters everything and registers the desired set from scratch (see
// "Full re-sync over incremental diff"), collecting failures instead of
// aborting on the first one: one combination taken by another application
// must not cost the user the other nine.
func (m *hotkeyManager) sync(layouts []store.Layout, fingerprint string) (newlyUnavailable int)
```

The errors `hotkey.Active` returns for unparseable or duplicated stored
strings are logged by `sync` and otherwise ignored: they describe the state
of the file, not of this sync, and there is nothing the user can do about
them from a balloon.

Ids are `1…n` over `hotkey.Active`'s order within a single sync. They are not
stable across syncs and do not need to be — nothing outside a sync holds one,
and `WM_HOTKEY` is translated through `registered` immediately on arrival.

`sync` is called:

- once at startup, after the store is loaded and the message window exists;
- after every store mutation, without exception: save, overwrite, rename,
  delete, hotkey assignment, and the include-checkbox toggle. Some of these
  (rename, the checkbox) cannot possibly change the registered set, but a
  needless sync costs a handful of syscalls, whereas a list of "mutations
  that do not need a sync" is a list a future mutation gets added to the
  wrong side of;
- on `WM_DISPLAYCHANGE`;
- with an empty layout list on quit, before the tray icon is removed.

Windows releases a process's hotkeys when it exits, so the quit call is
belt-and-braces; it is cheap and makes the lifecycle symmetric.

`unavailable` feeds the menu's `(belegt)` marker. A sync that newly refuses
one or more combinations balloons once — `2 Hotkeys sind belegt` — rather
than once per hotkey; each also gets a log line naming the layout and the
combination. Because every sync retries from scratch, closing the offending
application and triggering any sync makes the hotkey start working again
with no further action.

## Assignment

A new action `tray.ActionHotkey` and a new submenu entry beside
`Umbenennen…`:

- `Hotkey …` when none is set,
- `Hotkey: Strg+Alt+1 …` when one is,
- `Hotkey: Strg+Alt+1 (belegt) …` when one is set but currently refused.

`actionHotkey(layoutID)`:

1. `win32.InputBox("Hotkey", "Tastenkombination:", current.Label())`.
   Cancel changes nothing.
2. Empty input clears the binding: persist `""`, re-sync, balloon
   `Hotkey für %q entfernt`.
3. `hotkey.Parse` failure balloons the parser's own German message and
   changes nothing.
4. `hotkey.Conflict` against the same setup, excluding this layout, balloons
   `Strg+Alt+1 ist bereits mit %q belegt` and changes nothing.
5. Otherwise the registration is attempted **before** anything is persisted.
   `ErrHotkeyInUse` balloons `Strg+Alt+1 wird bereits von einer anderen
   Anwendung verwendet` and changes nothing; any other error is reported
   through `reportError` and changes nothing.
6. On success the layout is written and saved, with the same
   roll-back-on-failed-save pattern `actionRename` uses, and a full `sync`
   follows so the manager's own state is derived from the store rather than
   patched in place. Balloon: `Hotkey für %q: Strg+Alt+1`.

Assignment is reachable for layouts of *any* setup — the entry is in every
layout submenu, including those under "Andere Setups". Assigning one there
persists it and simply does not register it until that setup is current; the
balloon says `… (aktiv, sobald dieses Setup verwendet wird)` in that case, so
the user is not left wondering why the key does nothing.

## Pressing a hotkey

`onHotkey(id)`:

1. The existing `busy` re-entrancy guard applies unchanged. A hotkey pressed
   while the menu is open or a dialog is up is a no-op, not a second modal
   on top of the first — the same reason the guard exists for tray clicks.
2. `registered[id]` gives the layout ID. An id with no entry is logged and
   ignored: it can only mean a hotkey from before a sync arrived after it,
   which is a race Windows allows and nothing to alarm the user about.
3. Windows and monitors are enumerated fresh, exactly as `showMenuOnce`
   does. Nothing from the last menu build is reused; the whole point is that
   no menu was opened.
4. The restore itself is the *same code* `ActionRestore` runs, factored into
   a shared method rather than copied: identical matching, identical
   unticked-window handling, identical clamping, identical balloon via
   `restoreMessage`. A hotkey restore that behaved even slightly differently
   from the menu restore would be a bug that only reproduces for users who
   use hotkeys.
5. A layout deleted between registration and press is reported through
   `reportError`, not a panic — this handler runs inside a
   `syscall.NewCallback`, where a Go panic takes down the process and with
   it the tray icon.

## Menu rendering

The layout row shows its binding in the accelerator column: `AppendMenuW`
right-aligns the part of a label after a `\t`, so the row reads

```
Arbeit · 2 Monitore            Strg+Alt+1
```

like any native Windows menu. Only layouts of the current setup get this —
a layout under "Andere Setups" has no live binding, and showing one there
would advertise a key that does nothing. Tabs in a layout name are replaced
with spaces when building the label, so a name cannot forge a second column.

`tray.MenuInput` gains `Unavailable map[string]bool`, supplied from the
manager, so `BuildMenu` stays a pure function of its input.

## Error handling

| Situation | Behaviour |
|---|---|
| Unparseable hotkey string in `layouts.json` | Skipped, logged, layout otherwise intact; the file is not rewritten |
| Duplicate combination within one setup in the file | First in store order registered, second skipped and logged |
| `ERROR_HOTKEY_ALREADY_REGISTERED` at assignment | Nothing persisted, balloon names the combination |
| `ERROR_HOTKEY_ALREADY_REGISTERED` at sync | Logged per layout, one summary balloon, `(belegt)` in the menu, retried at the next sync |
| Other registration failure | `reportError` (log + balloon) |
| `WM_HOTKEY` for an unknown id | Logged, ignored |
| Hotkey pressed while busy | Ignored, as tray clicks already are |

## Testing

Automated, no desktop required (`go test ./...`):

- `internal/hotkey`: table tests for `Parse` (German and English modifier
  names, case, each separator, every key class, the empty string, unknown
  token, two non-modifier keys, missing modifier); `Canonical`/`Label`
  round-trips; `Active` filtering by fingerprint, store order, skipping
  unparseable entries, and the duplicate-in-file rule; `Conflict` including
  the `exceptID` case.
- `cmd/fenster`: `hotkeyManager.sync` against a fake backend — registers the
  matching set, unregisters what is no longer desired, survives a failing
  registration without losing the rest, records and clears `unavailable`,
  and reports the newly-unavailable count for the balloon.
- `internal/tray`: the new submenu entry in its three label forms, the
  accelerator column on current-setup rows only, tab stripping, and that
  `FlattenActions` assigns the new action an id.

Under `-tags win32integration` (Windows with a session):

- Register a real hotkey on a real message window, assert a second
  registration of the same combination returns `ErrHotkeyInUse`, unregister
  both and assert re-registration then succeeds.

Manual, added to `docs/manual-acceptance.md`:

- Assign a hotkey, press it with the tray menu closed, confirm the same
  balloon as the menu restore.
- Assign a combination held by another running application, confirm the
  refusal and that nothing was written.
- Unplug or plug a monitor, confirm the hotkeys of the newly current setup
  take over.
- Press a hotkey while the save dialog is open, confirm nothing happens.

## Documentation

`README.md` gains a "Hotkeys" section covering the accepted syntax, the
"only the current setup is live" rule and why it is useful, the modifier
requirement, `MOD_NOREPEAT`, and the two ways a combination can be
unavailable. The v1 spec's "out of scope: global hotkeys" line is left as
the historical record it is; this document supersedes it.
