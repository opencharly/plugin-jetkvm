package jetkvm

// install_test.go pins the jetkvm transport's CONVERSION into the shared
// console-wizard engine and the fail-fast on a missing recipe. The engine's pure
// logic (plan building, answer substitution, validation) is unit-tested where it
// lives — sdk/kit/console_test.go — so it is not duplicated here (R3).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
)

// TestHasSteps pins the "recipe present" predicate used for the fail-fast. A
// bare `device:` ref COUNTS: the provider resolves the referenced entity into
// steps before runInstall, so by then the recipe is populated.
func TestHasSteps(t *testing.T) {
	if hasSteps(&params.JetkvmInput{}) {
		t.Error("empty input must have no steps")
	}
	if !hasSteps(&params.JetkvmInput{Device: "omarchy-kvm"}) {
		t.Error("a device ref provides a recipe (resolved before runInstall) and must count")
	}
	if !hasSteps(&params.JetkvmInput{Steps: []params.JetkvmInstallStep{{WaitFor: "x"}}}) {
		t.Error("inline steps must count")
	}
}

// TestConsoleStepsToParams verifies the shared-kit step converts into the
// schema-generated param type (the one conversion the transport owns).
func TestConsoleStepsToParams(t *testing.T) {
	out := consoleStepsToParams([]kit.ConsoleStep{{WaitFor: "Username>", Action: "type", Text: "rdduser"}})
	if len(out) != 1 || out[0].Text != "rdduser" || out[0].WaitFor != "Username>" {
		t.Fatalf("conversion: %+v", out)
	}
}

// TestKeyInput / TestTypeInput pin the exact JetkvmInput the console engine
// drives for each action — the transport's contract, unit-locked with no device.
func TestKeyInput(t *testing.T) {
	k := keyInput("key", "Return", 120)
	if k.Method != "key" || k.KeyName != "Return" || !k.AllowControl || k.HoldMs != 120 {
		t.Fatalf("key input wrong: %+v", k)
	}
	c := keyInput("key-combo", "ctrl+c", 0)
	if c.Method != "key-combo" || c.Combo != "ctrl+c" || !c.AllowControl {
		t.Fatalf("combo input wrong: %+v", c)
	}
	ty := typeInput("hello")
	if ty.Method != "type" || ty.Text != "hello" || !ty.AllowControl {
		t.Fatalf("type input wrong: %+v", ty)
	}
}

// scriptedTransport replays a sequence of screens (advancing on each Capture)
// and records the input it receives — a deterministic stand-in for the device.
type scriptedTransport struct {
	screens []string
	idx     int
	keys    []string
	types   []string
}

func (s *scriptedTransport) Capture(context.Context) ([]byte, error) {
	scr := s.screens[s.idx]
	if s.idx < len(s.screens)-1 {
		s.idx++
	}
	return []byte(scr), nil
}
func (s *scriptedTransport) PressKey(_ context.Context, k string) error {
	s.keys = append(s.keys, k)
	return nil
}
func (s *scriptedTransport) PressCombo(_ context.Context, c string) error {
	s.keys = append(s.keys, "combo:"+c)
	return nil
}
func (s *scriptedTransport) Type(_ context.Context, t string) error {
	s.types = append(s.types, t)
	return nil
}

// TestInstall_MultiStepDistinctAnchors is the DETERMINISTIC proof of the
// headline behaviour the live single-screen bed cannot show: the engine walks an
// ORDERED multi-step recipe, and each step waits for its OWN distinct anchor
// (so step 2 does NOT proceed on step 1's screen). The transport replays
// screen1 -> screen2, and OCR is identity over the screen bytes.
func TestInstall_MultiStepDistinctAnchors(t *testing.T) {
	tr := &scriptedTransport{screens: []string{
		"screen-one: Press Return to Start Install",
		"screen-two: Select keyboard layout",
	}}
	steps := []kit.ConsoleStep{
		{WaitFor: "Press Return to Start Install", Action: "key", Key: "Return"},
		{WaitFor: "Select keyboard layout", Action: "key", Key: "F5"},
	}
	idOCR := func(b []byte) (string, error) { return string(b), nil }
	out, err := runInstallWith(context.Background(), steps, nil, tr, idOCR)
	if err != nil {
		t.Fatalf("runInstallWith: %v", err)
	}
	if !strings.Contains(out, "step 2") {
		t.Fatalf("both steps must run, got %q", out)
	}
	// The key order proves the SECOND step only fired after the SECOND screen:
	// if step 2's anchor were missing, the engine would not have reached it.
	if len(tr.keys) != 2 || tr.keys[0] != "Return" || tr.keys[1] != "F5" {
		t.Fatalf("per-step input order wrong: %+v", tr.keys)
	}
}

// TestInstall_DesyncIsCaught pins the ANTI-vacuity property: a step whose anchor
// never appears on the replayed screens must FAIL (the engine must not sail
// through on a stale screen). Here the first anchor is absent entirely.
func TestInstall_DesyncIsCaught(t *testing.T) {
	tr := &scriptedTransport{screens: []string{"only-screen"}}
	steps := []kit.ConsoleStep{{WaitFor: "never-on-any-screen", Action: "key", Key: "Return"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	idOCR := func(b []byte) (string, error) { return string(b), nil }
	if _, err := runInstallWith(ctx, steps, nil, tr, idOCR); err == nil {
		t.Fatal("a step whose anchor never appears must fail (not pass vacuously)")
	}
}
