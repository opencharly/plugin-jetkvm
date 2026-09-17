package kvmclient

// charly_extension.go — the LOCAL EXTENSION to the vendored client.
//
// The vendored upstream client (see ../../../../third_party/NOTICE) deliberately
// implements only the read-only inspection path plus gated HID input. plugin-jetkvm
// additionally serves JetKVM's device-ADMINISTRATION RPC methods (power/ATX,
// virtual media, USB gadget, EDID, display, network, tailscale, OTA status, and a
// raw escape hatch), which all ride the SAME already-negotiated "rpc" JSON-RPC
// channel.
//
// This file therefore adds exactly ONE primitive — an exported Call onto that
// channel — rather than reimplementing the transport, the session, the
// reconnection/handshake logic, or the JSON-RPC framing. Every admin method in
// the plugin is a thin typed wrapper over Call. This keeps the wire client
// single-sourced (R3): there is no second RPC client in this repository.

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient/hidproto"
)

// NormalizeDeviceURL makes the plugin's documented `host` contract real: a bare
// hostname or host:port (the form a user naturally writes) is given the device's
// default plaintext scheme, while an explicit http:// or https:// is left alone.
//
// JetKVM's local API is plaintext by default and its TLS certificate is
// self-signed, so plaintext is the correct DEFAULT here (see the upstream
// security note); an author who has installed a trusted certificate writes
// https:// explicitly.
func NormalizeDeviceURL(host string) string {
	h := strings.TrimSpace(host)
	if h == "" {
		return h
	}
	if strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") {
		return h
	}
	return "http://" + h
}

// applyInsecureTLS installs a TLS-verification-skipping transport when the
// author set `insecure: true`. It exists because a JetKVM ships a SELF-SIGNED
// certificate: without it an https:// device is unreachable, and the only
// alternatives would be trusting the CA out of band or downgrading to plaintext.
// It is deliberately opt-in and named "insecure" so the tradeoff is explicit at
// the authoring site.
func applyInsecureTLS(hc *httpClient) {
	tr, ok := hc.hc.Transport.(*http.Transport)
	if !ok || tr == nil {
		tr = http.DefaultTransport.(*http.Transport).Clone() //nolint:errcheck // a *http.Transport is the documented default
	}
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tr.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // explicit opt-in for a self-signed device certificate
	hc.hc.Transport = tr
}

// insecureTLSConfig returns the TLS config the signaling websocket dial needs
// when the author opted into skipping verification.
func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} //nolint:gosec // explicit opt-in
}

// Call issues an arbitrary JSON-RPC 2.0 request on the session's control channel
// and decodes the result into out (when non-nil). It serialises against the same
// command lock every other Client operation uses, so an admin call cannot
// interleave with a screenshot or a status probe.
//
// The JetKVM firmware returns heterogeneous result shapes per method; callers
// pass a type matching the method they invoke. A method the firmware does not
// implement surfaces as an RPCError with code -32601, which the plugin turns
// into a skip (the capability is absent on that firmware) rather than a failure.
func (c *Client) Call(ctx context.Context, method string, params map[string]any, out any) error {
	unlock, err := c.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return c.sess.rpc.call(ctx, method, params, out)
}

// Control returns the HID control lease. It is non-nil only when the client was
// constructed with AllowControl, so a caller that did not opt in cannot reach
// the input channel at all. Held exposes the typed reports (keyboard, pointer,
// relative mouse) and their release guarantees.
func (c *Client) ControlLease() (*controlLease, error) { return c.Control() }

// KeyBufferSize is the number of simultaneous keys in a HID keyboard report —
// re-exported so the plugin need not import the hidproto subpackage directly.
const KeyBufferSize = hidproto.HIDKeyBufferSize

// DefaultDragSteps is the number of interpolated intermediate pointer reports a
// drag gesture emits between its endpoints. It is a NAMED choice, not a magic
// count buried in a loop: enough that the target OS sees real movement rather
// than a teleport, while staying far below the client's MaxDragSteps bound.
const DefaultDragSteps = 24

// DragOptions describes one absolute-pointer drag gesture.
type DragOptions struct {
	FromX, FromY int
	ToX, ToY     int
	// Buttons is the pressed button mask; must be nonzero for a drag.
	Buttons int
	// Steps overrides DefaultDragSteps when > 0.
	Steps int
}

// BuildPointerDrag builds and validates a complete drag gesture via the vendored
// builder, returning the reports in send order (press, intermediates, release).
// Wrapping it here keeps the client's coordinate validation, its MaxDragSteps
// bound and its "must include a pressed state" rule as the single source (R3).
func BuildPointerDrag(o DragOptions) ([]PointerDragReport, error) {
	steps := o.Steps
	if steps <= 0 {
		steps = DefaultDragSteps
	}
	return BuildPointerDragReports(o.FromX, o.FromY, o.ToX, o.ToY, o.Buttons, steps)
}

// TypedReport is one keyboard report a text character expands to: the modifier
// byte and the up-to-6-byte key-usage buffer the HID protocol carries. It is
// the narrow shape a caller needs to SEND a typed keypress through the lease,
// so the character→HID mapping itself stays the vendored client's.
type TypedReport struct {
	Modifier byte
	Keys     []byte
}

// MapTypeReports expands text into the ordered HID keyboard reports that type
// it on a US layout — the same mapping the vendored client's own typing path
// uses (MapTypeString), projected into the bytes a report send needs. Keeping
// this projection here (rather than in the plugin) means the client's layout
// table, validation and length bounds are the single source for typed input.
func MapTypeReports(text string) ([]TypedReport, error) {
	presses, err := MapTypeString(text)
	if err != nil {
		return nil, err
	}
	out := make([]TypedReport, 0, len(presses))
	for _, p := range presses {
		keys := make([]byte, hidproto.HIDKeyBufferSize)
		if p.HIDUsageCode != 0 {
			keys[0] = byte(p.HIDUsageCode)
		}
		out = append(out, TypedReport{Modifier: byte(p.Modifier), Keys: keys})
	}
	return out, nil
}
