package jetkvm

// install_test.go pins the jetkvm transport's CONVERSION into the shared
// console-wizard engine and the fail-fast on a missing recipe. The engine's pure
// logic (plan building, answer substitution, validation) is unit-tested where it
// lives — sdk/kit/console_test.go — so it is not duplicated here (R3).

import (
	"testing"

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
