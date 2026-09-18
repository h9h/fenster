package layout

import (
	"testing"

	"fenster/internal/store"
)

var screens = []store.Monitor{
	{X: -1747, Y: -1440, W: 5120, H: 1440, Scale: 100},
	{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150, Primary: true},
}

func TestClampLeavesVisibleRectanglesAlone(t *testing.T) {
	r := store.Rect{X: 100, Y: 100, W: 800, H: 600}
	if got := Clamp(r, screens); got != r {
		t.Errorf("Clamp moved a visible rectangle: %+v", got)
	}
}

func TestClampLeavesPartiallyVisibleRectanglesAlone(t *testing.T) {
	r := store.Rect{X: -200, Y: 900, W: 800, H: 600} // hangs off the bottom left
	if got := Clamp(r, screens); got != r {
		t.Errorf("a partially visible window must not be moved: %+v", got)
	}
}

func TestClampMovesFullyOffScreenRectangleOntoNearestMonitor(t *testing.T) {
	r := store.Rect{X: 9000, Y: 9000, W: 800, H: 600}
	got := Clamp(r, screens)
	if got == r {
		t.Fatalf("off-screen rectangle was not moved")
	}
	if got.W != 800 || got.H != 600 {
		t.Errorf("size must be preserved, got %dx%d", got.W, got.H)
	}
	if !intersectsAny(got, screens) {
		t.Errorf("clamped rectangle %+v is still off screen", got)
	}
}

func TestClampShrinksRectanglesLargerThanTheTargetMonitor(t *testing.T) {
	r := store.Rect{X: 9000, Y: 9000, W: 4000, H: 3000}
	got := Clamp(r, []store.Monitor{{X: 0, Y: 0, W: 1710, H: 1073, Scale: 100, Primary: true}})
	if got.W > 1710 || got.H > 1073 {
		t.Errorf("rectangle not shrunk to fit: %+v", got)
	}
	if got.X < 0 || got.Y < 0 {
		t.Errorf("rectangle not placed on the monitor: %+v", got)
	}
}

func TestClampWithoutMonitorsIsIdentity(t *testing.T) {
	r := store.Rect{X: 9000, Y: 9000, W: 800, H: 600}
	if got := Clamp(r, nil); got != r {
		t.Errorf("without monitors Clamp must not guess: %+v", got)
	}
}

// TestClampMovesFullyOffScreenRectangleOntoWorkArea is the core of this task:
// a monitor 1920x1080 at (0,0) with a 40px taskbar at the bottom reports a
// work area 1920x1040 at (0,0). An off-screen window must be clamped inside
// that work area, not merely inside the full monitor rectangle, so it never
// lands underneath the taskbar.
func TestClampMovesFullyOffScreenRectangleOntoWorkArea(t *testing.T) {
	ms := []store.Monitor{{
		X: 0, Y: 0, W: 1920, H: 1080, Scale: 100, Primary: true,
		Work: store.Rect{X: 0, Y: 0, W: 1920, H: 1040},
	}}
	r := store.Rect{X: 9000, Y: 9000, W: 800, H: 600}
	got := Clamp(r, ms)
	if got.Y+got.H > 1040 {
		t.Errorf("window placed under the taskbar: %+v", got)
	}
	if got.X < 0 || got.X+got.W > 1920 {
		t.Errorf("window placed outside the monitor horizontally: %+v", got)
	}
}

// TestClampFallsBackToFullMonitorWhenWorkAreaUnknown covers a monitor
// enumerated by a build before Work existed, or otherwise reporting a zero
// Work: the old behaviour (clamp against the full monitor rectangle) must
// still apply, since there is nothing better to clamp against.
func TestClampFallsBackToFullMonitorWhenWorkAreaUnknown(t *testing.T) {
	ms := []store.Monitor{{X: 0, Y: 0, W: 1920, H: 1080, Scale: 100, Primary: true}}
	r := store.Rect{X: 9000, Y: 9000, W: 800, H: 600}
	got := Clamp(r, ms)
	if got.X < 0 || got.Y < 0 || got.X+got.W > 1920 || got.Y+got.H > 1080 {
		t.Errorf("window not clamped inside the full monitor rectangle: %+v", got)
	}
}

// TestClampVisibilityTestStaysOnFullMonitorRectangle pins that the
// visibility test — deciding whether a rectangle needs to be moved at all —
// still uses the full monitor rectangle, not the work area. A window the
// user deliberately dragged so it extends under the taskbar must be left
// exactly where it was.
func TestClampVisibilityTestStaysOnFullMonitorRectangle(t *testing.T) {
	ms := []store.Monitor{{
		X: 0, Y: 0, W: 1920, H: 1080, Scale: 100, Primary: true,
		Work: store.Rect{X: 0, Y: 0, W: 1920, H: 1040},
	}}
	// Overlaps the monitor, but extends under the taskbar (below y=1040).
	r := store.Rect{X: 100, Y: 900, W: 800, H: 600}
	if got := Clamp(r, ms); got != r {
		t.Errorf("rectangle overlapping the monitor must not be moved to the work area: %+v", got)
	}
}

// TestClampShrinksToWorkAreaWhenKnown extends
// TestClampShrinksRectanglesLargerThanTheTargetMonitor: shrinking an
// oversized window follows the same work-area-when-known rule as moving one.
func TestClampShrinksToWorkAreaWhenKnown(t *testing.T) {
	ms := []store.Monitor{{
		X: 0, Y: 0, W: 1920, H: 1080, Scale: 100, Primary: true,
		Work: store.Rect{X: 0, Y: 0, W: 1920, H: 1040},
	}}
	r := store.Rect{X: 9000, Y: 9000, W: 4000, H: 3000}
	got := Clamp(r, ms)
	if got.W > 1920 || got.H > 1040 {
		t.Errorf("rectangle not shrunk to fit the work area: %+v", got)
	}
	if got.X < 0 || got.Y < 0 {
		t.Errorf("rectangle not placed on the monitor: %+v", got)
	}
}

func intersectsAny(r store.Rect, ms []store.Monitor) bool {
	for _, m := range ms {
		if r.X < m.X+m.W && r.X+r.W > m.X && r.Y < m.Y+m.H && r.Y+r.H > m.Y {
			return true
		}
	}
	return false
}
