package jetkvm

// session.go implements the terminal-session methods:
//
//   - open-terminal  — open a terminal on the controlled machine (a GUI terminal
//     via a hotkey, or a bare text TTY via a VT switch) and OCR-wait for the
//     shell prompt.
//   - run-command    — run one or more commands in the open terminal, reading
//     each result by OCR; supports sudo (entering the password at the prompt).
//   - close-terminal — exit the open terminal.
//   - luks-unlock    — type a LUKS/disk-encryption passphrase at an initramfs
//     prompt and wait for a named outcome (the boot proceeding, an error, a
//     login).
//
// The DRIVING MECHANISM is the SDK's shared kit.ConsoleSession (R3): the same
// engine the SPICE transport will use, so these methods hold only jetkvm's
// transport adapter and the input decoding — never a second drive loop.
//
// WHY A MARKER, NOT A PROMPT (RCA): a shell reuses one terminal across commands,
// and a prompt string is neither screen-unique nor stable. Worse, the terminal
// ECHOES the typed command, so a naive "wait for the command text" or "wait for
// the marker substring" test would pass before the command ran. kit.ConsoleSession
// appends `; echo <opaque marker>` and waits for a LINE equal to that marker, which
// only the shell's own output produces — a real completion condition, never a sleep.

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// defaultTerminalCombo is the hotkey `open-terminal` sends when none is authored:
// Omarchy's terminal hotkey (Super+Return). A bare text TTY is requested with
// `terminal_combo: "ctrl+alt+F3"`.
const defaultTerminalCombo = "super+Return"

// defaultPromptAnchors are the substrings that mean a shell prompt is ready. The
// common prompt endings; an unusual prompt is overridden with `prompt_anchors:`.
var defaultPromptAnchors = []string{"$", "#", ">"}

// defaultLUKSSuccessAnchors are the substrings that mean the passphrase was
// ACCEPTED and boot proceeded (a login prompt, the Omarchy welcome, or the
// initramfs's own success line). There is no shell at the initramfs prompt, so
// completion is a screen condition, not an echoed marker.
var defaultLUKSSuccessAnchors = []string{"login:", "Welcome", "succeeded", "Booting"}

// defaultLUKSFailureAnchors are the substrings that mean the passphrase was
// REJECTED (cryptsetup's own message). Matching one FAILS the step rather than
// reading as an outcome.
var defaultLUKSFailureAnchors = []string{"No key available", "wrong password", "Failed to activate"}

// defaultBootManager is the EFI boot-manager binary `boot-order` drives. It is
// the standard Linux `efibootmgr`; a system naming it differently overrides
// `boot_order_command`.
const defaultBootManager = "efibootmgr"

// runBootOrder sets the UEFI boot order from INSIDE the running system, using the
// EFI boot manager (`efibootmgr`) in an open terminal — the OS-side counterpart
// to the firmware boot-menu key. It runs as root (sudo), since efibootmgr writes
// the firmware NVRAM.
//
//	list — `efibootmgr` (the entries + BootOrder), read by OCR.
//	next — `efibootmgr --bootnext <entry>` (one-time next boot).
//	set  — `efibootmgr --bootorder <sequence>` (persistent order).
func runBootOrder(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput, sudoPassword string) (string, error) {
	action := strings.TrimSpace(string(in.BootOrderAction))
	if action == "" {
		return "", fmt.Errorf("jetkvm: boot-order requires boot_order_action (list | next | set)")
	}
	bin := strings.TrimSpace(in.BootOrderCommand)
	if bin == "" {
		bin = defaultBootManager
	}
	var cmd, expect string
	needSudo := false
	switch action {
	case "list":
		// Reading the boot entries needs no privilege.
		cmd = bin
		expect = "Boot"
	case "next":
		if strings.TrimSpace(in.BootOrderEntry) == "" {
			return "", fmt.Errorf("jetkvm: boot-order action next requires boot_order_entry (the entry number, e.g. 0003)")
		}
		cmd = fmt.Sprintf("%s --bootnext %s", bin, strings.TrimSpace(in.BootOrderEntry))
		expect = "Boot"
		needSudo = true
	case "set":
		if strings.TrimSpace(in.BootOrderSequence) == "" {
			return "", fmt.Errorf("jetkvm: boot-order action set requires boot_order_sequence (e.g. 0003,0001,0002)")
		}
		cmd = fmt.Sprintf("%s --bootorder %s", bin, strings.TrimSpace(in.BootOrderSequence))
		expect = "Boot"
		needSudo = true
	default:
		return "", fmt.Errorf("jetkvm: boot-order action %q is not list | next | set", action)
	}
	// Writing the firmware NVRAM (next/set) needs root; reading (list) does not.
	// A sudo write with no password supplied fails with a clear message naming
	// the requirement rather than hanging at a prompt.
	runSudo := needSudo
	if runSudo && sudoPassword == "" {
		return "", fmt.Errorf("jetkvm: boot-order %s writes the firmware NVRAM and needs sudo; set sudo_password: or sudo_password_secret: (run it as root instead if the shell is already root)", action)
	}
	s := sessionFor(cl, op, sudoPassword)
	res, err := s.RunCommand(ctx, kit.ConsoleCommand{Command: cmd, Sudo: runSudo, Expect: expect})
	if err != nil {
		return res.Output, fmt.Errorf("jetkvm: boot-order %s: %w", action, err)
	}
	return fmt.Sprintf("boot-order %s ok:\n%s", action, res.Output), nil
}

// runFlow drives a BOUNDED console state machine (the shared kit.ConsoleFlow):
// continuous OCR-until-condition with named outcomes and if/then/else +
// case/switch + bounded-while routing. It converts the schema-generated flow
// into the neutral engine types (the SDK holds no wire type, SDD).
func runFlow(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	if strings.TrimSpace(in.FlowStart) == "" {
		return "", fmt.Errorf("jetkvm: flow requires flow_start (the entry node id)")
	}
	if len(in.FlowNodes) == 0 {
		return "", fmt.Errorf("jetkvm: flow requires a non-empty flow_nodes map")
	}
	nodes := make(map[string]kit.ConsoleFlowNode, len(in.FlowNodes))
	for id, n := range in.FlowNodes {
		waits := make([]kit.ConsoleFlowOutcome, 0, len(n.Wait))
		for _, o := range n.Wait {
			waits = append(waits, kit.ConsoleFlowOutcome{Name: o.Name, Match: o.Match, Failure: o.Failure})
		}
		nodes[id] = kit.ConsoleFlowNode{
			ID:          id,
			Description: n.Description,
			Wait:        waits,
			Action: kit.ConsoleFlowAction{
				Key: n.Key, Combo: n.Combo, Text: n.Text,
				Command: n.Command, Sudo: n.Sudo, Expect: n.Expect, CloseTerminal: n.CloseTerminal,
			},
			Transitions: n.Transitions,
			Next:        n.Next,
			Artifact:    n.Artifact,
			TimeoutSec:  n.TimeoutSec,
		}
	}
	f := &kit.ConsoleFlow{
		Start:        in.FlowStart,
		Nodes:        nodes,
		Transport:    jetkvmTransport{cl: cl, op: op},
		SudoPassword: in.SudoPassword,
		MaxSteps:     in.FlowMaxSteps,
		MaxLoops:     in.FlowMaxLoops,
		Logger:       func(format string, args ...any) { fmt.Printf("jetkvm: flow: "+format+"\n", args...) },
	}
	res, err := f.Run(ctx)
	if err != nil {
		return renderFlowEvidence(res), fmt.Errorf("jetkvm: flow: %w", err)
	}
	return fmt.Sprintf("flow completed at node %q after %d step(s):\n%s", res.Final, len(res.Steps), renderFlowEvidence(res)), nil
}

// renderFlowEvidence renders a flow run's per-node evidence, including the
// OCR-read output of any command action so "read the results via OCR" is visible.
func renderFlowEvidence(res kit.ConsoleFlowResult) string {
	var b strings.Builder
	b.WriteString(res.LogText)
	for _, s := range res.Steps {
		if s.CommandOutput != "" {
			fmt.Fprintf(&b, "  [%s] command output:\n%s\n", s.Node, s.CommandOutput)
		}
	}
	return b.String()
}

// sessionFor builds the shared session engine over a connected client. One
// construction point, so open/run/close/luks all share the OCR default and the
// sudo password (R3).
func sessionFor(cl *kvmclient.Client, op *spec.Op, sudoPassword string) *kit.ConsoleSession {
	return &kit.ConsoleSession{
		Transport:    jetkvmTransport{cl: cl, op: op},
		SudoPassword: sudoPassword,
		Logger:       func(format string, args ...any) { fmt.Printf("jetkvm: "+format+"\n", args...) },
	}
}

// runOpenTerminal opens a terminal and waits for the shell prompt.
func runOpenTerminal(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput, sudoPassword string) (string, error) {
	combo := strings.TrimSpace(in.TerminalCombo)
	if combo == "" {
		combo = defaultTerminalCombo
	}
	anchors := in.PromptAnchors
	if len(anchors) == 0 {
		anchors = defaultPromptAnchors
	}
	s := sessionFor(cl, op, sudoPassword)
	if err := s.Transport.PressCombo(ctx, combo); err != nil {
		return "", fmt.Errorf("jetkvm: open-terminal: sending %q: %w", combo, err)
	}
	timeout := 60
	if len(in.Commands) > 0 && in.Commands[0].TimeoutSec > 0 {
		timeout = in.Commands[0].TimeoutSec
	}
	got, ok, err := s.WaitForAny(ctx, anchors, timeout, in.Artifact)
	if err != nil {
		return "", fmt.Errorf("jetkvm: open-terminal: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("jetkvm: open-terminal: no shell prompt (%v) appeared after %q (read %q)",
			anchors, combo, kit.ConsolePreview(got, 200))
	}
	return fmt.Sprintf("Opened a terminal with %q and reached a shell prompt", combo), nil
}

// runCommands runs the authored commands in the open terminal, reading each
// result by OCR and optionally closing the terminal afterwards.
func runCommands(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput, sudoPassword string) (string, error) {
	if len(in.Commands) == 0 {
		return "", fmt.Errorf("jetkvm: run-command requires a non-empty commands list")
	}
	s := sessionFor(cl, op, sudoPassword)
	cmds := make([]kit.ConsoleCommand, 0, len(in.Commands))
	for i, c := range in.Commands {
		if strings.TrimSpace(c.Command) == "" {
			return "", fmt.Errorf("jetkvm: run-command: command %d is empty", i+1)
		}
		cmds = append(cmds, kit.ConsoleCommand{
			Command:     c.Command,
			Sudo:        c.Sudo,
			Expect:      c.Expect,
			TimeoutSec:  c.TimeoutSec,
			Artifact:    c.Artifact,
			Description: c.Description,
		})
	}
	results, runErr := s.RunCommands(ctx, cmds)
	var b strings.Builder
	for _, r := range results {
		label := "command"
		if r.Sudo {
			label = "sudo command"
		}
		fmt.Fprintf(&b, "%s %q ->\n%s\n", label, r.Command, r.Output)
	}
	if in.CloseTerminal {
		if err := s.Transport.Type(ctx, "exit"); err != nil {
			return b.String(), fmt.Errorf("jetkvm: run-command: closing terminal: %w", err)
		}
		if err := s.Transport.PressKey(ctx, "Return"); err != nil {
			return b.String(), fmt.Errorf("jetkvm: run-command: submitting exit: %w", err)
		}
	}
	if runErr != nil {
		return b.String(), fmt.Errorf("jetkvm: run-command: %w", runErr)
	}
	return b.String(), nil
}

// runCloseTerminal exits the open terminal.
func runCloseTerminal(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	s := sessionFor(cl, op, "")
	if err := s.Transport.Type(ctx, "exit"); err != nil {
		return "", fmt.Errorf("jetkvm: close-terminal: typing exit: %w", err)
	}
	if err := s.Transport.PressKey(ctx, "Return"); err != nil {
		return "", fmt.Errorf("jetkvm: close-terminal: submitting exit: %w", err)
	}
	return "Sent `exit` to the open terminal", nil
}

// runLUKSUnlock types the disk passphrase at the initramfs prompt and waits for a
// named outcome. The passphrase comes from `passphrase` or `passphrase_secret`
// (resolved into in.Passphrase by the provider).
func runLUKSUnlock(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	if strings.TrimSpace(in.Passphrase) == "" {
		return "", fmt.Errorf("jetkvm: luks-unlock requires a passphrase (author `passphrase:`, or set `passphrase_secret:` to a credential-store key; the operator sets the matching env/secret)")
	}
	success := in.Outcomes
	if len(success) == 0 {
		success = defaultLUKSSuccessAnchors
	}
	timeout := 120
	s := sessionFor(cl, op, "")
	got, ok, err := s.EnterPassphrase(ctx, in.Passphrase, success, defaultLUKSFailureAnchors, timeout, in.Artifact)
	if err != nil {
		return "", fmt.Errorf("jetkvm: luks-unlock: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("jetkvm: luks-unlock: no success anchor (%v) appeared after entering the passphrase (read %q)",
			success, kit.ConsolePreview(got, 200))
	}
	return fmt.Sprintf("Unlocked the disk with the LUKS passphrase. Read: %s", kit.ConsolePreview(got, 200)), nil
}
