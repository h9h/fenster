package store

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store is an in-memory copy of the layout file plus the operations that keep
// it and the file in sync. It is not safe for concurrent use; the application
// drives it from the single UI thread.
type Store struct {
	path string
	file File
}

// New returns a store backed by path. Nothing is read until Load is called.
func New(path string) *Store {
	return &Store{path: path, file: File{Version: SchemaVersion}}
}

// DefaultPath is %APPDATA%\fenster\layouts.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating APPDATA: %w", err)
	}
	return filepath.Join(dir, "fenster", "layouts.json"), nil
}

// Load reads the file. A missing file yields an empty store. A file that fails
// to parse is renamed to <path>.broken-<timestamp> and its new name returned,
// so the caller can tell the user; the store then starts empty. A file written
// by a newer schema version is refused with an error and left untouched.
func (s *Store) Load() (string, error) {
	blob, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.file = File{Version: SchemaVersion}
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", s.path, err)
	}

	var f File
	if err := json.Unmarshal(blob, &f); err != nil {
		broken := fmt.Sprintf("%s.broken-%s", s.path, time.Now().Format("20060102-150405"))
		if renameErr := os.Rename(s.path, broken); renameErr != nil {
			return "", fmt.Errorf("parsing %s failed (%v) and it could not be moved aside: %w", s.path, err, renameErr)
		}
		s.file = File{Version: SchemaVersion}
		return broken, nil
	}
	if f.Version > SchemaVersion {
		return "", fmt.Errorf("%s was written by a newer version (schema %d, this build understands %d)", s.path, f.Version, SchemaVersion)
	}
	f.Version = SchemaVersion
	s.file = f
	return "", nil
}

// Save writes the file atomically: a temp file in the same directory, then a
// rename over the target.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(s.path), err)
	}
	blob, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding layouts: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".layouts-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	// Sync before Close: the rename below is atomic, but without this the
	// written bytes may still only exist in the OS page cache, so a crash or
	// power loss between the rename and the next flush could leave the
	// renamed file's on-disk content stale or truncated despite the rename
	// itself having "succeeded".
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replacing %s: %w", s.path, err)
	}
	return nil
}

// Layouts returns the layouts in storage order.
func (s *Store) Layouts() []Layout { return s.file.Layouts }

// Get returns the layout with the given id.
func (s *Store) Get(id string) (Layout, bool) {
	for _, l := range s.file.Layouts {
		if l.ID == id {
			return l, true
		}
	}
	return Layout{}, false
}

// Add appends a layout.
func (s *Store) Add(l Layout) { s.file.Layouts = append(s.file.Layouts, l) }

// Replace swaps the layout with the given id for l.
func (s *Store) Replace(id string, l Layout) error {
	i, err := s.indexOf(id)
	if err != nil {
		return err
	}
	s.file.Layouts[i] = l
	return nil
}

// Delete removes the layout with the given id.
func (s *Store) Delete(id string) error {
	i, err := s.indexOf(id)
	if err != nil {
		return err
	}
	s.file.Layouts = append(s.file.Layouts[:i], s.file.Layouts[i+1:]...)
	return nil
}

// SetInclude flips the persisted checkbox of one window entry.
func (s *Store) SetInclude(id string, windowIdx int, include bool) error {
	i, err := s.indexOf(id)
	if err != nil {
		return err
	}
	if windowIdx < 0 || windowIdx >= len(s.file.Layouts[i].Windows) {
		return fmt.Errorf("window index %d out of range for layout %s", windowIdx, id)
	}
	s.file.Layouts[i].Windows[windowIdx].Include = include
	s.file.Layouts[i].Updated = time.Now()
	return nil
}

func (s *Store) indexOf(id string) (int, error) {
	for i, l := range s.file.Layouts {
		if l.ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no layout with id %s", id)
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID returns a short random identifier for a layout.
func NewID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failing is fatal for uniqueness; fall back to the clock.
		return strings.ToLower(time.Now().Format("150405"))
	}
	return idEncoding.EncodeToString(buf[:])[:6]
}

// MinimizeOthers reports whether restoring a layout should minimize the open
// windows that layout does not account for. It defaults to true: an absent
// key in layouts.json — every file written before the option existed — means
// the feature is on, so the default does not depend on the file having been
// rewritten.
func (s *Store) MinimizeOthers() bool {
	if s.file.MinimizeOthers == nil {
		return true
	}
	return *s.file.MinimizeOthers
}

// SetMinimizeOthers records the option. It always writes a concrete value,
// so an explicit false is persisted rather than collapsing back to "unset"
// and defaulting on again.
func (s *Store) SetMinimizeOthers(v bool) { s.file.MinimizeOthers = &v }
