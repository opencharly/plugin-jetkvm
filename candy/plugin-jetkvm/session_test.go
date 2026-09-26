package jetkvm

// session_test.go pins this plugin's DECODE of the terminal-session / flow /
// boot-order params into the SDK's shared console actions. The actions' own
// logic (marker/echo trap, sudo, passphrase, flow routing) is unit-tested where
// it lives — sdk/kit/console_actions.go + console_session.go + console_flow.go —
// so it is not duplicated here (R3). What this file owns: the param → neutral
// conversion, the required-field guards, and the dispatch wiring.

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
)

// TestSessionCommands_Decodes pins the PURE param→neutral mapping (the decode is
// this plugin's unit; the engine's behavior lives in sdk/kit).
func TestSessionCommands_Decodes(t *testing.T) {
	in := &params.JetkvmInput{Commands: []params.JetkvmSessionCommand{
		{Command: "id", Sudo: true, Expect: "uid", TimeoutSec: 30, Artifact: "/tmp/a"},
		{Command: "uname -r"},
	}}
	got, err := sessionCommands(in)
	if err != nil {
		t.Fatalf("sessionCommands: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 commands, got %d", len(got))
	}
	if !got[0].Sudo || got[0].Expect != "uid" || got[0].TimeoutSec != 30 || got[0].Artifact != "/tmp/a" {
		t.Fatalf("first command not decoded: %+v", got[0])
	}
	if got[1].Command != "uname -r" || got[1].Sudo {
		t.Fatalf("second command not decoded: %+v", got[1])
	}
}

// TestSessionCommands_BlankCommandFails names the offending index before any
// device call — the required-field guard on the decode path.
func TestSessionCommands_BlankCommandFails(t *testing.T) {
	in := &params.JetkvmInput{Commands: []params.JetkvmSessionCommand{{Command: "  "}}}
	_, err := sessionCommands(in)
	if err == nil || !strings.Contains(err.Error(), "command 1 is empty") {
		t.Fatalf("want blank-command failure, got %v", err)
	}
}

// TestRunLUKSUnlock_RequiresPassphrase pins the either/or guard (passphrase or
// passphrase_secret) and that its error names the secret option.
func TestRunLUKSUnlock_RequiresPassphrase(t *testing.T) {
	_, err := runLUKSUnlock(context.Background(), nil, nil, &params.JetkvmInput{})
	if err == nil || !strings.Contains(err.Error(), "passphrase_secret") {
		t.Fatalf("want passphrase-required failure naming the secret option, got %v", err)
	}
}

// TestSessionTerminalOpen_Decodes pins the open-terminal param→neutral mapping:
// the default timeout, the authored combo/anchors/artifact, and the
// first-command timeout override.
func TestSessionTerminalOpen_Decodes(t *testing.T) {
	// Default timeout (60) with no commands.
	got := sessionTerminalOpen(&params.JetkvmInput{TerminalCombo: "ctrl+alt+F3", PromptAnchors: []string{"$"}, Artifact: "/tmp/a"})
	if got.Combo != "ctrl+alt+F3" || len(got.PromptAnchors) != 1 || got.Artifact != "/tmp/a" || got.TimeoutSec != 60 {
		t.Fatalf("open-terminal decode wrong: %+v", got)
	}
	// The first command's timeout_sec overrides the default.
	got = sessionTerminalOpen(&params.JetkvmInput{Commands: []params.JetkvmSessionCommand{{TimeoutSec: 120}}})
	if got.TimeoutSec != 120 {
		t.Fatalf("first-command timeout not honored: %+v", got)
	}
}

// TestSessionBootOrder_Decodes pins the boot-order param→neutral mapping,
// including the schema-enum type conversion and the sudo password pass-through.
func TestSessionBootOrder_Decodes(t *testing.T) {
	got := sessionBootOrder(&params.JetkvmInput{
		BootOrderAction: "set", BootOrderSequence: "0003,0001,0002", BootOrderCommand: "/usr/sbin/efibootmgr",
	}, "hunter2")
	if got.Action != "set" || got.Sequence != "0003,0001,0002" || got.Binary != "/usr/sbin/efibootmgr" || got.SudoPassword != "hunter2" {
		t.Fatalf("boot-order decode wrong: %+v", got)
	}
}

// TestRunBootOrder_Guards pins the required-field guards (checked before any
// terminal session, so a nil transport is safe here).
func TestRunBootOrder_Guards(t *testing.T) {
	cases := []struct {
		name string
		in   *params.JetkvmInput
		want string
	}{
		{"no action", &params.JetkvmInput{}, "requires an action"},
		{"next no entry", &params.JetkvmInput{BootOrderAction: "next"}, "requires an entry"},
		{"set no sequence", &params.JetkvmInput{BootOrderAction: "set"}, "requires a sequence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := kit.RunBootOrder(context.Background(), nil, sessionBootOrder(tc.in, ""))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// TestRunFlow_Guards pins the flow decode's required-field guards.
func TestRunFlow_Guards(t *testing.T) {
	if _, err := runFlow(context.Background(), nil, nil, &params.JetkvmInput{}); err == nil || !strings.Contains(err.Error(), "flow_start") {
		t.Fatalf("want flow_start guard, got %v", err)
	}
	if _, err := runFlow(context.Background(), nil, nil, &params.JetkvmInput{FlowStart: "a"}); err == nil || !strings.Contains(err.Error(), "flow_nodes") {
		t.Fatalf("want flow_nodes guard, got %v", err)
	}
}
