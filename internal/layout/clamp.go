package layout

import "fenster/internal/store"

// Clamp keeps a target rectangle reachable. The visibility test — deciding
// whether the rectangle needs to move at all — always uses the full monitor
// rectangle: a rectangle that overlaps any monitor is returned unchanged, so
// a window the user deliberately dragged partly under the taskbar is never
// moved. A rectangle entirely outside every monitor, which happens when a
// layout from a different setup is restored, is moved onto the nearest
// monitor's work area (the taskbar-free portion of the monitor) and shrunk if
// it does not fit; when that monitor's work area is unknown (zero, e.g. a
// layout enumerated before store.Monitor.Work existed), the full monitor
// rectangle is used instead, matching the previous behaviour.
func Clamp(r store.Rect, ms []store.Monitor) store.Rect {
	if len(ms) == 0 {
		return r
	}
	for _, m := range ms {
		if r.X < m.X+m.W && r.X+r.W > m.X && r.Y < m.Y+m.H && r.Y+r.H > m.Y {
			return r
		}
	}

	target := placementArea(nearest(r, ms))
	out := r
	if out.W > target.W {
		out.W = target.W
	}
	if out.H > target.H {
		out.H = target.H
	}
	out.X = clampInt32(out.X, target.X, target.X+target.W-out.W)
	out.Y = clampInt32(out.Y, target.Y, target.Y+target.H-out.H)
	return out
}

// placementArea returns the rectangle a window should be placed inside on
// monitor m: its work area when known, falling back to the full monitor
// rectangle when Work is the zero value (W<=0 or H<=0), the same convention
// store.WindowEntry.Screen uses for "absent".
func placementArea(m store.Monitor) store.Rect {
	if m.Work.W > 0 && m.Work.H > 0 {
		return m.Work
	}
	return store.Rect{X: m.X, Y: m.Y, W: m.W, H: m.H}
}

// nearest returns the monitor whose centre is closest to the rectangle's centre.
func nearest(r store.Rect, ms []store.Monitor) store.Monitor {
	rx, ry := r.X+r.W/2, r.Y+r.H/2
	best, bestDist := ms[0], int64(-1)
	for _, m := range ms {
		dx := int64(rx - (m.X + m.W/2))
		dy := int64(ry - (m.Y + m.H/2))
		if d := dx*dx + dy*dy; bestDist < 0 || d < bestDist {
			best, bestDist = m, d
		}
	}
	return best
}

func clampInt32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
