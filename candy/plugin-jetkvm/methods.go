package jetkvm

// methods.go is the jetkvm method dispatcher: the safety classification, the
// required-modifier table, and the single dispatch entry the provider calls.
//
// THE DEVICE-SAFETY GATE. A JetKVM is a physical appliance wired into a real
// machine's keyboard, video and power: unlike a container it is NOT disposable,
// and a stray keystroke or power action has real consequences. So every method
// is classified read-only or mutating, and a mutating method REFUSES to act
// unless the author wrote `allow_control: true`. The refusal is a documented
// `skip`, not a failure, so a plan can carry the method and the gate reports
// itself honestly instead of the step silently doing nothing.

import (
	"context"
	"fmt"
	"strings"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk"
	"github.com/opencharly/spec/spec"
)

// readOnlyMethods is the explicit ALLOWLIST of methods that may run without
// allow_control. Written as an allowlist, not a deny-list, so a method added
// later is mutating-by-default and cannot act on the device until it is
// deliberately classified — the fail-safe direction for a physical appliance.
var readOnlyMethods = map[string]bool{
	"status": true, "screenshot": true, "version": true,
	"diagnostics": true, "video-state": true, "usb-state": true,
	"atx-state": true, "dc-state": true,
	"virtual-media-state": true, "storage-files": true, "wol-devices": true,
	"macros": true, "keyboard-layout": true, "timezones": true,
	"cloud-state": true, "network-state": true, "network-settings": true,
	"tailscale-status": true, "update-status": true, "devmode-state": true,
	"ssh-key": true, "tls-state": true, "extensions": true, "public-ip": true,
	"check-media-url": true,
	"usb-config":      true,
}

// NeverAutonomousMethods lists the methods that refuse even WITH allow_control:
// an unattended plan must never wipe or irreversibly reimage a physical device.
// The operator runs these by hand.
//
// Exported for the catalog invariant test, which must read the REAL refusal set
// rather than a second copy of it (R3): the test needs to know which catalogued
// methods are legitimately undispatched, and a hardcoded copy would keep passing
// if this set shrank while the schema kept advertising the method.
func NeverAutonomousMethods() map[string]bool {
	out := make(map[string]bool, len(neverAutonomous))
	for k, v := range neverAutonomous {
		out[k] = v
	}
	return out
}

var neverAutonomous = map[string]bool{
	"factory-reset": true,
	"update":        true,
}

// rpcForbiddenMethods are device JSON-RPC method names the raw `rpc` escape
// hatch MUST NOT reach, whatever allow_control says. `rpc` is deliberately
// MUTATING (it is not in readOnlyMethods): it can invoke any device method, so
// gating it is the whole point — otherwise it is a documented bypass of the
// device-safety gate. These names are the irreversible/reimaging ones, refused
// for the same reason their typed counterparts are.
var rpcForbiddenMethods = map[string]bool{
	"factoryReset":        true,
	"tryUpdate":           true,
	"tryUpdateComponents": true,
}

// methodSafety reports whether a method may act, and why not when it may not.
// skip true means the caller must report a documented skip and take NO device
// action.
func methodSafety(method string, allowControl bool) (skip bool, reason string) {
	switch {
	case readOnlyMethods[method]:
		return false, ""
	case neverAutonomous[method]:
		return true, fmt.Sprintf(
			"jetkvm: %s is refused: it irreversibly reimages or wipes a physical device and is never available to an unattended plan (run it by hand on the device)", method)
	case !allowControl:
		return true, fmt.Sprintf(
			"jetkvm: %s is a mutating method on a physical appliance; author `allow_control: true` to permit it (read-only methods need no gate)", method)
	}
	return false, ""
}

// requiredModifiers is the per-method required-field contract. The fields live
// in the desugared plugin INPUT map, which the shared sdk.OpModifierZero reads
// map-first.
var requiredModifiers = map[string][]string{
	"key":           {"key"},
	"type":          {"text"},
	"key-combo":     {"combo"},
	"click":         {"x", "y"},
	"mouse":         {"x", "y"},
	"move":          {"x", "y"},
	"drag":          {"from_x", "from_y", "x", "y"},
	"scroll":        {"scroll_y"},
	"power":         {"action"},
	"dc-power":      {"action"},
	"wol":           {"value"},
	"virtual-media": {"action"},
	"screenshot":    {"artifact"},
	"rpc":           {"rpc_method"},
}

// skipError marks a policy-gated skip (distinct from a device failure). The
// provider converts it to the documented `skip` verdict.
type skipError struct{ reason string }

func (e *skipError) Error() string { return e.reason }

// isSkip reports whether an error is a policy skip rather than a failure.
func isSkip(err error) (*skipError, bool) {
	se, ok := err.(*skipError)
	return se, ok
}

// trimErrMsg keeps an error message single-line for the verdict wire.
func trimErrMsg(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}

// dispatch runs one jetkvm method and returns its captured output. A returned
// *skipError means the policy gate declined (reported as a skip, not a failure).
func dispatch(ctx context.Context, op *spec.Op, in *params.JetkvmInput, host, password, authToken string) (string, error) {
	if err := sdk.CheckRequiredModifiers(string(in.Method), op, requiredModifiers, sdk.OpModifierZero); err != nil {
		return "", err
	}
	if skip, reason := methodSafety(string(in.Method), in.AllowControl); skip {
		return "", &skipError{reason: reason}
	}

	cl, err := kvmclient.Connect(ctx, kvmclient.Options{
		BaseURL: kvmclient.NormalizeDeviceURL(host),
		Credentials: kvmclient.Credentials{
			Password:  kvmclient.NewSecret(password),
			AuthToken: kvmclient.NewSecret(authToken),
		},
		AllowControl: in.AllowControl,
		InsecureTLS:  in.Insecure,
	})
	if err != nil {
		return "", err
	}
	defer cl.Close(context.Background()) //nolint:errcheck // best-effort teardown

	return runMethod(ctx, cl, op, in)
}
