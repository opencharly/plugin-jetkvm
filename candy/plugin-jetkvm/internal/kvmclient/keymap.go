package kvmclient

// keymap.go is the general keyboard key/combo resolver.
//
// The vendored client's keycombo.go exposed only a small named-combo registry
// (alt+tab, ctrl+c, …), so `jetkvm: {method: key, key: "F5"}` or
// `{method: key-combo, combo: "Control_L+Alt_L+Delete"}` — both documented by
// the plugin's schema — FAILED with "unknown key combo". This file replaces
// that registry with a real resolver over the USB HID Keyboard/Keypad usage
// table, so any single key or chord works: letters, digits, function keys,
// arrows, navigation keys, punctuation, and the modifier keys.
//
// The wire contract is unchanged: a modifier byte plus up to six key usages,
// exactly what sendKeyboardReport consumes.

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// modifierBits maps a modifier NAME (normalized: lower-case, no separators) to
// its bit in the HID keyboard report's modifier byte.
var modifierBits = map[string]byte{
	"ctrl":         ModifierLeftControl,
	"control":      ModifierLeftControl,
	"controll":     ModifierLeftControl,
	"leftctrl":     ModifierLeftControl,
	"leftcontrol":  ModifierLeftControl,
	"lctrl":        ModifierLeftControl,
	"shift":        ModifierLeftShift,
	"shiftl":       ModifierLeftShift,
	"leftshift":    ModifierLeftShift,
	"lshift":       ModifierLeftShift,
	"alt":          ModifierLeftAlt,
	"altl":         ModifierLeftAlt,
	"leftalt":      ModifierLeftAlt,
	"lalt":         ModifierLeftAlt,
	"meta":         ModifierLeftMeta,
	"super":        ModifierLeftMeta,
	"win":          ModifierLeftMeta,
	"windows":      ModifierLeftMeta,
	"cmd":          ModifierLeftMeta,
	"command":      ModifierLeftMeta,
	"gui":          ModifierLeftMeta,
	"controlr":     ModifierRightControl,
	"rightctrl":    ModifierRightControl,
	"rightcontrol": ModifierRightControl,
	"rctrl":        ModifierRightControl,
	"shiftr":       ModifierRightShift,
	"rightshift":   ModifierRightShift,
	"rshift":       ModifierRightShift,
	"altr":         ModifierRightAlt,
	"altgr":        ModifierRightAlt,
	"rightalt":     ModifierRightAlt,
	"ralt":         ModifierRightAlt,
	"metar":        ModifierRightMeta,
	"rightmeta":    ModifierRightMeta,
	"rmeta":        ModifierRightMeta,
	"rightwin":     ModifierRightMeta,
	"rightcmd":     ModifierRightMeta,
}

// keyUsages maps a key NAME (normalized: lower-case, no separators) to its USB
// HID Keyboard/Keypad usage id. Modifier keys are handled via modifierBits, not
// here. Names cover the common spellings a caller might use.
var keyUsages = map[string]byte{
	"enter": 0x28, "return": 0x28, "ret": 0x28,
	"esc": 0x29, "escape": 0x29,
	"backspace": 0x2a, "bksp": 0x2a,
	"tab":   0x2b,
	"space": 0x2c, "spacebar": 0x2c,
	"capslock": 0x39, "caps": 0x39,
	"printscreen": 0x46, "prtsc": 0x46,
	"scrolllock": 0x47,
	"pause":      0x48, "break": 0x48,
	"insert": 0x49, "ins": 0x49,
	"home":   0x4a,
	"pageup": 0x4b, "pgup": 0x4b, "prior": 0x4b,
	"delete": 0x4c, "del": 0x4c,
	"end":      0x4d,
	"pagedown": 0x4e, "pgdn": 0x4e, "next": 0x4e,
	"rightarrow": 0x4f, "right": 0x4f, "arrowright": 0x4f,
	"leftarrow": 0x50, "left": 0x50, "arrowleft": 0x50,
	"downarrow": 0x51, "down": 0x51, "arrowdown": 0x51,
	"uparrow": 0x52, "up": 0x52, "arrowup": 0x52,
	"menu": 0x65, "applications": 0x65,
	"minus": 0x2d, "equal": 0x2e, "equals": 0x2e,
	"leftbracket": 0x2f, "rightbracket": 0x30,
	"backslash": 0x31, "semicolon": 0x33, "quote": 0x34,
	"grave": 0x35, "backtick": 0x35,
	"comma": 0x36, "period": 0x37, "slash": 0x38,
}

func init() {
	// F1..F12 = 0x3a..0x45
	for i := 1; i <= 12; i++ {
		keyUsages[fmt.Sprintf("f%d", i)] = byte(0x3a + i - 1)
	}
	// a..z = 0x04..0x1d
	for r := 'a'; r <= 'z'; r++ {
		keyUsages[string(r)] = byte(0x04 + (r - 'a'))
	}
	// 1..9 = 0x1e..0x26 ; 0 = 0x27
	for d := byte(1); d <= 9; d++ {
		keyUsages[fmt.Sprintf("%d", d)] = byte(0x1e + d - 1)
	}
	keyUsages["0"] = 0x27
}

// shiftedKeys are the printable characters whose HID usage requires Shift, with
// their unshifted base usage id.
var shiftedKeys = map[rune]byte{
	'!': 0x1e, '@': 0x1f, '#': 0x20, '$': 0x21, '%': 0x22, '^': 0x23,
	'&': 0x24, '*': 0x25, '(': 0x26, ')': 0x27,
	'_': 0x2d, '+': 0x2e, '{': 0x2f, '}': 0x30, '|': 0x31,
	':': 0x33, '"': 0x34, '~': 0x35, '<': 0x36, '>': 0x37, '?': 0x38,
}

// normalizeKeyToken lower-cases and strips separators from one combo token.
func normalizeKeyToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// resolveToken resolves one key/combo token to either a modifier bit or a key
// usage (with an optional shift for a shifted printable character). Exactly one
// of the returns is meaningful: mod != 0 for a modifier, key != 0 for a key.
func resolveToken(tok string, shift *byte) (mod, key byte, err error) {
	// A single printable character that needs Shift (e.g. "!" or an uppercase
	// letter) maps to its base usage plus the shift modifier.
	if r := []rune(tok); len(r) == 1 {
		c := r[0]
		if base, ok := shiftedKeys[c]; ok {
			*shift |= ModifierLeftShift
			return 0, base, nil
		}
		if c >= 'A' && c <= 'Z' {
			*shift |= ModifierLeftShift
			return 0, byte(0x04 + (c - 'A')), nil
		}
	}
	n := normalizeKeyToken(tok)
	if n == "" {
		return 0, 0, fmt.Errorf("empty key token")
	}
	if m, ok := modifierBits[n]; ok {
		return m, 0, nil
	}
	if k, ok := keyUsages[n]; ok {
		return 0, k, nil
	}
	return 0, 0, fmt.Errorf("unknown key %q", tok)
}

// ResolveKeyCombo resolves a named key or chord to one validated HID keyboard
// report. Separators between tokens are `+`, `-`, or whitespace. Examples:
//
//	"F5"                       -> key 0x3e
//	"Return" / "Enter"         -> key 0x28
//	"Control_L+Alt_L+Delete"   -> modifier ctrl|alt, key 0x4c
//	"ctrl+shift+t"             -> modifier ctrl|shift, key 0x14
//	"A" / "a"                  -> key 0x04 (shifted for the uppercase form)
//
// Modifier tokens compose the report's modifier byte; all non-modifier tokens
// form the key array (at most six, per the USB boot-keyboard report). The
// returned key slice is owned by the caller.
func ResolveKeyCombo(name string) (modifier byte, keys []byte, err error) {
	if len([]rune(name)) > MaxKeyComboNameRunes {
		return 0, nil, fmt.Errorf("key combo name must contain at most %d runes", MaxKeyComboNameRunes)
	}
	raw := strings.FieldsFunc(name, func(r rune) bool {
		return r == '+' || r == '-' || unicode.IsSpace(r)
	})
	if len(raw) == 0 {
		return 0, nil, fmt.Errorf("empty key combo")
	}
	var mod, shift byte
	var usages []int
	for _, tok := range raw {
		m, k, err := resolveToken(tok, &shift)
		if err != nil {
			return 0, nil, err
		}
		if m != 0 {
			mod |= m
			continue
		}
		usages = append(usages, int(k))
	}
	mod |= shift
	if err := ValidateKeyCombo(int(mod), usages); err != nil {
		return 0, nil, fmt.Errorf("invalid key combo %q: %w", name, err)
	}
	keys = make([]byte, len(usages))
	for i, u := range usages {
		keys[i] = byte(u)
	}
	return mod, keys, nil
}

// ResolveKeySequence resolves and validates a complete ordered sequence before
// returning any reports. Errors identify the failing array index without
// reflecting the caller-controlled chord name.
func ResolveKeySequence(names []string) ([]ResolvedKeyCombo, error) {
	if err := ValidateKeySequenceLength(len(names)); err != nil {
		return nil, err
	}
	resolved := make([]ResolvedKeyCombo, len(names))
	for i, name := range names {
		modifier, keys, err := ResolveKeyCombo(name)
		if err != nil {
			return nil, fmt.Errorf("combos[%d]: %w", i, err)
		}
		integerKeys := make([]int, len(keys))
		for keyIndex, key := range keys {
			integerKeys[keyIndex] = int(key)
		}
		if err := ValidateKeyCombo(int(modifier), integerKeys); err != nil {
			return nil, fmt.Errorf("combos[%d]: invalid resolved key combo: %w", i, err)
		}
		resolved[i] = ResolvedKeyCombo{Modifier: modifier, Keys: keys}
	}
	return resolved, nil
}

// validKeyComboNames is retained for error messages and tests: the sorted list
// of representative key names the resolver recognizes.
func validKeyComboNames() []string {
	names := make([]string, 0, len(keyUsages))
	for name := range keyUsages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
