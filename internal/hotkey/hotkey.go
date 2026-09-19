// Package hotkey describes global key combinations as data: parsing what a
// user typed, formatting one for storage or for the menu, and deciding
// which of a store's layouts should have a combination registered right
// now. It contains no Win32 calls and no build tag, so all of it is
// exercised by a plain `go test ./...` on any machine; only the
// registration itself, in internal/win32, needs a desktop.
package hotkey

import (
	"errors"
	"fmt"
	"strings"
)

// Mod is a bitmask of modifier keys. These are the package's own values,
// not the Win32 MOD_* constants: the translation to Win32 lives at the one
// place that talks to RegisterHotKey (win32Mods in cmd/fenster/hotkeys.go),
// so this package stays free of platform constants.
type Mod uint32

const (
	ModAlt Mod = 1 << iota
	ModCtrl
	ModShift
	ModWin
)

// Hotkey is a modifier combination plus exactly one key. The zero value
// means "no hotkey". Key is a Win32 virtual-key code: that is what
// identifies a key to RegisterHotKey, and a private enum in front of it
// would buy nothing but a translation table.
type Hotkey struct {
	Mods Mod
	Key  uint32
}

// IsZero reports whether h is "no hotkey".
func (h Hotkey) IsZero() bool { return h.Mods == 0 && h.Key == 0 }

// modNames maps every accepted spelling of a modifier, lowercased, to its
// bit. Both German and English names are accepted, because the UI is German
// but the canonical storage form is English and both must parse.
var modNames = map[string]Mod{
	"strg": ModCtrl, "ctrl": ModCtrl, "control": ModCtrl, "steuerung": ModCtrl,
	"alt":      ModAlt,
	"umschalt": ModShift, "shift": ModShift,
	"win": ModWin, "windows": ModWin,
}

// namedKey is one key that is not a letter, digit or function key.
// canonical is the English storage spelling, label the German one shown in
// the menu, and aliases are further spellings accepted on input. canonical
// and label are always accepted too, lowercased, so they need not be
// repeated in aliases.
type namedKey struct {
	vk        uint32
	canonical string
	label     string
	aliases   []string
}

var namedKeys = []namedKey{
	{0x25, "Left", "Links", nil},
	{0x26, "Up", "Hoch", []string{"auf"}},
	{0x27, "Right", "Rechts", nil},
	{0x28, "Down", "Runter", []string{"ab"}},
	{0x24, "Home", "Pos1", nil},
	{0x23, "End", "Ende", nil},
	{0x21, "PageUp", "Bild↑", []string{"bildauf", "bildhoch"}},
	{0x22, "PageDown", "Bild↓", []string{"bildab", "bildrunter"}},
	{0x2D, "Insert", "Einfg", []string{"einfuegen"}},
	{0x2E, "Delete", "Entf", []string{"entfernen", "del"}},
	{0x20, "Space", "Leertaste", nil},
	{0x1B, "Escape", "Esc", nil},
	{0x09, "Tab", "Tab", []string{"tabulator"}},
	{0x0D, "Enter", "Eingabe", []string{"return"}},
}

// keyByName is every accepted key spelling, lowercased, to its virtual-key
// code. Letters, digits and function keys are folded in here too rather
// than special-cased in Parse, so there is exactly one lookup.
var keyByName = func() map[string]uint32 {
	m := map[string]uint32{}
	for c := byte('a'); c <= 'z'; c++ {
		m[string(c)] = uint32(c-'a') + 0x41
	}
	for d := byte('0'); d <= '9'; d++ {
		m[string(d)] = uint32(d-'0') + 0x30
	}
	for i := 1; i <= 24; i++ {
		m[fmt.Sprintf("f%d", i)] = uint32(0x70 + i - 1)
	}
	for _, k := range namedKeys {
		m[strings.ToLower(k.canonical)] = k.vk
		m[strings.ToLower(k.label)] = k.vk
		for _, a := range k.aliases {
			m[a] = k.vk
		}
	}
	return m
}()

// isSeparator reports whether r separates two tokens of a combination. All
// three of "+", "-" and a space are accepted because all three are things
// people actually type; no supported key name contains any of them.
func isSeparator(r rune) bool { return r == '+' || r == '-' || r == ' ' }

// Parse reads a combination as a human would write it. It is deliberately
// lenient about spelling, case and separators, and strict about meaning.
// The empty string is not an error: it parses to the zero Hotkey, which is
// how "clear this binding" is expressed. Error texts are German because
// they are shown to the user verbatim in a balloon.
func Parse(s string) (Hotkey, error) {
	if strings.TrimSpace(s) == "" {
		return Hotkey{}, nil
	}

	var h Hotkey
	haveKey := false
	for _, token := range strings.FieldsFunc(s, isSeparator) {
		lower := strings.ToLower(token)
		if m, ok := modNames[lower]; ok {
			h.Mods |= m
			continue
		}
		vk, ok := keyByName[lower]
		if !ok {
			return Hotkey{}, fmt.Errorf("unbekannte Taste %q", token)
		}
		if haveKey {
			return Hotkey{}, errors.New("nur eine Taste neben den Modifikatoren erlaubt")
		}
		h.Key = vk
		haveKey = true
	}

	if !haveKey {
		return Hotkey{}, errors.New("es fehlt eine Taste, z. B. Strg+Alt+1")
	}
	// A combination without a modifier would be swallowed system-wide, from
	// every application, for as long as fenster runs. Nobody asks for that
	// on purpose, so it is refused rather than granted.
	if h.Mods == 0 {
		return Hotkey{}, errors.New("mindestens ein Modifikator (Strg, Alt, Umschalt oder Win) ist nötig")
	}
	return h, nil
}

// Canonical is the storage form written to layouts.json: English,
// "+"-separated, modifiers in the fixed order Ctrl, Alt, Shift, Win. The
// fixed order means one combination always produces one string, so two
// bindings are compared as two Hotkey values and never as two spellings.
func (h Hotkey) Canonical() string { return h.format(false) }

// Label is the display form shown in the tray menu: German, same order.
func (h Hotkey) Label() string { return h.format(true) }

func (h Hotkey) format(german bool) string {
	if h.IsZero() {
		return ""
	}
	var parts []string
	if h.Mods&ModCtrl != 0 {
		parts = append(parts, pick(german, "Strg", "Ctrl"))
	}
	if h.Mods&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if h.Mods&ModShift != 0 {
		parts = append(parts, pick(german, "Umschalt", "Shift"))
	}
	if h.Mods&ModWin != 0 {
		parts = append(parts, "Win")
	}
	return strings.Join(append(parts, keyName(h.Key, german)), "+")
}

func pick(german bool, de, en string) string {
	if german {
		return de
	}
	return en
}

// keyName is the inverse of the keyByName lookup. A virtual-key code that
// no name maps to can only come from a hand-built Hotkey, never from Parse;
// it is rendered as a hex code rather than dropped, so a menu row showing
// it is at least diagnosable.
func keyName(vk uint32, german bool) string {
	switch {
	case vk >= 0x41 && vk <= 0x5A:
		return string(rune('A' + vk - 0x41))
	case vk >= 0x30 && vk <= 0x39:
		return string(rune('0' + vk - 0x30))
	case vk >= 0x70 && vk <= 0x87:
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	for _, k := range namedKeys {
		if k.vk == vk {
			return pick(german, k.label, k.canonical)
		}
	}
	return fmt.Sprintf("VK%02X", vk)
}
