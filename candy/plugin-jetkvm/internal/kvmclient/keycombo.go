package kvmclient

// keycombo.go holds the shared modifier BIT constants and the resolved-combo
// type. The named-combo REGISTRY that once lived here (a 15-entry table of
// alt+tab/ctrl+c/…) was replaced by keymap.go, which resolves key names and
// chords over a USB HID Keyboard/Keypad usage table — the schema always
// documented arbitrary keys (Return, Escape, F5, Control_L), but the registry
// only accepted 15. Only the modifier bits remain here (used by the report
// builder); the resolver and its key table live in keymap.go.

// USB boot-keyboard modifier bits used by the keyboard report.
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

// MaxKeyComboNameRunes bounds caller-controlled key/combo names before the
// resolver normalizes them.
const MaxKeyComboNameRunes = 64

// ResolvedKeyCombo is one named chord after it has crossed the shared
// resolver and integer validation boundary. Keys is owned by the caller.
type ResolvedKeyCombo struct {
	Modifier byte
	Keys     []byte
}
