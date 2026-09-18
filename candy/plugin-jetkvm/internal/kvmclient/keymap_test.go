package kvmclient

import "testing"

// TestResolveKeyComboGeneralKeys pins the general key resolver: arbitrary named
// keys and chords that the old 15-entry registry rejected must resolve.
func TestResolveKeyComboGeneralKeys(t *testing.T) {
	cases := []struct {
		name string
		mod  byte
		keys []byte
	}{
		{"F5", 0, []byte{0x3e}},
		{"Return", 0, []byte{0x28}},
		{"Enter", 0, []byte{0x28}},
		{"Escape", 0, []byte{0x29}},
		{"Up", 0, []byte{0x52}},
		{"Down", 0, []byte{0x51}},
		{"PageUp", 0, []byte{0x4b}},
		{"Delete", 0, []byte{0x4c}},
		{"a", 0, []byte{0x04}},
		{"A", ModifierLeftShift, []byte{0x04}},
		{"1", 0, []byte{0x1e}},
		{"Control_L+Alt_L+Delete", ModifierLeftControl | ModifierLeftAlt, []byte{0x4c}},
		{"ctrl+shift+t", ModifierLeftControl | ModifierLeftShift, []byte{0x17}},
		{"ctrl+c", ModifierLeftControl, []byte{0x06}},
		{"alt+tab", ModifierLeftAlt, []byte{0x2b}},
		{"cmd+space", ModifierLeftMeta, []byte{0x2c}},
		{"!", ModifierLeftShift, []byte{0x1e}},
	}
	for _, c := range cases {
		mod, keys, err := ResolveKeyCombo(c.name)
		if err != nil {
			t.Errorf("ResolveKeyCombo(%q) errored: %v", c.name, err)
			continue
		}
		if mod != c.mod {
			t.Errorf("ResolveKeyCombo(%q) mod=0x%02x want 0x%02x", c.name, mod, c.mod)
		}
		if len(keys) != len(c.keys) {
			t.Errorf("ResolveKeyCombo(%q) keys=%v want %v", c.name, keys, c.keys)
			continue
		}
		for i := range keys {
			if keys[i] != c.keys[i] {
				t.Errorf("ResolveKeyCombo(%q) keys=%v want %v", c.name, keys, c.keys)
			}
		}
	}
}

// TestResolveKeyComboRejectsUnknown pins that a genuinely unknown key still
// fails (the generalization must not accept anything).
func TestResolveKeyComboRejectsUnknown(t *testing.T) {
	if _, _, err := ResolveKeyCombo("NotAKey"); err == nil {
		t.Fatal("an unknown key name must be rejected")
	}
}

// TestResolveKeyComboRegressionOldRegistry confirms every name the retired
// registry accepted still resolves to the same key, so no existing plan breaks.
func TestResolveKeyComboRegressionOldRegistry(t *testing.T) {
	for _, name := range []string{"alt+tab", "cmd", "cmd+space", "ctrl+alt+del",
		"ctrl+c", "ctrl+shift+t", "ctrl+v", "ctrl+z", "e", "enter", "esc",
		"m", "r", "t", "win"} {
		if _, _, err := ResolveKeyCombo(name); err != nil {
			t.Errorf("previously-valid combo %q no longer resolves: %v", name, err)
		}
	}
}
