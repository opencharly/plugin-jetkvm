package jetkvm

// catalog.go is the method catalog: one thin, typed wrapper per authored jetkvm
// method over the single Call primitive on the vendored client. Every method
// here is administration or inspection of the device's own state; the input and
// power surfaces additionally go through the vendored typed HID lease so the
// release guarantees are the vendored client's, not a second implementation.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/spec/spec"
)

// holdDefaultMS is the default press duration for key/key-combo when hold_ms is
// unset — long enough for the target OS to register the keystroke.
const holdDefaultMS = 40

// runMethod routes one method to its implementation.
func runMethod(ctx context.Context, cl *kvmclient.Client, op *spec.Op, in *params.JetkvmInput) (string, error) {
	switch in.Method {
	// --- observation ------------------------------------------------------
	case "status":
		return methodStatus(ctx, cl)
	case "screenshot":
		return methodScreenshot(ctx, cl, in, op)
	case "version":
		return callSummary(ctx, cl, "getLocalVersion", nil)
	case "video-state":
		return callSummary(ctx, cl, "getVideoState", nil)
	case "usb-state":
		return callSummary(ctx, cl, "getUSBState", nil)
	case "atx-state":
		return callSummary(ctx, cl, "getATXState", nil)
	case "dc-state":
		return callSummary(ctx, cl, "getDCPowerState", nil)
	case "virtual-media-state":
		return callSummary(ctx, cl, "getVirtualMediaState", nil)
	case "storage-files":
		return callSummary(ctx, cl, "listStorageFiles", nil)
	case "wol-devices":
		return callSummary(ctx, cl, "getWakeOnLanDevices", nil)
	case "macros":
		return callSummary(ctx, cl, "getKeyboardMacros", nil)
	case "keyboard-layout":
		return callSummary(ctx, cl, "getKeyboardLayout", nil)
	case "timezones":
		return callSummary(ctx, cl, "getTimezones", nil)
	case "cloud-state":
		return callSummary(ctx, cl, "getCloudState", nil)
	case "network-state":
		return callSummary(ctx, cl, "getNetworkState", nil)
	case "network-settings":
		return callSummary(ctx, cl, "getNetworkSettings", nil)
	case "tailscale-status":
		return callSummary(ctx, cl, "getTailscaleStatus", nil)
	case "update-status":
		return callSummary(ctx, cl, "getUpdateStatus", nil)
	case "devmode-state":
		return callSummary(ctx, cl, "getDevModeState", nil)
	case "ssh-key":
		return callSummary(ctx, cl, "getSSHKeyState", nil)
	case "tls-state":
		return callSummary(ctx, cl, "getTLSState", nil)
	case "extensions":
		return methodExtensions(ctx, cl)
	case "public-ip":
		return methodPublicIP(ctx, cl)
	case "diagnostics":
		return methodDiagnostics(ctx, cl)
	case "check-media-url":
		return methodCheckMediaURL(ctx, cl, in)

	// --- input (mutating, gated) -----------------------------------------
	case "key", "key-combo", "macro":
		return methodKeys(ctx, cl, in)
	case "type":
		return methodType(ctx, cl, in)
	case "click", "mouse", "move":
		return methodPointer(ctx, cl, in)
	case "scroll":
		return methodScroll(ctx, cl, in)
	case "drag":
		return methodDrag(ctx, cl, in)

	// --- power (mutating, gated) -----------------------------------------
	case "power":
		return methodPower(ctx, cl, in)
	case "dc-power":
		return methodDCPower(ctx, cl, in)
	case "reboot":
		return methodReboot(ctx, cl)
	case "wol":
		return methodWOL(ctx, cl, in)

	// --- media / usb (mutating, gated) ------------------------------------
	case "virtual-media":
		return methodVirtualMedia(ctx, cl, in)
	case "usb-device":
		return methodUSBDevice(ctx, cl, in)
	case "usb-emulation":
		return methodUSBEmulation(ctx, cl, in)
	case "usb-config":
		return callSummary(ctx, cl, "getUsbConfig", nil)

	// --- device config (mutating, gated) ----------------------------------
	case "set-settings":
		return methodSetSettings(ctx, cl, in)
	case "set-edid":
		return methodSetScalar(ctx, cl, "setEDID", "edid", in.Value)
	case "set-video":
		return methodSetVideo(ctx, cl, in)
	case "set-display":
		return methodSetDisplay(ctx, cl, in)
	case "set-audio":
		return methodSetAudio(ctx, cl, in)
	case "set-network":
		return methodSetNetwork(ctx, cl, in)
	case "set-tailscale":
		return methodSetScalar(ctx, cl, "setTailscaleControlURL", "controlURL", in.Value)
	case "set-devmode":
		return methodSetBool(ctx, cl, "setDevModeState", "enabled", in.Value)
	case "set-ssh-key":
		return methodSetScalar(ctx, cl, "setSSHKeyState", "sshKey", in.Value)
	case "set-tls":
		return methodSetJSON(ctx, cl, "setTLSState", in.Settings)
	case "set-keyboard-layout":
		return methodSetScalar(ctx, cl, "setKeyboardLayout", "layout", in.Value)
	case "set-macros":
		return methodSetJSON(ctx, cl, "setKeyboardMacros", in.Settings)
	case "set-jiggler":
		return methodSetBool(ctx, cl, "setJigglerState", "enabled", in.Value)
	case "set-extension":
		return methodSetScalar(ctx, cl, "setActiveExtension", "extensionId", in.Value)
	case "set-wol-devices":
		return methodSetJSON(ctx, cl, "setWakeOnLanDevices", in.Settings)
	case "set-log-level":
		return methodSetScalar(ctx, cl, "setDefaultLogLevel", "level", in.Value)
	case "renew-dhcp":
		return callSummary(ctx, cl, "renewDHCPLease", nil)

	// --- raw escape hatch -------------------------------------------------
	case "rpc":
		return methodRawRPC(ctx, cl, in)
	}
	return "", fmt.Errorf("jetkvm: unknown method %q", in.Method)
}

// callSummary invokes a parameterless RPC method and renders its JSON result as
// the captured output. The device's JSON-RPC methods return heterogeneous
// shapes, so the result is decoded generically and marshalled back.
func callSummary(ctx context.Context, cl *kvmclient.Client, method string, p map[string]any) (string, error) {
	var out any
	if err := cl.Call(ctx, method, p, &out); err != nil {
		return "", err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("jetkvm: encoding %s result: %w", method, err)
	}
	return string(b), nil
}

// methodStatus reports the device identity and firmware, and probes that the
// control channel answers — the canonical "is this device reachable and well"
// probe, mirroring the vnc/cdp `status` convention.
func methodStatus(ctx context.Context, cl *kvmclient.Client) (string, error) {
	st, err := cl.Status(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintln(&b, "jetkvm:    ok")
	fmt.Fprintf(&b, "device_id: %s\n", st.DeviceID)
	fmt.Fprintf(&b, "firmware:  %s\n", st.FirmwareVersion)
	fmt.Fprintf(&b, "rpc:       %v\n", st.RPCReachable)
	return b.String(), nil
}

// methodScreenshot captures one fresh H.264 keyframe as PNG and hands it to the
// shared artifact-landing entry point. It is the ONE artifact-producing method,
// so the provider passes artifact=true to the verdict pipeline for it.
//
// NOTE — do NOT add a getVideoState pre-flight gate here. Measured against
// firmware 0.5.9: `getVideoState.ready` describes whether the device's native
// capture PIPELINE is currently running, and that pipeline is started per
// WebRTC session and stopped when the last one disconnects
// (`host_display_disable_when_idle`). So it reads `ready:false, error:no_signal`
// whenever no session is active — including on a device with a perfectly good
// HDMI signal that screenshots successfully. Gating on it rejects every real
// capture. CaptureScreenshot is the authority: it opens the session (which
// starts the pipeline) and waits for a frame, with its own diagnostics.
func methodScreenshot(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput, _ *spec.Op) (string, error) {
	shot, err := cl.CaptureScreenshot(ctx)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(in.Artifact, shot.PNG, 0o644); err != nil {
		return "", fmt.Errorf("jetkvm: screenshot: writing %s: %w", in.Artifact, err)
	}
	return fmt.Sprintf("Screenshot saved to %s (%dx%d)", in.Artifact, shot.Width, shot.Height), nil
}

// methodExtensions lists the device's supported extension ids.
func methodExtensions(ctx context.Context, cl *kvmclient.Client) (string, error) {
	active, err := callSummary(ctx, cl, "getActiveExtension", nil)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintln(&b, strings.TrimSpace(active))
	fmt.Fprintln(&b, "supported: atx-power, dc-power")
	return b.String(), nil
}

// methodPublicIP reports the device's public addresses.
func methodPublicIP(ctx context.Context, cl *kvmclient.Client) (string, error) {
	return callSummary(ctx, cl, "getPublicIPAddresses", map[string]any{"refresh": true})
}

// methodDiagnostics collects the same facts the device exposes for a support
// bundle: identity, video, USB, network and extension state.
func methodDiagnostics(ctx context.Context, cl *kvmclient.Client) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "device_id: %s\nfirmware:  %s\n", cl.DeviceID(), cl.FirmwareVersion())
	for _, m := range []string{"getVideoState", "getUSBState", "getNetworkState", "getActiveExtension", "getATXState"} {
		var out any
		if err := cl.Call(ctx, m, nil, &out); err != nil {
			fmt.Fprintf(&b, "%s: error: %s\n", m, trimErrMsg(err.Error()))
			continue
		}
		js, _ := json.Marshal(out) //nolint:errcheck // rendering a diagnostic line
		fmt.Fprintf(&b, "%s: %s\n", m, js)
	}
	return b.String(), nil
}

// methodCheckMediaURL asks the device whether it can mount a remote image
// directly, without downloading it to device storage first.
func methodCheckMediaURL(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if in.MediaUrl == "" {
		return "", fmt.Errorf("jetkvm: check-media-url requires media_url")
	}
	return callSummary(ctx, cl, "checkMountUrl", map[string]any{"url": in.MediaUrl})
}

// --- input ----------------------------------------------------------------

// methodKeys presses and releases the requested key(s) through the vendored
// typed HID lease, so the auto-release guarantee is the client's.
func methodKeys(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	hold := in.HoldMs
	if hold == 0 {
		hold = holdDefaultMS
	}
	if err := kvmclient.ValidateHoldMS(hold); err != nil {
		return "", err
	}
	lease, err := cl.ControlLease()
	if err != nil {
		return "", err
	}
	held, err := lease.Acquire(ctx, 30*time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = held.Release() }()

	switch in.Method {
	case "key":
		mod, keys, err := kvmclient.ResolveKeyCombo(in.KeyName)
		if err != nil {
			return "", err
		}
		if err := pressAndRelease(ctx, held, mod, keys, hold); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed key %s", in.KeyName), nil
	case "key-combo":
		mod, keys, err := kvmclient.ResolveKeyCombo(in.Combo)
		if err != nil {
			return "", err
		}
		if err := pressAndRelease(ctx, held, mod, keys, hold); err != nil {
			return "", err
		}
		return fmt.Sprintf("Pressed combo %s", in.Combo), nil
	default: // macro
		if len(in.Macro) == 0 {
			return "", fmt.Errorf("jetkvm: macro requires at least one macro step")
		}
		for _, step := range in.Macro {
			mod, keys, err := resolveMacroStep(step)
			if err != nil {
				return "", err
			}
			delay := int(step.Delay)
			if delay == 0 {
				delay = hold
			}
			if err := pressAndRelease(ctx, held, mod, keys, delay); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("Ran macro with %d step(s)", len(in.Macro)), nil
	}
}

// resolveMacroStep converts one authored macro step into a HID report pair.
func resolveMacroStep(step params.JetkvmMacroStep) (byte, []byte, error) {
	mods := append([]string(nil), step.Modifiers...)
	sort.Strings(mods)
	if len(step.Keys) == 1 && len(mods) == 0 {
		return kvmclient.ResolveKeyCombo(step.Keys[0])
	}
	return kvmclient.ResolveKeyCombo(strings.Join(append(mods, step.Keys...), "+"))
}

// pressAndRelease sends a keyboard report, holds, then releases — releasing in
// a defer so an error mid-hold cannot leave a key down.
func pressAndRelease(ctx context.Context, held *kvmclient.Held, modifier byte, keys []byte, holdMS int) error {
	if err := held.SendKeyboardReport(ctx, modifier, keys); err != nil {
		return err
	}
	released := false
	defer func() {
		if !released {
			_ = held.ReleaseKeyboard(context.Background())
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(holdMS) * time.Millisecond):
	}
	if err := held.ReleaseKeyboard(ctx); err != nil {
		return err
	}
	released = true
	return nil
}

// methodType types text as a sequence of key events.
func methodType(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if in.Text == "" {
		return "", fmt.Errorf("jetkvm: type requires text")
	}
	lease, err := cl.ControlLease()
	if err != nil {
		return "", err
	}
	held, err := lease.Acquire(ctx, 30*time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = held.Release() }()

	// Reuse the vendored client's character→HID resolution (US layout, with its
	// own validation and length bounds) and its per-character report ordering;
	// only the report SEND goes through the typed lease here, so the release
	// guarantee stays the client's.
	presses, err := kvmclient.MapTypeReports(in.Text)
	if err != nil {
		return "", err
	}
	for _, p := range presses {
		if err := held.SendKeyboardReport(ctx, p.Modifier, p.Keys); err != nil {
			return "", err
		}
		if err := held.ReleaseKeyboard(ctx); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Typed %d characters", len([]rune(in.Text))), nil
}

// methodPointer clicks or moves the absolute pointer.
func methodPointer(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if err := validateCoord(in.X, in.Y); err != nil {
		return "", err
	}
	lease, err := cl.ControlLease()
	if err != nil {
		return "", err
	}
	held, err := lease.Acquire(ctx, 30*time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = held.Release() }()

	button, _, err := kvmclient.ResolveMouseButton(in.Button, "press")
	if err != nil {
		return "", err
	}
	if err := held.SendPointerReport(ctx, int32(in.X), int32(in.Y), 0); err != nil {
		return "", err
	}
	if in.Method == "click" {
		if err := held.SendPointerReport(ctx, int32(in.X), int32(in.Y), button); err != nil {
			return "", err
		}
		if err := held.SendPointerReport(ctx, int32(in.X), int32(in.Y), 0); err != nil {
			return "", err
		}
		label := in.Button
		if label == "" {
			label = "left"
		}
		return fmt.Sprintf("Clicked %s at (%d, %d)", label, in.X, in.Y), nil
	}
	return fmt.Sprintf("Moved pointer to (%d, %d)", in.X, in.Y), nil
}

// methodScroll sends one wheel event via the legacy JSON-RPC wheel path (the
// firmware's binary HID wheel case is a documented no-op).
func methodScroll(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if err := cl.Scroll(ctx, int8clamp(in.ScrollX), int8clamp(in.ScrollY)); err != nil {
		return "", err
	}
	return fmt.Sprintf("Scrolled by (%d, %d)", in.ScrollX, in.ScrollY), nil
}

// methodDrag performs a bounded pointer drag from (from_x,from_y) to (x,y).
func methodDrag(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if err := validateCoord(in.FromX, in.FromY); err != nil {
		return "", err
	}
	if err := validateCoord(in.X, in.Y); err != nil {
		return "", err
	}
	lease, err := cl.ControlLease()
	if err != nil {
		return "", err
	}
	held, err := lease.Acquire(ctx, 30*time.Second)
	if err != nil {
		return "", err
	}
	defer func() { _ = held.Release() }()

	button, _, err := kvmclient.ResolveMouseButton(in.Button, "press")
	if err != nil {
		return "", err
	}
	// Build the gesture with the vendored builder and validate it with the
	// vendored validator, so the interpolation count, the coordinate bounds and
	// the "must include a pressed state" rule are the client's (R3), not a
	// second hand-rolled loop with its own magic count.
	reports, err := kvmclient.BuildPointerDrag(kvmclient.DragOptions{
		FromX: in.FromX, FromY: in.FromY, ToX: in.X, ToY: in.Y, Buttons: int(button),
	})
	if err != nil {
		return "", err
	}
	released := false
	defer func() {
		if !released {
			_ = held.SendPointerReport(context.Background(), int32(in.X), int32(in.Y), 0)
		}
	}()
	for _, r := range reports {
		if err := held.SendPointerReport(ctx, int32(r.X), int32(r.Y), byte(r.Buttons)); err != nil {
			return "", err
		}
	}
	released = true
	return fmt.Sprintf("Dragged from (%d, %d) to (%d, %d)", in.FromX, in.FromY, in.X, in.Y), nil
}

// --- power ----------------------------------------------------------------

// methodPower sends an ATX power action.
func methodPower(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	switch in.Action {
	case "power-short", "power-long", "reset":
	default:
		return "", fmt.Errorf("jetkvm: power: action must be power-short, power-long or reset (got %q)", in.Action)
	}
	if _, err := callSummary(ctx, cl, "setATXPowerAction", map[string]any{"action": in.Action}); err != nil {
		return "", err
	}
	return fmt.Sprintf("ATX action %s sent", in.Action), nil
}

// methodDCPower sends a DC power on/off/restore action.
func methodDCPower(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	switch in.Action {
	case "on", "off":
		if _, err := callSummary(ctx, cl, "setDCPowerState", map[string]any{"enabled": in.Action == "on"}); err != nil {
			return "", err
		}
	case "restore-on", "restore-off", "restore-last":
		state := map[string]int{"restore-off": 0, "restore-on": 1, "restore-last": 2}[in.Action]
		if _, err := callSummary(ctx, cl, "setDCRestoreState", map[string]any{"state": state}); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("jetkvm: dc-power: action must be on, off, restore-on, restore-off or restore-last (got %q)", in.Action)
	}
	return fmt.Sprintf("DC action %s sent", in.Action), nil
}

// methodReboot asks the DEVICE (not the controlled host) to reboot.
func methodReboot(ctx context.Context, cl *kvmclient.Client) (string, error) {
	if _, err := callSummary(ctx, cl, "reboot", map[string]any{"force": false}); err != nil {
		return "", err
	}
	return "JetKVM reboot requested", nil
}

// methodWOL wakes the controlled host by MAC address.
func methodWOL(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if _, err := callSummary(ctx, cl, "sendWOLMagicPacket", map[string]any{"macAddress": in.Value}); err != nil {
		return "", err
	}
	return fmt.Sprintf("WOL magic packet sent to %s", in.Value), nil
}

// --- media / usb ----------------------------------------------------------

// methodVirtualMedia mounts, unmounts or deletes virtual media.
func methodVirtualMedia(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	mode := in.MediaMode
	if mode == "" {
		mode = "disk"
	}
	switch in.Action {
	case "mount-url":
		if in.MediaUrl == "" {
			return "", fmt.Errorf("jetkvm: virtual-media mount-url requires media_url")
		}
		if _, err := callSummary(ctx, cl, "mountWithHTTP", map[string]any{"url": in.MediaUrl, "mode": mode}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Mounted %s as %s", in.MediaUrl, mode), nil
	case "mount-storage":
		if in.MediaFile == "" {
			return "", fmt.Errorf("jetkvm: virtual-media mount-storage requires media_file")
		}
		if _, err := callSummary(ctx, cl, "mountWithStorage", map[string]any{"filename": in.MediaFile, "mode": mode}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Mounted %s as %s", in.MediaFile, mode), nil
	case "unmount":
		if _, err := callSummary(ctx, cl, "unmountImage", nil); err != nil {
			return "", err
		}
		return "Unmounted virtual media", nil
	case "delete":
		if in.MediaFile == "" {
			return "", fmt.Errorf("jetkvm: virtual-media delete requires media_file")
		}
		if _, err := callSummary(ctx, cl, "deleteStorageFile", map[string]any{"filename": in.MediaFile}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Deleted %s", in.MediaFile), nil
	default:
		return "", fmt.Errorf("jetkvm: virtual-media: action must be mount-url, mount-storage, unmount or delete (got %q)", in.Action)
	}
}

// methodUSBDevice enables or disables one virtual USB device class.
func methodUSBDevice(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	switch in.UsbDevice {
	case "absolute_mouse", "relative_mouse", "keyboard", "mass_storage", "serial_console", "audio":
	default:
		return "", fmt.Errorf("jetkvm: usb-device: unsupported device %q", in.UsbDevice)
	}
	if _, err := callSummary(ctx, cl, "setUsbDeviceState", map[string]any{"device": in.UsbDevice, "enabled": in.UsbEnabled}); err != nil {
		return "", err
	}
	return fmt.Sprintf("USB device %s enabled=%v", in.UsbDevice, in.UsbEnabled), nil
}

// methodUSBEmulation binds or unbinds the whole USB controller.
func methodUSBEmulation(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if _, err := callSummary(ctx, cl, "setUsbEmulationState", map[string]any{"enabled": in.UsbEnabled}); err != nil {
		return "", err
	}
	return fmt.Sprintf("USB emulation enabled=%v", in.UsbEnabled), nil
}

// --- device config --------------------------------------------------------

// methodSetSettings applies a raw JSON settings object.
func methodSetSettings(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	return methodSetJSON(ctx, cl, "setUsbConfig", in.Settings)
}

// methodSetVideo sets the video codec preference, quality factor or sleep mode.
func methodSetVideo(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	switch in.Action {
	case "codec":
		if in.Value != "auto" && in.Value != "h264" && in.Value != "h265" {
			return "", fmt.Errorf("jetkvm: set-video codec: value must be auto, h264 or h265")
		}
		return callSummary(ctx, cl, "setVideoCodecPreference", map[string]any{"codec": in.Value})
	case "quality":
		return callSummary(ctx, cl, "setStreamQualityFactor", map[string]any{"factor": float64(in.IntValue)})
	case "sleep":
		return callSummary(ctx, cl, "setVideoSleepMode", map[string]any{"duration": in.IntValue})
	case "paused":
		return callSummary(ctx, cl, "setVideoStreamPaused", map[string]any{"paused": in.IntValue != 0})
	}
	return "", fmt.Errorf("jetkvm: set-video: action must be codec, quality, sleep or paused (got %q)", in.Action)
}

// methodSetDisplay sets the display rotation or backlight settings.
func methodSetDisplay(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	switch in.Action {
	case "rotation":
		return callSummary(ctx, cl, "setDisplayRotation", map[string]any{"params": map[string]any{"rotation": in.Value}})
	case "backlight":
		return callSummary(ctx, cl, "setBacklightSettings", map[string]any{"params": map[string]any{"max_brightness": in.IntValue}})
	}
	return "", fmt.Errorf("jetkvm: set-display: action must be rotation or backlight (got %q)", in.Action)
}

// methodSetAudio enables or disables audio capture.
func methodSetAudio(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	return callSummary(ctx, cl, "setAudioConfig", map[string]any{"params": map[string]any{"enabled": in.IntValue != 0}})
}

// methodSetNetwork applies a raw JSON network settings object.
func methodSetNetwork(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if strings.TrimSpace(in.Settings) == "" {
		return "", fmt.Errorf("jetkvm: set-network requires settings (a JSON object)")
	}
	var settings any
	if err := json.Unmarshal([]byte(in.Settings), &settings); err != nil {
		return "", fmt.Errorf("jetkvm: set-network: settings is not valid JSON: %w", err)
	}
	return callSummary(ctx, cl, "setNetworkSettings", map[string]any{"settings": settings})
}

// methodSetScalar applies a one-string-field setter.
func methodSetScalar(ctx context.Context, cl *kvmclient.Client, rpc, field, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("jetkvm: %s requires value", rpc)
	}
	if _, err := callSummary(ctx, cl, rpc, map[string]any{field: value}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s applied", rpc), nil
}

// methodSetBool applies a one-bool-field setter from the value string.
func methodSetBool(ctx context.Context, cl *kvmclient.Client, rpc, field, value string) (string, error) {
	enabled, ok := parseBool(value)
	if !ok {
		return "", fmt.Errorf("jetkvm: %s requires value=true|false (got %q)", rpc, value)
	}
	if _, err := callSummary(ctx, cl, rpc, map[string]any{field: enabled}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s applied", rpc), nil
}

// methodSetJSON applies a setter taking a parsed JSON settings object.
func methodSetJSON(ctx context.Context, cl *kvmclient.Client, rpc, settings string) (string, error) {
	if strings.TrimSpace(settings) == "" {
		return "", fmt.Errorf("jetkvm: %s requires settings (a JSON value)", rpc)
	}
	var v any
	if err := json.Unmarshal([]byte(settings), &v); err != nil {
		return "", fmt.Errorf("jetkvm: %s: settings is not valid JSON: %w", rpc, err)
	}
	if _, err := callSummary(ctx, cl, rpc, map[string]any{"settings": v}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s applied", rpc), nil
}

// methodRawRPC is the escape hatch: invoke any device JSON-RPC method, so a
// method this plugin has not typed yet is reachable without a release.
func methodRawRPC(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if rpcForbiddenMethods[in.RpcMethod] {
		return "", fmt.Errorf("jetkvm: rpc: %q is refused: it irreversibly reimages or wipes a physical device and is never reachable through the raw escape hatch", in.RpcMethod)
	}
	var p map[string]any
	if strings.TrimSpace(in.RpcParams) != "" {
		if err := json.Unmarshal([]byte(in.RpcParams), &p); err != nil {
			return "", fmt.Errorf("jetkvm: rpc: rpc_params is not valid JSON: %w", err)
		}
	}
	var out any
	if err := cl.Call(ctx, in.RpcMethod, p, &out); err != nil {
		return "", err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("jetkvm: rpc: encoding result: %w", err)
	}
	return string(b), nil
}

// --- helpers --------------------------------------------------------------

// maxCoordinate is the absolute-pointer range the JetKVM HID descriptor
// advertises; a coordinate outside it is an authoring error, not a device error.
const maxCoordinate = 32767

// validateCoord rejects a coordinate outside the pointer's advertised range.
func validateCoord(x, y int) error {
	for _, v := range []int{x, y} {
		if v < 0 || v > maxCoordinate {
			return fmt.Errorf("jetkvm: coordinate %d out of range 0..%d", v, maxCoordinate)
		}
	}
	return nil
}

// int8clamp clamps a wheel delta into the int8 the firmware expects.
func int8clamp(v int) int8 {
	switch {
	case v > 127:
		return 127
	case v < -128:
		return -128
	}
	return int8(v)
}

// parseBool accepts the JSON-ish boolean spellings a YAML value string may carry.
func parseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}
