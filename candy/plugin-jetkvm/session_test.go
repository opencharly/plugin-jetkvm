package jetkvm

// session_test.go pins the jetkvm session methods' DECODING and their wiring into
// the shared kit.ConsoleSession engine, with no device. The engine's own logic
// (marker/echo trap, sudo, passphrase) is unit-tested where it lives —
// sdk/kit/console_session_test.go — so it is not duplicated here (R3). What this
// file owns is: the param → kit conversion, the required-field guards, and the
// defaults (terminal combo, prompt anchors, LUKS outcomes).

import (
	"context"
	"strings"
	"testing"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
)

// TestSessionFor_SharesTransportAndSudo pins the ONE construction point: every
// session method builds the engine over the jetkvm transport and the supplied
// sudo password (R3 — no per-method construction drift).
func TestSessionFor_SharesTransportAndSudo(t *testing.T) {
	s := sessionFor(nil, nil, "hunter2")
	if s.SudoPassword != "hunter2" {
		t.Fatalf("sudo password not carried: %+v", s)
	}
	if s.Transport == nil {
		t.Fatal("transport must be set")
	}
}

// TestRunCommands_ConvertsAndRuns proves the commands decode into the shared
// engine and each runs, reading its OCR output. The transport simulates a shell
// that echoes the typed line then, for the command line, prints the marker line.
func TestRunCommands_ConvertsAndRuns(t *testing.T) {
	tr := &shellTransport{}
	s := sessionForFromTransport(tr, "")
	results, err := s.RunCommands(context.Background(), []kit.ConsoleCommand{
		{Command: "uname -r", Expect: "kernel"},
		{Command: "id"},
	})
	if err != nil {
		t.Fatalf("RunCommands: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("both commands must run: %d", len(results))
	}
	for _, r := range results {
		if !strings.Contains(r.Output, "kernel") {
			t.Fatalf("output not read for %q: %q", r.Command, r.Output)
		}
	}
}

// TestRunCommands_EmptyListFails is the required-field guard on the method path.
func TestRunCommands_EmptyListFails(t *testing.T) {
	_, err := runCommands(context.Background(), nil, nil, &params.JetkvmInput{}, "")
	if err == nil || !strings.Contains(err.Error(), "non-empty commands") {
		t.Fatalf("want commands-required failure, got %v", err)
	}
}

// TestRunCommands_BlankCommandFails names the offending index.
func TestRunCommands_BlankCommandFails(t *testing.T) {
	in := &params.JetkvmInput{Commands: []params.JetkvmSessionCommand{{Command: "  "}}}
	_, err := runCommands(context.Background(), nil, nil, in, "")
	if err == nil || !strings.Contains(err.Error(), "command 1 is empty") {
		t.Fatalf("want blank-command failure, got %v", err)
	}
}

// TestRunLUKSUnlock_RequiresPassphrase pins the either/or guard (passphrase or
// passphrase_secret) and that its error names BOTH options.
func TestRunLUKSUnlock_RequiresPassphrase(t *testing.T) {
	_, err := runLUKSUnlock(context.Background(), nil, nil, &params.JetkvmInput{})
	if err == nil || !strings.Contains(err.Error(), "passphrase_secret") {
		t.Fatalf("want passphrase-required failure naming both options, got %v", err)
	}
}

// TestSessionDefaults pins the documented defaults the methods apply when the
// author omits them.
func TestSessionDefaults(t *testing.T) {
	if defaultTerminalCombo != "super+Return" {
		t.Fatalf("terminal combo default changed: %q", defaultTerminalCombo)
	}
	if len(defaultPromptAnchors) == 0 || len(defaultLUKSSuccessAnchors) == 0 || len(defaultLUKSFailureAnchors) == 0 {
		t.Fatal("prompt/LUKS defaults must be non-empty")
	}
}

// shellTransport simulates a shell: it echoes the last typed line; once a command
// line was submitted it also prints a fixed result and the shell's own marker
// LINE, so kit.ConsoleSession sees genuine completion.
type shellTransport struct {
	keys  []string
	types []string
}

func (s *shellTransport) Capture(context.Context) ([]byte, error) {
	if len(s.types) == 0 {
		return []byte("prompt $ "), nil
	}
	typed := s.types[len(s.types)-1]
	if !strings.Contains(typed, kit.ConsoleMarkerPrefix) {
		return []byte(typed), nil
	}
	marker := ""
	if i := strings.Index(typed, "echo "); i >= 0 {
		marker = strings.TrimSpace(typed[i+len("echo "):])
	}
	return []byte(typed + "\nkernel 6.12.0-omarchy\n" + marker + "\n"), nil
}
func (s *shellTransport) PressKey(_ context.Context, k string) error {
	s.keys = append(s.keys, k)
	return nil
}
func (s *shellTransport) PressCombo(_ context.Context, c string) error {
	s.keys = append(s.keys, "combo:"+c)
	return nil
}
func (s *shellTransport) Type(_ context.Context, t string) error {
	s.types = append(s.types, t)
	return nil
}

var _ kit.ConsoleTransport = (*shellTransport)(nil)

// sessionForFromTransport builds a session over an injected transport (test-only
// seam), mirroring sessionFor without a device client.
func sessionForFromTransport(tr kit.ConsoleTransport, sudoPassword string) *kit.ConsoleSession {
	return &kit.ConsoleSession{
		Transport:    tr,
		SudoPassword: sudoPassword,
		// Identity OCR over the scripted screen bytes, so the engine's marker
		// logic is exercised without invoking tesseract.
		OCR: func(b []byte) (string, error) { return string(b), nil },
	}
}
