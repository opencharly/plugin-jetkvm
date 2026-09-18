package kvmclient

// keycombo.go holds the shared key/combo CONSTANTS and the resolved-type, plus
// the USB HID usage ids the resolvers in keymap.go build on. The named-combo
// REGISTRY that once lived here (a 15-entry table of alt+tab/ctrl+c/…) was
// replaced by keymap.go, which resolves any key name or chord over the full HID
// Keyboard/Keypad usage table — the schema always documented arbitrary keys
// (Return, Escape, F5, Control_L), but the registry only accepted 15.

import (
	"strings"
	"unicode"
)

// MaxKeyComboNameRunes bounds caller-controlled combo names before
// normalization allocates a lower-cased, tokenized copy. It applies to each
// entry of a key sequence as well as to a standalone combo.
const MaxKeyComboNameRunes = 64

// USB boot-keyboard modifier bits used by the named combo registry.
const (
	ModifierLeftControl = 1 << iota
	ModifierLeftShift
	ModifierLeftAlt
	ModifierLeftMeta
	ModifierRightControl
	ModifierRightShift
	ModifierRightAlt
	ModifierRightMeta
)

// USB HID keyboard usage codes used by the named combo registry.
const (
	KeyUsageC      = 0x06
	KeyUsageE      = 0x08
	KeyUsageM      = 0x10
	KeyUsageR      = 0x15
	KeyUsageT      = 0x17
	KeyUsageV      = 0x19
	KeyUsageZ      = 0x1d
	KeyUsageEnter  = 0x28
	KeyUsageEscape = 0x29
	KeyUsageTab    = 0x2b
	KeyUsageSpace  = 0x2c
	KeyUsageDelete = 0x4c
)

// ResolvedKeyCombo is one named chord after it has crossed the shared
// resolver and integer validation boundary. Keys is owned by the caller.
type ResolvedKeyCombo struct {
	Modifier byte
	Keys     []byte
}

// normalizeKeyComboName is retained for callers that want the canonical
// separator form.
func normalizeKeyComboName(name string) string {
	parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '+' || r == '-' || unicode.IsSpace(r)
	})
	return strings.Join(parts, "+")
}
