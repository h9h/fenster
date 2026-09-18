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

func intersectsAny(r store.Rect, ms []store.Monitor) bool {
	for _, m := range ms {
		if r.X < m.X+m.W && r.X+r.W > m.X && r.Y < m.Y+m.H && r.Y+r.H > m.Y {
			return true
		}
	}
	return false
}
