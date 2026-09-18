package autostart

import "testing"

func testEntry(t *testing.T) Entry {
	t.Helper()
	e := Entry{KeyPath: `Software\fenster-test\Run`, ValueName: "fenster"}
	t.Cleanup(func() { _ = e.Disable() })
	return e
}

func TestEnableThenReportsEnabled(t *testing.T) {
	e := testEntry(t)
	exe := `C:\tools\fenster.exe`

	if on, err := e.Enabled(exe); err != nil || on {
		t.Fatalf("before Enable: on=%v err=%v, want false/nil", on, err)
	}
	if err := e.Enable(exe); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if on, err := e.Enabled(exe); err != nil || !on {
		t.Fatalf("after Enable: on=%v err=%v, want true/nil", on, err)
	}
}

func TestEnabledIsFalseForADifferentExecutable(t *testing.T) {
	e := testEntry(t)
	if err := e.Enable(`C:\old\fenster.exe`); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if on, err := e.Enabled(`C:\new\fenster.exe`); err != nil || on {
		t.Errorf("stale entry should not count as enabled: on=%v err=%v", on, err)
	}
}

func TestEnableIsIdempotentAndDisableRemoves(t *testing.T) {
	e := testEntry(t)
	exe := `C:\tools\fenster.exe`
	if err := e.Enable(exe); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := e.Enable(exe); err != nil {
		t.Fatalf("second Enable: %v", err)
	}
	if err := e.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if on, err := e.Enabled(exe); err != nil || on {
		t.Errorf("after Disable: on=%v err=%v, want false/nil", on, err)
	}
	if err := e.Disable(); err != nil {
		t.Errorf("Disable on a missing value must succeed, got %v", err)
	}
}

func TestDefaultPointsAtTheRunKey(t *testing.T) {
	d := Default()
	if d.KeyPath != `Software\Microsoft\Windows\CurrentVersion\Run` || d.ValueName != "fenster" {
		t.Errorf("Default = %+v", d)
	}
}
