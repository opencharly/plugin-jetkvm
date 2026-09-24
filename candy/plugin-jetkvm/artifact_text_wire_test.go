package jetkvm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
)

// The sdk's artifact pipeline reads these modifiers out of the desugared
// plugin-input MAP by string key: inputString(op, "artifact_contains_text") /
// inputInt(op, "artifact_min_bytes"). Nothing type-checks that lookup against
// this plugin's schema — they meet only at runtime, through a map, so a rename
// or typo on either side compiles and runs, the sdk finds no key, reads "", and
// SKIPS the validator: the step passes while asserting nothing.
//
// This pins the wire name the schema generates against the literal the sdk looks
// up (the same guard candy/plugin-wl carries for its ocr).
func TestArtifactContainsText_WireNameMatchesTheSdkLookupKey(t *testing.T) {
	b, err := json.Marshal(params.JetkvmInput{
		Method:               "screenshot",
		ArtifactMinBytes:     20000,
		ArtifactContainsText: "omarch",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := round["artifact_contains_text"]; !ok || got != "omarch" {
		t.Fatalf("JetkvmInput does not serialise the %q key the sdk looks up (got %v); the OCR "+
			"assertion would silently be skipped", "artifact_contains_text", round)
	}
	if _, ok := round["artifact_min_bytes"]; !ok {
		t.Fatalf("JetkvmInput does not serialise artifact_min_bytes; keys=%v", round)
	}
}

// The schema is the single source the params struct is generated from, so the
// field must exist there too — a hand-edited params struct would pass the test
// above while the authored YAML failed validation against the served schema.
func TestArtifactContainsText_DeclaredInTheServedSchema(t *testing.T) {
	b, err := schemaFS.ReadFile("schema/jetkvm.cue")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if !strings.Contains(string(b), "artifact_contains_text?:") {
		t.Error("schema/jetkvm.cue declares no artifact_contains_text field, so an authored step " +
			"using it would be REJECTED by the host's plugin-input validation")
	}
}
