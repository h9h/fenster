package monitors

import (
	"regexp"
	"testing"

	"fenster/internal/store"
)

var docked = []store.Monitor{
	{X: -1747, Y: -1440, W: 5120, H: 1440, Scale: 100},
	{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150, Primary: true},
}

func TestFingerprintIsStableAcrossEnumerationOrder(t *testing.T) {
	reversed := []store.Monitor{docked[1], docked[0]}
	if Fingerprint(docked) != Fingerprint(reversed) {
		t.Errorf("fingerprint depends on enumeration order")
	}
}

func TestFingerprintIsEightHexChars(t *testing.T) {
	fp := Fingerprint(docked)
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(fp) {
		t.Errorf("fingerprint = %q, want 8 hex chars", fp)
	}
}

func TestFingerprintIgnoresAbsolutePositionButKeepsArrangement(t *testing.T) {
	// The whole virtual desktop shifted: same arrangement relative to primary.
	shifted := []store.Monitor{
		{X: -1747 + 500, Y: -1440 + 500, W: 5120, H: 1440, Scale: 100},
		{X: 500, Y: 500, W: 1710, H: 1073, Scale: 150, Primary: true},
	}
	if Fingerprint(docked) != Fingerprint(shifted) {
		t.Errorf("fingerprint should be relative to the primary monitor")
	}

	// Secondary monitor moved from above to the left of the primary.
	rearranged := []store.Monitor{
		{X: -5120, Y: 0, W: 5120, H: 1440, Scale: 100},
		{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150, Primary: true},
	}
	if Fingerprint(docked) == Fingerprint(rearranged) {
		t.Errorf("a different arrangement must produce a different fingerprint")
	}
}

func TestFingerprintDistinguishesResolutionScaleAndCount(t *testing.T) {
	base := Fingerprint(docked)

	otherRes := []store.Monitor{docked[0], {X: 0, Y: 0, W: 1920, H: 1080, Scale: 150, Primary: true}}
	if Fingerprint(otherRes) == base {
		t.Errorf("different resolution must change the fingerprint")
	}

	otherScale := []store.Monitor{docked[0], {X: 0, Y: 0, W: 1710, H: 1073, Scale: 100, Primary: true}}
	if Fingerprint(otherScale) == base {
		t.Errorf("different DPI scaling must change the fingerprint")
	}

	single := []store.Monitor{{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150, Primary: true}}
	if Fingerprint(single) == base {
		t.Errorf("different monitor count must change the fingerprint")
	}
}

func TestLabel(t *testing.T) {
	if got, want := Label(docked), "2 Monitore · 5120×1440 + 1710×1073"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
	single := []store.Monitor{{W: 1710, H: 1073, Scale: 150, Primary: true}}
	if got, want := Label(single), "1 Monitor · 1710×1073"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
	if got, want := Label(nil), "Kein Monitor erkannt"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
}

func TestDescribeCombinesBoth(t *testing.T) {
	got := Describe(docked)
	if got.Fingerprint != Fingerprint(docked) || got.Label != Label(docked) {
		t.Errorf("Describe disagrees with Fingerprint/Label: %+v", got)
	}
	if len(got.Monitors) != 2 {
		t.Errorf("Describe should carry the monitors, got %d", len(got.Monitors))
	}
}

func TestFingerprintIsTranslationInvariantWithoutAPrimaryMonitor(t *testing.T) {
	// No primary monitor set, arrangement identical but shifted.
	noPrimary := []store.Monitor{
		{X: -1747, Y: -1440, W: 5120, H: 1440, Scale: 100},
		{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150},
	}
	shifted := []store.Monitor{
		{X: -1747 + 500, Y: -1440 + 500, W: 5120, H: 1440, Scale: 100},
		{X: 500, Y: 500, W: 1710, H: 1073, Scale: 150},
	}
	if Fingerprint(noPrimary) != Fingerprint(shifted) {
		t.Errorf("fingerprint should be translation-invariant even without primary monitor")
	}

	// The same arrangement but with a primary flag should differ from noPrimary.
	withPrimary := []store.Monitor{
		{X: -1747, Y: -1440, W: 5120, H: 1440, Scale: 100},
		{X: 0, Y: 0, W: 1710, H: 1073, Scale: 150, Primary: true},
	}
	if Fingerprint(noPrimary) == Fingerprint(withPrimary) {
		t.Errorf("primary flag should affect the fingerprint (canonical includes *)")
	}
}
