package layout

import "fenster/internal/store"

// Clamp keeps a target rectangle reachable. A rectangle that overlaps any
// monitor is returned unchanged — a window deliberately hanging off an edge
// stays where it was. A rectangle entirely outside every monitor, which
// happens when a layout from a different setup is restored, is moved onto the
// nearest monitor and shrunk if it does not fit.
func Clamp(r store.Rect, ms []store.Monitor) store.Rect {
	if len(ms) == 0 {
		return r
	}
	for _, m := range ms {
		if r.X < m.X+m.W && r.X+r.W > m.X && r.Y < m.Y+m.H && r.Y+r.H > m.Y {
			return r
		}
	}

	target := nearest(r, ms)
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
