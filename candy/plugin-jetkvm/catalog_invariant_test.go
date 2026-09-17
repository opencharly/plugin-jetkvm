package jetkvm

// catalog_invariant_test.go pins the invariant the pr-validator caught breaking
// twice: every method the SCHEMA advertises must be reachable — either
// dispatched by runMethod or explicitly refused by methodSafety — and every
// method runMethod dispatches must be in the schema. Without this, a method can
// sit in the catalog as a phantom (advertised but unimplemented), which makes
// the schema and the skill that documents it actively mislead.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// schemaMethods extracts the quoted method names from #JetkvmMethod in the CUE
// schema. Reading the source (rather than the generated params) is deliberate:
// the method enum is not a struct field, so only the .cue carries it.
func schemaMethods(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("schema/jetkvm.cue")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	block := regexp.MustCompile(`(?s)#JetkvmMethod: string &(.*?)\n\n`).FindStringSubmatch(string(b))
	if block == nil {
		t.Fatal("could not locate the #JetkvmMethod block in schema/jetkvm.cue")
	}
	names := regexp.MustCompile(`"([a-z][a-z0-9-]*)"`).FindAllStringSubmatch(block[1], -1)
	out := make([]string, 0, len(names))
	for _, m := range names {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// dispatchedMethods extracts the method names runMethod routes. Cases may be
// grouped (`case "a", "b":`), so each case line is split on commas.
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

// refuseBeforeDispatch is the set methodSafety refuses WITHOUT runMethod ever
// being reached; they are legitimately catalogued without a dispatch case.
var refuseBeforeDispatch = []string{"factory-reset", "update"}

// TestCatalogHasNoPhantomMethods asserts every schema method is either
// dispatched or refuse-before-dispatch — no method is advertised that the plugin
// cannot act on.
func TestCatalogHasNoPhantomMethods(t *testing.T) {
	reachable := map[string]bool{}
	for _, m := range dispatchedMethods(t) {
		reachable[m] = true
	}
	for _, m := range refuseBeforeDispatch {
		reachable[m] = true
	}
	for _, m := range schemaMethods(t) {
		if !reachable[m] {
			t.Errorf("schema method %q is a PHANTOM: not dispatched by runMethod and not refused by methodSafety — remove it from the schema or implement it", m)
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
