package hotkey

import "testing"

func TestParseAcceptsGermanAndEnglishSpellings(t *testing.T) {
	want := Hotkey{Mods: ModCtrl | ModAlt, Key: 0x31} // VK for "1"
	for _, in := range []string{
		"Strg+Alt+1",
		"ctrl+alt+1",
		"CONTROL-ALT-1",
		"Steuerung + Alt + 1",
		"  strg alt 1  ",
		"Alt+Strg+1", // modifier order is irrelevant
	} {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): unexpected error %v", in, err)
		}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseKeyClasses(t *testing.T) {
	tests := []struct {
		in  string
		key uint32
	}{
		{"Strg+A", 0x41},
		{"Strg+z", 0x5A},
		{"Strg+0", 0x30},
		{"Strg+9", 0x39},
		{"Strg+F1", 0x70},
		{"Strg+f24", 0x87},
		{"Strg+Links", 0x25},
		{"Strg+Left", 0x25},
		{"Strg+Hoch", 0x26},
		{"Strg+Rechts", 0x27},
		{"Strg+Runter", 0x28},
		{"Strg+Pos1", 0x24},
		{"Strg+End", 0x23},
		{"Strg+BildAuf", 0x21},
		{"Strg+PageDown", 0x22},
		{"Strg+Einfg", 0x2D},
		{"Strg+Entf", 0x2E},
		{"Strg+Leertaste", 0x20},
		{"Strg+Esc", 0x1B},
		{"Strg+Tab", 0x09},
		{"Strg+Eingabe", 0x0D},
	}
	for _, tc := range tests {
		got, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q): unexpected error %v", tc.in, err)
		}
		if got.Key != tc.key {
			t.Errorf("Parse(%q).Key = %#x, want %#x", tc.in, got.Key, tc.key)
		}
	}
}

func TestParseEmptyIsNoHotkey(t *testing.T) {
	for _, in := range []string{"", "   "} {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): unexpected error %v", in, err)
		}
		if !got.IsZero() {
			t.Errorf("Parse(%q) = %+v, want the zero Hotkey", in, got)
		}
	}
}

func TestParseRejections(t *testing.T) {
	for _, in := range []string{
		"F5",           // no modifier at all
		"Strg",         // no key
		"Strg+Alt",     // still no key
		"Strg+A+B",     // two non-modifier keys
		"Strg+Zwiebel", // unknown key
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", in, got)
		}
	}
}

func TestFormatting(t *testing.T) {
	h := Hotkey{Mods: ModWin | ModShift | ModCtrl | ModAlt, Key: 0x70}
	if got, want := h.Canonical(), "Ctrl+Alt+Shift+Win+F1"; got != want {
		t.Errorf("Canonical() = %q, want %q", got, want)
	}
	if got, want := h.Label(), "Strg+Alt+Umschalt+Win+F1"; got != want {
		t.Errorf("Label() = %q, want %q", got, want)
	}
	if got := (Hotkey{}).Canonical(); got != "" {
		t.Errorf("zero Hotkey Canonical() = %q, want empty", got)
	}
}

func TestFormatParseRoundTripModifiers(t *testing.T) {
	for _, h := range []Hotkey{
		{Mods: ModCtrl, Key: 0x41},
		{Mods: ModCtrl | ModAlt, Key: 0x31},
		{Mods: ModWin | ModShift, Key: 0x25},
		{Mods: ModAlt, Key: 0x87},
	} {
		for _, s := range []string{h.Canonical(), h.Label()} {
			back, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q): unexpected error %v", s, err)
			}
			if back != h {
				t.Errorf("Parse(%q) = %+v, want %+v", s, back, h)
			}
		}
	}
}

// TestFormatParseRoundTripNamedKeys drives the round trip from namedKeys
// itself, rather than from a hand-picked subset, so every named key is
// checked in both its Canonical() (English, ASCII) and Label() (German,
// sometimes non-ASCII, e.g. Bild↑/Bild↓) forms, and a future table entry
// cannot be added without gaining round-trip coverage automatically.
func TestFormatParseRoundTripNamedKeys(t *testing.T) {
	for _, k := range namedKeys {
		h := Hotkey{Mods: ModCtrl, Key: k.vk}
		for _, s := range []string{h.Canonical(), h.Label()} {
			back, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q): unexpected error %v", s, err)
			}
			if back != h {
				t.Errorf("Parse(%q) = %+v, want %+v", s, back, h)
			}
		}
	}
}
