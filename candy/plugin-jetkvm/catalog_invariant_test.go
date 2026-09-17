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

// jetkvmMethodBlock extracts the #JetkvmMethod disjunction from the schema.
//
// It deliberately does NOT terminate on a blank line: the enum is grouped by
// comment headings and may legitimately gain blank lines between groups, and a
// `\n\n` terminator would silently truncate the parse at the first one —
// exempting the whole tail of the catalog while the test still passed. The block
// runs instead to the FIRST top-level (column-0) declaration that follows it,
// which is the next `#Def:` or the end of file.
func jetkvmMethodBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("schema/jetkvm.cue")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "#JetkvmMethod:")
	if start < 0 {
		t.Fatal("could not locate #JetkvmMethod in schema/jetkvm.cue")
	}
	rest := src[start:]
	// Advance past the definition's own opening line, then stop at the next
	// column-0 declaration (a line starting with `#` or a bare identifier
	// followed by `:`).
	lines := strings.Split(rest, "\n")
	var block []string
	for i, line := range lines {
		if i == 0 {
			block = append(block, line)
			continue
		}
		if line != "" && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			break // the next top-level declaration
		}
		block = append(block, line)
	}
	return strings.Join(block, "\n")
}

// schemaMethods returns the sorted method names in #JetkvmMethod.
func schemaMethods(t *testing.T) []string {
	t.Helper()
	block := jetkvmMethodBlock(t)
	names := regexp.MustCompile(`"([a-z][a-z0-9-]*)"`).FindAllStringSubmatch(block, -1)
	out := make([]string, 0, len(names))
	for _, m := range names {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
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

// TestSchemaMethodParseCoversTheWholeEnum guards the PARSER itself: the enum's
// last entry must be parsed, so a future blank line or terminator change cannot
// silently truncate schemaMethods and exempt the catalog's tail. `rpc` is the
// final method in the disjunction, and `status` the first.
func TestSchemaMethodParseCoversTheWholeEnum(t *testing.T) {
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
		t.Errorf("parser missed the enum HEAD (status) — got %d methods: %v", len(got), got)
	}
	if !has("rpc") {
		t.Errorf("parser missed the enum TAIL (rpc) — the parse is truncating; got %d methods: %v", len(got), got)
	}
	if len(got) < 60 {
		t.Errorf("parsed only %d methods — the #JetkvmMethod parse looks truncated", len(got))
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
