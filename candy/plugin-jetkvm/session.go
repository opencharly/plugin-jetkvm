package jetkvm

// session.go implements the terminal-session / flow / boot-order methods by
// DECODING this plugin's authored params into the SDK's shared, transport-neutral
// console actions (sdk/kit console_actions.go), which the SPICE transport calls
// too (R3). This file owns ONLY the decode — never a second drive loop.
//
//   - open-terminal  — open a terminal on the controlled machine (a GUI terminal
//     via a hotkey, or a bare text TTY via a VT switch) and OCR-wait for the
//     shell prompt.
//   - run-command    — run one or more commands in the open terminal, reading
//     each result by OCR; supports sudo (entering the password at the prompt).
//   - close-terminal — exit the open terminal.
//   - luks-unlock    — type a LUKS/disk-encryption passphrase at an initramfs
//     prompt and wait for a named outcome.
//   - flow           — a bounded console state machine (continuous OCR-until-
//     condition + if/then/else + case/switch + bounded while).
//   - boot-order     — set the UEFI boot order from inside the OS via efibootmgr.
//
// WHY A MARKER, NOT A PROMPT (RCA): a shell reuses one terminal across commands,
// and the terminal ECHOES the typed line — so a naive "wait for the command text"
// or "wait for the marker substring" test would pass before the command ran.
// kit.ConsoleSession appends `; echo <opaque marker>` and waits for a LINE equal
// to it, which only the shell's own output produces (the echo trap, unit-pinned).

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// sessionTransport adapts a connected client for the shared console actions.
func sessionTransport(cl *kvmclient.Client, op *spec.Op) kit.ConsoleTransport {
	return jetkvmTransport{cl: cl, op: op}
}

// runOpenTerminal decodes `open-terminal` into the shared action.
func runOpenTerminal(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	timeout := 60
	if len(in.Commands) > 0 && in.Commands[0].TimeoutSec > 0 {
		timeout = in.Commands[0].TimeoutSec
	}
	return kit.OpenTerminal(ctx, sessionTransport(cl, op), kit.TerminalOpen{
		Combo:         in.TerminalCombo,
		PromptAnchors: in.PromptAnchors,
		TimeoutSec:    timeout,
		Artifact:      in.Artifact,
	})
}

// sessionCommands decodes the authored `commands:` into the neutral form the
// shared engine drives. It is PURE, so the param→neutral mapping is unit-locked
// without a device (the engine's own behavior is tested in sdk/kit).
func sessionCommands(in *params.JetkvmInput) ([]kit.ConsoleCommand, error) {
	cmds := make([]kit.ConsoleCommand, 0, len(in.Commands))
	for i, c := range in.Commands {
		if strings.TrimSpace(c.Command) == "" {
			return nil, fmt.Errorf("jetkvm: run-command: command %d is empty", i+1)
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
	return cmds, nil
}

// runCommands decodes `run-command` into the shared action.
func runCommands(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput, sudoPassword string) (string, error) {
	cmds, err := sessionCommands(in)
	if err != nil {
		return "", err
	}
	return kit.RunCommands(ctx, sessionTransport(cl, op), cmds, sudoPassword, in.CloseTerminal, in.PromptAnchors)
}

// runCloseTerminal decodes `close-terminal` into the shared action.
func runCloseTerminal(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	return kit.CloseTerminal(ctx, sessionTransport(cl, op))
}

// runLUKSUnlock decodes `luks-unlock` into the shared action. The passphrase was
// resolved from `passphrase`/`passphrase_secret` by the provider.
func runLUKSUnlock(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	return kit.LUKSUnlock(ctx, sessionTransport(cl, op), in.Passphrase, in.Outcomes, nil, 120, in.Artifact)
}

// runBootOrder decodes `boot-order` into the shared action.
func runBootOrder(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput, sudoPassword string) (string, error) {
	return kit.RunBootOrder(ctx, sessionTransport(cl, op), kit.BootOrder{
		Action:       string(in.BootOrderAction),
		Entry:        in.BootOrderEntry,
		Sequence:     in.BootOrderSequence,
		Binary:       in.BootOrderCommand,
		SudoPassword: sudoPassword,
	})
}

// runFlow decodes `flow` into the shared, bounded state machine.
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
			waits = append(waits, kit.ConsoleFlowOutcome{
				Name: o.Name, Match: o.Match, Reference: o.Reference,
				MaxDistance: o.MaxDistance, Failure: o.Failure,
			})
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
	res, err := kit.RunConsoleFlow(ctx, sessionTransport(cl, op), kit.ConsoleFlowSpec{
		Start:            in.FlowStart,
		Nodes:            nodes,
		SudoPassword:     in.SudoPassword,
		MaxSteps:         in.FlowMaxSteps,
		MaxLoops:         in.FlowMaxLoops,
		ResumeFromScreen: in.FlowResume,
		ResumeOrder:      in.FlowResumeOrder,
		PromptAnchors:    in.PromptAnchors,
	})
	if err != nil {
		return kit.RenderFlowEvidence(res), fmt.Errorf("jetkvm: flow: %w", err)
	}
	return fmt.Sprintf("flow completed at node %q after %d step(s):\n%s", res.Final, len(res.Steps), kit.RenderFlowEvidence(res)), nil
}
