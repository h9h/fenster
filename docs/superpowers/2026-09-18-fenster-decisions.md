# SDD ledger — plan: docs/superpowers/plans/2026-09-18-fenster.md

Spec: docs/superpowers/specs/2026-09-18-fenster-design.md (read)
Branch: build/fenster (created from master @ 18b6b79)

Ruling: work on branch `build/fenster` in the primary working directory instead of a
separate git worktree — brand-new repo, no other work in flight, no remote, and the user
asked for the app to be built in this directory. Cost if wrong: the user wanted worktree
isolation; recoverable by moving the branch.

## Pre-flight scan

| Row | Tasks / file / interface | Checked | Finding |
|---|---|---|---|
| 1 | T1 → T2,3,4,5,6,8: `store.Rect/Monitor/Setup/WindowEntry/Layout/WindowState` | field names and types used in every consumer | consistent |
| 2 | T3,T4,T5 share package `internal/layout` | helper names across test files: `live` (capture_test), `entry`/`liveAt` (match_test), `screens`/`intersectsAny` (clamp_test) | no collisions; match_test depends on `live` from capture_test — same package, fine |
| 3 | T4 internal | `MatchEntries` pass 3 sketch contains dead `seen` counter | plan already flags it; Ruling below |
| 4 | T6 → T9: `internal/win32/syscalls_windows.go` | T6 step 3 note adds RegisterClassExW/CreateWindowExW/DestroyWindow/UnregisterClassW/DefWindowProcW for the test helper; T9 step 1 adds the same procs again | duplicate declarations = compile error; Ruling below |
| 5 | T6 → T9: `procGetWindowTextLengthW` vs `procGetWindowTextLen` | both bind GetWindowTextLengthW | redundant; Ruling below |
| 6 | T8 → T10: `FlattenActions` vs `Render` id assignment | FlattenActions gives submenu parents an id, Render does not | divergent id maps; Ruling below |
| 7 | T8 → T9/T10: layout item is both clickable and a submenu parent | Win32 popup menus cannot select a submenu parent | spec's "clicking the layout name restores it" is not implementable; Ruling below |
| 8 | T10 internal: `suggestName` test vs sketch | test wants `2 Monitore · 18.09. 13:40`, sketch formats `%s %s` (no middot); empty-label case wants `Layout 18.09. 13:40` (no middot) | sketch contradicts its own test; Ruling below |
| 9 | T1 self-consistency | tests vs code: NewID 4 random bytes → base32 → truncated to 6 | consistent |
| 10 | T2 self-consistency | Label test expects the docked monitor first; `normalize` sorts by (y,x) relative to primary | consistent |
| 11 | T3 self-consistency | Eligible table vs `shellClasses` map and guard order | consistent |
| 12 | T5 self-consistency | clamp tests vs intersect/nearest/shrink logic | consistent |
| 13 | T6 self-consistency | `EnableDPIAwareness` and `monitorScale` use `LazyProc.Find()` with opposite nil checks | both correct (nil == found) |
| 14 | T7 self-consistency | tests use a throwaway `HKCU\Software\fenster-test\Run` key; `Default()` returns the real Run key | consistent, no risk to the real key |
| 15 | T8 self-consistency | `TestEmptyStoreStillOffersSaveAndQuit` expects "Noch keine Layouts"; BuildMenu emits "Noch keine Layouts für dieses Setup" | substring match, consistent |
| 16 | T6 minor | `processPath` allocates `syscall.MAX_LONG_PATH` uint16 per window (64 KB × N, transient) | minor, not blocking |

## Pre-flight rulings

Ruling: T4 pass 3 drops the dead `seen` counter and returns the first free window of that
executable — the plan says so in its implementer note, and the ordinal semantics come from
processing entries in save order. Cost if wrong: none, tests cover the ordering.

Ruling: T6 owns the window-class procs (RegisterClassExW, CreateWindowExW, DestroyWindow,
UnregisterClassW, DefWindowProcW, GetModuleHandleW); T9 adds only procs not already
declared. Cost if wrong: a compile error the implementer fixes in the same round.

Ruling: T9 reuses `procGetWindowTextLengthW` and does not add `procGetWindowTextLen`.
Cost if wrong: one redundant binding, harmless.

Ruling: `FlattenActions` skips items that have children, matching `Render` — submenu
parents get no command id. The T8 uniqueness test still holds. Cost if wrong: none at
runtime (Render builds the live map); it removes a divergence between two traversals.

Ruling: a layout's restore affordance is the "Alle wiederherstellen" entry inside its
submenu, not the layout row itself — Win32 cannot make a submenu parent clickable. The
model keeps `Action{Restore}` on the layout row for completeness; Render ignores it. The
README must state it. Cost if wrong: restoring costs one extra click versus the spec's
sketch — flag this to the user at the end.

Ruling: in T10 the tests are authoritative over the code sketch — `suggestName` returns
`"<prefix> · <02.01. 15:04>"` when a setup label exists and `"Layout <02.01. 15:04>"`
when it does not. Cost if wrong: a cosmetic default name.

## Progress

Task 1: complete (commits 7fe2799..e34a434, review clean)
Task 1: minor (deferred): Layouts()/Get() return the internal slice without copying
Task 1: minor (deferred): .broken- timestamp has second granularity, collides within one second
Task 1: minor (deferred): no test pins the pretty-printed JSON output
Task 2: review ❌ 1 Important (plan-mandated): normalize() silently skips translation when no
  monitor is flagged primary, so the fingerprint stops being translation-invariant, invisibly.
Task 2: Ruling: fix it rather than park it — the plan's code is defensible only because Win32
  guarantees one primary, but the guard is three lines and removes a silent failure. normalize()
  falls back to the topmost-leftmost monitor as the origin when no primary is flagged, with a test
  pinning translation invariance for that input. Cost if wrong: a few lines of unreachable code.
Task 2: minor (deferred): Describe() normalizes three times (Fingerprint, Label, own copy)
Task 2: minor (deferred): pathological (Y,X,W)-tie monitors have unstable sort order
Task 2: fix round 1/5 (1 addressed, 0 open; commits c7feb0e..a0814ce)
Task 2: complete (commits e34a434..a0814ce, review clean)
Task 3: complete (commits a0814ce..929145d, review clean)
Task 3: minor (deferred): report overstated the TestEligible case count (12, not 15)
Task 3: minor (deferred): Capture's `live` parameter shadows the test helper of the same name
Task 4: review ❌ 1 Important (plan-mandated): NormalizeTitle strips ANY trailing dash segment,
  so "Session - main" and "Session - test" of the same exe both collapse to "session" and pass 2
  silently picks the wrong window.
Task 4: Ruling: fix, and tie the suffix strip to the actual application name — NormalizeTitle takes
  (title, exe) and strips the trailing separator segment only when it equals the exe's base name
  without extension, case-insensitively. The spec says " - <AppName>", so matching the real app name
  is the faithful reading; the brief's one-argument signature and its test cases change with it.
  Cost if wrong: titles whose app suffix differs from the exe name (e.g. Code.exe → "Visual Studio
  Code") stop being stripped and fall through to pass 3 ordinal matching instead.
Task 4: Ruling: also take the deferred tie-break minor now — pass 2 accepts a candidate only when
  score >= threshold AND score > best, so the first of several equally similar windows wins instead
  of the last. One line, removes a dependency on enumeration order. Cost if wrong: none.
Task 4: minor (deferred): pass 2 re-normalizes candidate titles inside the inner loop
Task 4: fix round 1/5 (2 addressed, 0 open; commits b24eb52..23193a0)
Task 4: complete (commits 929145d..23193a0, review clean)
Task 4: minor (deferred): NormalizeTitle tries only the first separator type present, no fallthrough
Task 4: minor (deferred): the new pass-2 regression test would coincidentally pass against the
  literal pre-fix baseline (both old bugs cancel for that window order); it does pin the normalize
  bug in isolation
Task 5: complete (commits 23193a0..6f53092, review clean, no findings)
Task 6: review ✅ spec compliant, 1 Important (plan-mandated) + minors.
Task 6: Ruling: fix the GetWindowPlacement-failure path — State must never be the empty string and a
  window whose geometry cannot be read must not be captured with a 0x0 rect. Default State to
  normal, fall back to GetWindowRect, and make layout.Eligible reject non-positive width/height.
  Cost if wrong: a window that legitimately reports 0x0 would be dropped from layouts.
Task 6: Ruling: promote the reviewer's NewCallback minor to a fix. syscall.NewCallback allocates from
  a process-wide table of ~2000 slots that is never freed, and Task 10 calls EnumWindowsInfo and
  EnumMonitors on every single tray-menu open — a long-running tray app would eventually panic.
  The callbacks must be created once per process. Cost if wrong: slightly more intricate enumeration
  code than the brief's sketch.
Task 6: minor (deferred): the four ignored SetWindowPos coords are bare 0 literals, not int32-cast
Task 6: minor (deferred): no integration test for the minimized round-trip
Task 6: minor (deferred): test window class name derived from a stack address
Task 6: fix round 1/5 (2 addressed, 0 open; commits f364ad7..1feaa75)
Task 6: complete (commits 6f53092..1feaa75, review clean)
Task 6: minor (deferred): windowRect returns the current screen rect, not a restored rect
Task 7: complete (commits 1feaa75..781d699, review clean)
Task 7: minor (deferred): RegQueryValueExW's returned value kind is never checked against REG_SZ
Task 8: fix round 1/5 (1 addressed, 0 open; commits 0bb20d9..1067a7e)
Task 8: complete (commits 781d699..1067a7e, review clean)
Task 8: minor (deferred): "Andere Setups" per-item label format "%s (%s)" is an implementation choice
Task 9: Ruling: the plan verifies Task 9 only by hand, but most of it can be checked without a human.
  Require an integration-tagged smoke test that creates the message window, adds and removes the tray
  icon, shows a balloon, and builds a nested menu with checked and greyed items, asserting no API
  errors — everything except the two genuinely blocking calls (TrackPopupMenu, InputBox), which stay
  on the manual checklist. Cost if wrong: a little extra test code for UI plumbing.
Task 9: review ❌ 2 Important: wsBorder passed as dwExStyle to CreateWindowExW; smoke test asserts
  only the menu item count, so a broken checked/greyed flag composition would pass.
Task 9: Ruling: fix both, and take two cheap minors with them (window classes set no cursor or
  background brush, and the re-posted WM_QUIT drops the original exit code). Cost if wrong: minimal.
Task 9: carry to Task 10 — the reviewer flagged that Win32 windows are thread-affine and nothing in
  Task 9 can guarantee the message loop stays on one OS thread. main() must call runtime.LockOSThread.
Task 9: minor (deferred): control-creation return values in InputBox are not checked for failure
Task 9: minor (deferred): procEnableWindow is declared but unused until Task 10
Task 9: fix round 1/5 (4 addressed, 0 open; commits 7aefa29..d83d4f1)
Task 9: complete (commits 1067a7e..d83d4f1, review clean)
Task 10: pre-review fix (2 items: raw syscalls moved into win32, delete rollback order;
  commits ef852b2..2adc49d)
Task 10: complete (commits d83d4f1..2adc49d, review clean, approved with minors)
Task 10: Ruling: fold three of Task 10's minors into Task 11 rather than deferring them to the final
  review — the menu-reopen recursion becomes a loop, dispatch gets a default branch, and rollback
  failures get logged. All three are small, and Task 11 is already touching the same area for the
  README. Cost if wrong: Task 11's diff is slightly wider than the plan drew it.
Task 10: minor (deferred): autostart lookup failure on menu open is logged but not ballooned
Task 10: minor (deferred): main.go is 467 lines and would benefit from an actions file split
Task 11: complete (commits 2adc49d..186f725, review clean)
Task 11: controller resolved the reviewer's ⚠️ items directly: the ten German menu labels in README.md
  are byte-identical to internal/tray/menumodel.go; layouts.json, fenster.log, fenster.ico and the
  HKCU Run key path all match the code; win32integration is documented; go vet and go test ./... green.
Task 11: minor (deferred): manual-acceptance.md steps 5-6 forward-reference step 12
Final review (opus, whole branch 18b6b79..186f725): no Criticals; 4 Important, ~18 Minor, all
  15 deferred findings triaged.
Ruling: fix wave covers I1 (tray icon never returns after an Explorer restart), I2 (tray click during
  an open dialog orphans it), I3 (unticked windows reported as "nicht offen"), the unstable monitor
  sort comparator, and eight cheap minors. Cost if wrong: a wider final diff than a pure doc pass.
Ruling: defer I4 (no automated tests for the action handlers) — closing it means introducing prompt
  and notifier interfaces through every handler, which is a design change, not a fix, and the branch
  is otherwise merge-ready. Cost if wrong: the riskiest 200 lines stay covered only by manual testing.
Ruling: defer the Clamp work-area deviation — the spec says work area, the code uses the full monitor
  rect, and closing it needs a schema field plumbed through EnumMonitors while carefully staying out
  of the fingerprint. A clamped window lands under the taskbar but stays reachable and draggable.
Ruling: on internal/autostart calling advapi32 directly — the spec contradicts itself (its prose says
  to, its package table says it depends on win32). Keep the code, amend the spec's table. Cost if
  wrong: two packages hold unsafe code instead of one.
Final fix wave: 12 of 13 addressed (commit 186f725..b46eb6e); fix 5 partially.
Ruling: park fix 5. Render and FlattenActions now share the isActionable predicate and agree on ids,
  but still walk the tree twice, so the re-reviewer is right that they agree by inspection rather than
  by construction. There is no observable defect — the ids match today — and unifying them properly
  means restructuring Render around an injectable menu builder so it can be tested off-Windows, which
  is a design change, not a fix. Recorded as a follow-up. Cost if wrong: someone adds a new Item kind
  to one walk and not the other, and menu clicks silently fire the wrong action.
Branch build/fenster ready to hand over: 22 commits, all automated checks green, interactive
  verification outstanding (docs/manual-acceptance.md, 18 items).
