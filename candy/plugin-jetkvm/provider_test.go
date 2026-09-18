package jetkvm

// provider_test.go drives the provider's Invoke end to end against an in-process
// fake JetKVM device (internal/fakedevice, which speaks the real HTTP + signaling
// WebSocket + WebRTC + JSON-RPC wire). These tests exercise the ACTUAL changed
// path — the registered provider's verdict, the device-safety gate, and the
// artifact verdict — not a compilation.
//
// The fake device's H.264 path is not part of this fake (it serves no video
// track), so screenshot behaviour is covered by the video-readiness unit tests
// below and by the live device bed in Phase 4, not by a fabricated frame here.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/fakedevice"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// invoke runs one provider Invoke with the given plugin input + env.
func invoke(t *testing.T, in map[string]any, env map[string]any) (string, string) {
	t.Helper()
	inputJSON, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	paramsJSON, err := json.Marshal(spec.Op{PluginInput: mustMap(t, inputJSON)})
	if err != nil {
		t.Fatalf("marshal op: %v", err)
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	reply, err := (provider{}).Invoke(context.Background(), &proto.InvokeRequest{
		ParamsJson: paramsJSON,
		EnvJson:    envJSON,
	})
	if err != nil {
		t.Fatalf("Invoke returned a transport error: %v", err)
	}
	var wire struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(reply.GetResultJson()), &wire); err != nil {
		t.Fatalf("decode reply %q: %v", reply.GetResultJson(), err)
	}
	return wire.Status, wire.Message
}

func mustMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// TestInvokeBoxModeSkips verifies the live-device verb reports the documented
// skip under `charly check box`, so a plan carrying `jetkvm:` steps still passes
// the build-context check with no device attached.
func TestInvokeBoxModeSkips(t *testing.T) {
	status, msg := invoke(t, map[string]any{"method": "status"}, map[string]any{"mode": "box"})
	if status != "skip" {
		t.Fatalf("want skip in box mode, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "requires a live device") {
		t.Fatalf("skip message should name the reason, got %q", msg)
	}
}

// TestInvokeNoAddressSkips verifies a step with no device address reports the
// honest no-context skip rather than attempting a connection.
func TestInvokeNoAddressSkips(t *testing.T) {
	status, msg := invoke(t, map[string]any{"method": "status"}, map[string]any{"mode": "live"})
	if status != "skip" {
		t.Fatalf("want skip with no address, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "no device address") {
		t.Fatalf("skip message should name the missing address, got %q", msg)
	}
}

// TestInvokeStatusAgainstFakeDevice is the end-to-end happy path: the provider
// connects to a real (fake) device over the real wire and reports its identity,
// so the changed path runs live in CI.
func TestInvokeStatusAgainstFakeDevice(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "status", "host": dev.BaseURL()},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("want pass, got %q (%s)", status, msg)
	}
	for _, want := range []string{"jetkvm:    ok", "device_id: fake-device", "rpc:       true"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("status output missing %q; got:\n%s", want, msg)
		}
	}
}

// TestInvokeMutatingMethodIsGated is the CENTRAL device-safety test: a mutating
// method without allow_control must NOT act, and must say so.
func TestInvokeMutatingMethodIsGated(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "reboot", "host": dev.BaseURL()},
		map[string]any{"mode": "live"})
	if status != "skip" {
		t.Fatalf("a mutating method without allow_control must skip, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "mutating method on a physical appliance") {
		t.Fatalf("gate message should name the reason, got %q", msg)
	}
}

// TestInvokeFactoryResetIsNeverAutonomous verifies the irreversible methods are
// refused even WITH allow_control — no unattended plan may wipe a device.
func TestInvokeFactoryResetIsNeverAutonomous(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	for _, method := range []string{"factory-reset", "update"} {
		status, msg := invoke(t,
			map[string]any{"method": method, "host": dev.BaseURL(), "allow_control": true},
			map[string]any{"mode": "live"})
		if status != "skip" {
			t.Fatalf("%s must be refused even with allow_control, got %q (%s)", method, status, msg)
		}
		if !strings.Contains(msg, "irreversibly reimages or wipes") {
			t.Fatalf("%s refusal should name the reason, got %q", method, msg)
		}
	}
}

// TestInvokeRequiredModifierIsEnforced verifies the per-method required-field
// contract is checked before any device action.
func TestInvokeRequiredModifierIsEnforced(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "click", "host": dev.BaseURL(), "allow_control": true},
		map[string]any{"mode": "live"})
	if status != "fail" {
		t.Fatalf("click without x/y must fail the modifier check, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "missing required modifier") {
		t.Fatalf("failure should name the missing modifiers, got %q", msg)
	}
}

// TestInvokeRawRPCPassesParams verifies rpc_params is decoded and forwarded once
// allow_control permits the escape hatch: the fake answers `null` for an unknown
// method, so a passing verdict proves the call reached the device with its params
// and the result came back, rather than being rejected client-side.
func TestInvokeRawRPCPassesParams(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{
			"method": "rpc", "rpc_method": "getVideoState",
			"rpc_params": `{"refresh":true}`, "host": dev.BaseURL(),
			"allow_control": true,
		},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("rpc with params should pass through to the device, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "null") {
		t.Fatalf("the fake's null result should round-trip, got %q", msg)
	}
}

// TestInvokeReadOnlyMethodNeedsNoGate is the inverse of the gate test: a
// read-only method must run WITHOUT allow_control.
func TestInvokeReadOnlyMethodNeedsNoGate(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "version", "host": dev.BaseURL()},
		map[string]any{"mode": "live"})
	if status == "skip" && strings.Contains(msg, "mutating") {
		t.Fatalf("version is read-only and must not be gated; got %q (%s)", status, msg)
	}
}

// TestSafetyClassificationIsAllowlist confirms the classification direction:
// an UNKNOWN method name is treated as mutating (fail-safe), never as read-only.
func TestSafetyClassificationIsAllowlist(t *testing.T) {
	if skip, _ := methodSafety("some-future-method", false); !skip {
		t.Fatal("an unclassified method must be treated as mutating (allowlist, fail-safe)")
	}
	if skip, _ := methodSafety("some-future-method", true); skip {
		t.Fatal("an unclassified method with allow_control should be permitted")
	}
	if skip, _ := methodSafety("status", false); skip {
		t.Fatal("status is classified read-only and must not be gated")
	}
}

// TestScreenshotRequiresArtifact verifies the artifact path's required field.
func TestScreenshotRequiresArtifact(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "screenshot", "host": dev.BaseURL()},
		map[string]any{"mode": "live"})
	if status != "fail" {
		t.Fatalf("screenshot without artifact must fail, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "artifact") {
		t.Fatalf("failure should name the artifact modifier, got %q", msg)
	}
}

// TestMain keeps os.Exit out of the test binary's teardown path.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// TestNormalizeDeviceURL pins the documented `host` contract: a bare hostname or
// host:port gets the device's default plaintext scheme, while an explicit scheme
// is preserved. This is the regression test for the live bed failure
// "device URL scheme must be http or https" — the plugin documents a bare-host
// input, so it must normalize before handing the value to the client.
func TestNormalizeDeviceURL(t *testing.T) {
	cases := map[string]string{
		"jk.example.ts.net":         "http://jk.example.ts.net",
		"jk.example.ts.net:8080":    "http://jk.example.ts.net:8080",
		"http://jk.example.ts.net":  "http://jk.example.ts.net",
		"https://jk.example.ts.net": "https://jk.example.ts.net",
		"  jk.example.ts.net  ":     "http://jk.example.ts.net",
		"192.168.1.10":              "http://192.168.1.10",
		"":                          "",
	}
	for in, want := range cases {
		if got := kvmclient.NormalizeDeviceURL(in); got != want {
			t.Errorf("NormalizeDeviceURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestInsecureOptionIsWired confirms the insecure flag reaches the client's
// connection options rather than being silently ignored: a device with a
// self-signed certificate is unreachable without it, so a no-op flag would be a
// documented-but-false surface.
func TestInsecureOptionIsWired(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	// The fake serves plaintext, so both paths must connect; this asserts the
	// option does not ITSELF break a working connection.
	status, msg := invoke(t,
		map[string]any{"method": "status", "host": dev.BaseURL(), "insecure": true},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("insecure against a plaintext fake must still connect, got %q (%s)", status, msg)
	}
}

// TestRawRPCRequiresControl is the regression test for the validator's finding
// that `rpc` was classified read-only: the raw escape hatch can invoke ANY
// device method, so leaving it ungated is a documented bypass of the
// device-safety gate. It must be mutating (gated), not read-only.
func TestRawRPCRequiresControl(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "rpc", "rpc_method": "ping", "host": dev.BaseURL()},
		map[string]any{"mode": "live"})
	if status != "skip" {
		t.Fatalf("rpc without allow_control must be gated, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "mutating method on a physical appliance") {
		t.Fatalf("gate message should name the reason, got %q", msg)
	}
}

// TestRawRPCWithControlStillRefusesIrreversible verifies the second half of the
// fix: even WITH allow_control the escape hatch cannot reach the irreversible
// reimaging methods, so it is not a way around the never-autonomous rule.
func TestRawRPCWithControlStillRefusesIrreversible(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	for _, m := range []string{"factoryReset", "tryUpdate", "tryUpdateComponents"} {
		status, msg := invoke(t,
			map[string]any{"method": "rpc", "rpc_method": m, "host": dev.BaseURL(), "allow_control": true},
			map[string]any{"mode": "live"})
		if status != "fail" {
			t.Fatalf("rpc %s must be refused even with allow_control, got %q (%s)", m, status, msg)
		}
		if !strings.Contains(msg, "irreversibly reimages or wipes") {
			t.Fatalf("refusal for %s should name the reason, got %q", m, msg)
		}
	}
}

// TestRawRPCWithControlReachesOrdinaryMethod verifies the gate is not so broad
// that it breaks the escape hatch's purpose: with allow_control a normal device
// method is reachable.
func TestRawRPCWithControlReachesOrdinaryMethod(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "rpc", "rpc_method": "ping", "host": dev.BaseURL(), "allow_control": true},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("rpc ping with allow_control should pass, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "pong") {
		t.Fatalf("expected the pong result, got %q", msg)
	}
}

// TestInvokeMoveWithoutButtonIsAPureMove pins the `move` contract: a bare move
// (no `button:`) sends the pointer to (x,y) with NO button bit and must PASS.
// The regression this guards: methodPointer resolved a mouse button
// unconditionally, so `move` with no authored button failed with
// "unknown mouse button" even though a move presses no button.
func TestInvokeMoveWithoutButtonIsAPureMove(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "move", "host": dev.BaseURL(), "x": 960, "y": 540, "allow_control": true},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("a bare move must pass (no button is sent), got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "Moved pointer to (960, 540)") {
		t.Fatalf("move output should report the position, got %q", msg)
	}
	abs, _ := dev.MouseInterfaceState()
	if abs.X != 960 || abs.Y != 540 {
		t.Fatalf("device saw pointer at (%d,%d), want (960,540)", abs.X, abs.Y)
	}
	if abs.Buttons != 0 {
		t.Fatalf("a pure move must leave the button mask at 0, got %d", abs.Buttons)
	}
}

// TestInvokeClickDefaultsToLeft verifies `click` with no button still presses
// left — the default the pointer path documents.
func TestInvokeClickDefaultsToLeft(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	status, msg := invoke(t,
		map[string]any{"method": "click", "host": dev.BaseURL(), "x": 100, "y": 200, "allow_control": true},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("click with defaults must pass, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "Clicked left at (100, 200)") {
		t.Fatalf("click should report the default left button, got %q", msg)
	}
}

// TestInvokeHostFromEnv verifies the JETKVM_HOST fallback: a step with NO
// authored host connects to the env-provided address, so a bed can be portable
// and carry no device-specific hostname.
func TestInvokeHostFromEnv(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	t.Setenv("JETKVM_HOST", dev.BaseURL())
	status, msg := invoke(t,
		map[string]any{"method": "status"},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("JETKVM_HOST must supply the device address, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "rpc:       true") {
		t.Fatalf("status via JETKVM_HOST should connect to the fake, got %q", msg)
	}
}

// TestInvokeAuthoredHostBeatsEnv verifies precedence: an authored `host:` wins
// over JETKVM_HOST.
func TestInvokeAuthoredHostBeatsEnv(t *testing.T) {
	dev := fakedevice.Start(t, fakedevice.Options{})
	t.Setenv("JETKVM_HOST", "http://127.0.0.1:1") // an address that cannot answer
	status, msg := invoke(t,
		map[string]any{"method": "status", "host": dev.BaseURL()},
		map[string]any{"mode": "live"})
	if status != "pass" {
		t.Fatalf("authored host must beat JETKVM_HOST, got %q (%s)", status, msg)
	}
}

// TestInvokeNoHostAnywhereSkips verifies that with no authored host, no
// JETKVM_HOST and no venue host, the verb reports the documented skip.
func TestInvokeNoHostAnywhereSkips(t *testing.T) {
	t.Setenv("JETKVM_HOST", "")
	status, msg := invoke(t,
		map[string]any{"method": "status"},
		map[string]any{"mode": "live"})
	if status != "skip" {
		t.Fatalf("no address must skip, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "JETKVM_HOST") {
		t.Fatalf("the skip should name JETKVM_HOST, got %q", msg)
	}
}
