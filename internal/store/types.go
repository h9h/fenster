// Package store holds the shared data types of the application and persists
// layouts to a JSON file. It has no dependencies, so every other package may
// import it.
package store

import "time"

// SchemaVersion is the version of the on-disk format this binary writes.
const SchemaVersion = 1

// Rect is a window or monitor rectangle in physical pixels of the virtual screen.
type Rect struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
	W int32 `json:"w"`
	H int32 `json:"h"`
}

// Monitor describes one display in the virtual screen.
type Monitor struct {
	X       int32 `json:"x"`
	Y       int32 `json:"y"`
	W       int32 `json:"w"`
	H       int32 `json:"h"`
	Scale   int   `json:"scale"` // DPI scaling in percent, 100 == 96 dpi
	Primary bool  `json:"primary"`
}

// Setup is the monitor arrangement a layout was captured on.
type Setup struct {
	Fingerprint string    `json:"fingerprint"`
	Label       string    `json:"label"`
	Monitors    []Monitor `json:"monitors"`
}

// WindowState is the show state of a window.
type WindowState string

const (
	StateNormal    WindowState = "normal"
	StateMinimized WindowState = "minimized"
	StateMaximized WindowState = "maximized"
)

// WindowEntry is one saved window inside a layout.
type WindowEntry struct {
	Exe     string      `json:"exe"`
	Class   string      `json:"class"`
	Title   string      `json:"title"`
	Ordinal int         `json:"ordinal"` // n-th window of Exe at save time
	Rect    Rect        `json:"rect"`    // restored rectangle, even when maximized
	State   WindowState `json:"state"`
	Topmost bool        `json:"topmost"`
	Include bool        `json:"include"` // persisted checkbox state
}

// Layout is a named arrangement captured on one monitor setup.
type Layout struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Created time.Time     `json:"created"`
	Updated time.Time     `json:"updated"`
	Setup   Setup         `json:"setup"`
	Windows []WindowEntry `json:"windows"`
}

// File is the root of the JSON document.
type File struct {
	Version int      `json:"version"`
	Layouts []Layout `json:"layouts"`
}
