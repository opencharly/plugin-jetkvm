package jetkvm

// install.go wires the jetkvm transport into the SHARED, transport-agnostic
// console-wizard engine (sdk/kit's ConsoleWizard) and implements the `install`
// method: connect ONCE and drive an ordered recipe, OCR-gating each screen.
//
// WHY ONE CONNECTION, NOT A CHAIN OF `check:` STEPS (RCA, measured): the verb's
// `dispatch` opens a fresh WebRTC session per step and tears it down after. On
// firmware 0.5.9 a session costs 0.8–1.6 s to establish and the device's own log
// shows a fresh `/webrtc/signaling/client` handshake (1.5–6.7 s) per screenshot;
// a ~15-screen wizard would pay ~15 handshakes of pure overhead. The driver opens
// ONE session and runs the whole recipe on it.
//
// The MECHANISM (wait-for-screen, send input, recipe/answer model) is shared with
// other transports (see sdk/kit ConsoleWizard); this file supplies only the
// jetkvm-specific transport.

import (
	"context"
	"fmt"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/spec"
)

// jetkvmTransport adapts a connected kvmclient.Client to kit.ConsoleTransport.
type jetkvmTransport struct {
	cl     *kvmclient.Client
	op     *spec.Op
	holdMS int
}

func (t jetkvmTransport) Capture(ctx context.Context) ([]byte, error) {
	shot, err := t.cl.CaptureScreenshot(ctx)
	if err != nil {
		return nil, err
	}
	return shot.PNG, nil
}

func (t jetkvmTransport) PressKey(ctx context.Context, name string) error {
	return t.press(ctx, "key", name)
}

func (t jetkvmTransport) PressCombo(ctx context.Context, combo string) error {
	return t.press(ctx, "key-combo", combo)
}

func (t jetkvmTransport) press(ctx context.Context, method, value string) error {
	in := params.JetkvmInput{Method: params.JetkvmMethod(method), AllowControl: true, HoldMs: t.holdMS}
	if method == "key" {
		in.KeyName = value
	} else {
		in.Combo = value
	}
	_, err := runMethod(ctx, t.cl, t.op, &in)
	return err
}

func (t jetkvmTransport) Type(ctx context.Context, text string) error {
	_, err := runMethod(ctx, t.cl, t.op, &params.JetkvmInput{Method: "type", Text: text, AllowControl: true})
	return err
}

// runInstall drives the recipe on an already-connected client through the shared
// console-wizard engine.
func runInstall(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	if !hasSteps(in) {
		return "", fmt.Errorf("jetkvm: install requires a steps recipe (author `steps:` inline, or reference a `kind: jetkvm` entity recipe with `device:`/`recipe:`)")
	}
	steps := make([]kit.ConsoleStep, 0, len(in.Steps))
	for _, s := range in.Steps {
		steps = append(steps, kit.ConsoleStep{
			WaitFor:     s.WaitFor,
			Action:      s.Action,
			Key:         s.KeyName,
			Combo:       s.Combo,
			Text:        s.Text,
			TimeoutSec:  s.TimeoutSec,
			Optional:    s.Optional,
			Artifact:    s.Artifact,
			Description: s.Description,
		})
	}
	w := &kit.ConsoleWizard{
		Steps:     steps,
		Answers:   in.Answers,
		Transport: jetkvmTransport{cl: cl, op: op},
	}
	return w.Run(ctx)
}
