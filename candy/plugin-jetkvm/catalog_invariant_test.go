package jetkvm

// catalog_invariant_test.go pins the invariant the pr-validator caught breaking
// twice: every method the SCHEMA advertises must be reachable — either
// dispatched by runMethod or explicitly refused by methodSafety — and every
// method runMethod dispatches must be in the schema. Without this, a method can
// sit in the catalog as a phantom (advertised but unimplemented), which makes
// the schema and the skill that documents it actively mislead an agent that
// dispatches on it.
//
// The tests read the REAL sources (the CUE schema, catalog.go's runMethod, and
// the production readOnlyMethods / neverAutonomous sets) rather than restating
// any of them, so they cannot drift alongside the code they guard.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// schemaMethods returns the sorted method names the SERVED #JetkvmMethod enum
// accepts.
//
// It reads the schema through the CUE API (servedSchema, schema_served_test.go)
// instead of parsing its text, so the enum's shape — §10.1 S3: a vocabulary
// table plus a derived word list — can change without silently truncating the
// read. The old text parse stopped at the next column-0 declaration; a derived
// enum has no such body, so the parse would have found ZERO methods and left
// every invariant below passing vacuously.
func schemaMethods(t *testing.T) []string {
	t.Helper()
	return schemaWordList(t, servedSchema(t), "#JetkvmMethod")
}

// dispatchedMethods returns the sorted method names runMethod routes. Cases may
// be grouped (`case "a", "b":`), so each case line is split on commas.
func dispatchedMethods(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("catalog.go")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	fn := regexp.MustCompile(`(?s)func runMethod\(.*?\n\}`).FindString(string(b))
	if fn == "" {
		t.Fatal("could not locate runMethod in catalog.go")
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`case ([^:]+):`).FindAllStringSubmatch(fn, -1) {
		for _, part := range strings.Split(m[1], ",") {
			name := strings.Trim(strings.TrimSpace(part), `"`)
			if name != "" {
				seen[name] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// TestServedCatalogCoversTheWholeEnum guards the READER and the served catalog in
// one step: the enum's first and last entries must be present, so neither a
// truncated read nor a table whose tail never reaches the enum can exempt the
// catalog. `status` is the first method and `rpc` the last.
func TestServedCatalogCoversTheWholeEnum(t *testing.T) {
	got := schemaMethods(t)
	has := func(name string) bool {
		for _, m := range got {
			if m == name {
				return true
			}
		}
		return false
	}
	if !has("status") {
		t.Errorf("served enum is missing its HEAD (status) — got %d methods: %v", len(got), got)
	}
	if !has("rpc") {
		t.Errorf("served enum is missing its TAIL (rpc) — the derivation is losing rows; got %d methods: %v", len(got), got)
	}
	if len(got) < 60 {
		t.Errorf("read only %d methods — the served #JetkvmMethod derivation looks truncated", len(got))
	}
}

// TestCatalogHasNoPhantomMethods asserts every schema method is either
// dispatched or refuse-before-dispatch — no method is advertised that the plugin
// cannot act on. The refusal set is read from PRODUCTION (NeverAutonomousMethods)
// rather than restated, so it cannot keep passing if the real set shrinks.
func TestCatalogHasNoPhantomMethods(t *testing.T) {
	reachable := map[string]bool{}
	for _, m := range dispatchedMethods(t) {
		reachable[m] = true
	}
	refused := NeverAutonomousMethods()
	for m := range refused {
		reachable[m] = true
	}
	for _, m := range schemaMethods(t) {
		if !reachable[m] {
			t.Errorf("schema method %q is a PHANTOM: not dispatched by runMethod and not refused by methodSafety — remove it from the schema or implement it", m)
		}
	}
}

// TestRefusedSetIsRefusedBySafety cross-checks the exported refusal set against
// methodSafety's actual behaviour, so the invariant cannot be satisfied by a
// set that production does not honour.
func TestRefusedSetIsRefusedBySafety(t *testing.T) {
	for m := range NeverAutonomousMethods() {
		if skip, _ := methodSafety(m, true); !skip {
			t.Errorf("NeverAutonomousMethods names %q, but methodSafety does NOT refuse it even with allow_control", m)
		}
	}
}

// TestNoUncatalogedDispatch asserts the inverse: runMethod must not dispatch a
// method the schema does not advertise, which would make an authored step fail
// schema validation while the code silently supports it.
func TestNoUncatalogedDispatch(t *testing.T) {
	inSchema := map[string]bool{}
	for _, m := range schemaMethods(t) {
		inSchema[m] = true
	}
	for _, m := range dispatchedMethods(t) {
		if !inSchema[m] {
			t.Errorf("runMethod dispatches %q, which is NOT in #JetkvmMethod — add it to the schema or remove the case", m)
		}
	}
}

// TestReadOnlyMethodsAreCataloged asserts the read-only allowlist names only
// advertised methods, so the safety classification cannot drift from the
// catalog either.
func TestReadOnlyMethodsAreCataloged(t *testing.T) {
	inSchema := map[string]bool{}
	for _, m := range schemaMethods(t) {
		inSchema[m] = true
	}
	for m := range readOnlyMethods {
		if !inSchema[m] {
			t.Errorf("readOnlyMethods classifies %q, which is NOT in #JetkvmMethod — the classification has drifted from the catalog", m)
		}
	}
}

// TestReadOnlyAndRefusedDoNotOverlap asserts the classification is a true
// partition: a method cannot be both read-only and refused-before-dispatch.
func TestReadOnlyAndRefusedDoNotOverlap(t *testing.T) {
	for m := range NeverAutonomousMethods() {
		if readOnlyMethods[m] {
			t.Errorf("%q is in BOTH readOnlyMethods and neverAutonomous — the classification is contradictory", m)
		}
	}
}

// TestActionEnumCoversEveryActionBearingMethod asserts the #JetkvmAction CUE enum
// is a SUPERSET of every verb the action-bearing methods accept, so the
// validate-time narrowing (`action?: string` -> `#JetkvmAction`) cannot reject a
// verb the code supports. The accepted verbs are read from PRODUCTION (the
// `case ...` arms of methodPower/methodDCPower/methodVirtualMedia/methodSetVideo/
// methodSetDisplay in catalog.go), and the enum is read from the schema — neither
// is restated.
func TestActionEnumCoversEveryActionBearingMethod(t *testing.T) {
	src, err := os.ReadFile("catalog.go")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	code := string(src)
	// The enum verbs, read from the served #JetkvmAction exactly as the host
	// reads them when it validates an authored `action:`.
	enum := map[string]bool{}
	for _, w := range schemaWordList(t, servedSchema(t), "#JetkvmAction") {
		enum[w] = true
	}
	// The verbs each action-bearing method accepts, from its `case "a", "b":` arms.
	methods := []string{"methodPower", "methodDCPower", "methodVirtualMedia", "methodSetVideo", "methodSetDisplay"}
	accepted := map[string]bool{}
	for _, fn := range methods {
		body := regexp.MustCompile(`(?s)func ` + fn + `\(.*?\n\}`).FindString(code)
		if body == "" {
			t.Fatalf("could not locate %s in catalog.go", fn)
		}
		for _, m := range regexp.MustCompile(`case ([^:]+):`).FindAllStringSubmatch(body, -1) {
			for _, part := range strings.Split(m[1], ",") {
				name := strings.Trim(strings.TrimSpace(part), `"`)
				if name != "" && !strings.Contains(name, " ") && name != "default" {
					accepted[name] = true
				}
			}
		}
	}
	if len(accepted) == 0 {
		t.Fatal("parsed no accepted action verbs — the parser is broken")
	}
	for v := range accepted {
		if !enum[v] {
			t.Errorf("#JetkvmAction is missing %q, which an action-bearing method accepts — the enum narrowing would reject a valid value", v)
		}
	}
}
