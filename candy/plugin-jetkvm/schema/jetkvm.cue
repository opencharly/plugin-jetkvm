// The `jetkvm` plugin's OWN CUE schema — the typed plugin_input for the
// `jetkvm` IP-KVM check/control verb.
//
// It is the SINGLE SOURCE for this plugin's params, used two ways (the same
// contract core `spec` and every other plugin use):
//
//  1. GENERATE the Go param struct — `cue exp gengotypes` (driven by the
//     cue:gen pipeline, which wraps this with `package params` + `@go(params)`)
//     emits ../params/cue_types_gen.go, so the provider decodes plugin_input
//     into a TYPED struct, never a hand-parsed map.
//  2. VALIDATE authored input AT RUNTIME — the plugin serves this source over
//     the Describe channel; the host splices it onto the base (base ++ plugin)
//     and validates every authored `jetkvm:` step's plugin_input against
//     #JetkvmInput.
//
// An authored `jetkvm: <method>` step (scalar sugar) or
// `jetkvm: {method: …, x: …}` (map form) desugars to the INTERNAL
// plugin/plugin_input envelope, and every jetkvm-exclusive modifier lives
// HERE. The shared assertion matchers (exit_status/stdout/stderr) and the
// general `timeout` stay on core #Op, read off the step Op by the provider.
//
// SELF-CONTAINED: it references NO base def, so it compiles standalone
// (gengotypes + the load-gate compile) AND splices onto the base (base ++ plugin
// is a def-name collision check, not a base-reference resolver).
#JetkvmInput: {
	// method — the jetkvm method to dispatch (also the scalar-sugar primary:
	// `jetkvm: <method>`).
	method: #JetkvmMethod

	// --- connection -------------------------------------------------------
	// host — the device address ("host", "host:port", or an http(s) URL). When
	// omitted the provider falls back to the deploy's venue address.
	host?: string & !=""
	// password — the device password. Prefer password_secret (the credential
	// store); this literal exists for ad-hoc/CI use.
	password?: string
	// password_secret — the credential-store key holding the device password.
	password_secret?: string @go(PasswordSecret)
	// auth_token — an already-valid authToken session cookie value, skipping login.
	auth_token?: string @go(AuthToken)
	// allow_control — REQUIRED to be true for every mutating method (input,
	// power, USB, virtual media, config writes). Read-only methods ignore it.
	// This is the device-safety gate: a physical appliance is not disposable.
	allow_control?: bool @go(AllowControl)
	// insecure — permit a self-signed TLS certificate (the device ships
	// self-signed by default). Also implied by an http:// host.
	insecure?: bool

	// --- input (keyboard/pointer) ----------------------------------------
	// text — the text `type` types.
	text?: string
	// key — the named key `key` presses (Return, Escape, F5, Control_L, ...).
	key?: string @go(KeyName)
	// combo — the chord `key-combo` presses, e.g. "Control_L+Alt_L+Delete".
	combo?: string
	// hold_ms — how long `key`/`combo` holds before release (default 40).
	hold_ms?: int & >=0 @go(HoldMs,type=int)
	// x / y — desktop-absolute coordinates (click/mouse/move/drag to).
	x?: int @go(,type=int)
	y?: int @go(,type=int)
	// from_x / from_y — drag start coordinates.
	from_x?: int @go(FromX,type=int)
	from_y?: int @go(FromY,type=int)
	// button — pointer button (left/right/middle; default left).
	button?: string
	// scroll_x / scroll_y — wheel deltas (scroll).
	scroll_x?: int @go(ScrollX,type=int)
	scroll_y?: int @go(ScrollY,type=int)
	// macro — the ordered key-steps a `macro` method executes.
	macro?: [...#JetkvmMacroStep]

	// --- power / ATX / DC -------------------------------------------------
	// action — the power action: power-short | power-long | reset | on | off |
	// restore-on | restore-off | restore-last.
	action?: string

	// --- virtual media ----------------------------------------------------
	// media_url — the HTTP(S) URL `virtual-media` mounts.
	media_url?: string @go(MediaUrl)
	// media_file — the device-storage filename `virtual-media` mounts/deletes.
	media_file?: string @go(MediaFile)
	// media_mode — cdrom | disk.
	media_mode?: string @go(MediaMode)

	// --- usb --------------------------------------------------------------
	// usb_device — absolute_mouse | relative_mouse | keyboard | mass_storage |
	// serial_console | audio.
	usb_device?: string @go(UsbDevice)
	// usb_enabled — enable/disable the addressed usb_device (or the whole
	// emulation bus for `usb-emulation`).
	usb_enabled?: bool @go(UsbEnabled)

	// --- config / network -------------------------------------------------
	// value — the generic scalar a set-style method writes (rotation, layout,
	// codec, factor, log level, extension id, ...).
	value?: string
	// int_value — the generic integer a set-style method writes (brightness,
	// dim_after, off_after, ...).
	int_value?: int @go(IntValue,type=int)
	// settings — the raw JSON object a get/set settings method round-trips.
	settings?: string

	// --- raw RPC escape hatch ---------------------------------------------
	// rpc_method — the device JSON-RPC method `rpc` invokes.
	rpc_method?: string @go(RpcMethod)
	// rpc_params — the JSON object of params for `rpc`.
	rpc_params?: string @go(RpcParams)

	// --- artifact ---------------------------------------------------------
	// artifact — the host path `screenshot` writes the PNG to.
	artifact?: string
	// artifact_dir — the runner-injected generic evidence-artifact dir.
	artifact_dir?: string @go(ArtifactDir)
	// session_id — the detached-recorder session id (`session`).
	session_id?: string @go(SessionId)
	// state_dir — the detached recorder's state directory.
	state_dir?: string @go(StateDir)
	// fps — the detached recorder capture rate (default 5).
	fps?: int & >=1 @go(Fps,type=int)
	// duration_sec — how long a recording/bounded capture runs.
	duration_sec?: int & >=0 @go(DurationSec,type=int)
	// log_dir — the detached recorder's log directory.
	log_dir?: string @go(LogDir)
	// venue / phase — stamped into the recorder's evidence row.
	venue?: string @go(Venue)
	phase?: string @go(Phase)

	// artifact_min_bytes / artifact_min_dimensions / artifact_not_uniform —
	// the post-run artifact-reality assertions (sdk.RunArtifactValidators).
	artifact_min_bytes?:      int & >=0                    @go(ArtifactMinBytes,type=int)
	artifact_min_dimensions?: string & =~"^[0-9]+x[0-9]+$" @go(ArtifactMinDimensions)
	artifact_not_uniform?:    bool                         @go(ArtifactNotUniform)
}

// #JetkvmMethod — the method catalog. Grouped by intent so the read-only
// surface is separable from the mutating one (the allow_control gate).
//
// NOTE: written as a bare trailing-pipe disjunction, NOT `(...)`-wrapped: CUE
// does not terminate a parenthesised expression at a newline, so the grouped
// form requires commas and breaks `cue exp gengotypes`.
#JetkvmMethod: string &
	// observation (read-only)
	"status" |
	"screenshot" |
	"version" |
	"diagnostics" |
	"video-state" |
	"usb-state" |
	"atx-state" |
	"dc-state" |
	"virtual-media-state" |
	"storage-files" |
	"wol-devices" |
	"macros" |
	"keyboard-layout" |
	"timezones" |
	"cloud-state" |
	"network-state" |
	"network-settings" |
	"tailscale-status" |
	"update-status" |
	"devmode-state" |
	"ssh-key" |
	"tls-state" |
	"extensions" |
	"public-ip" |
	// input (mutating)
	"key" |
	"type" |
	"key-combo" |
	"macro" |
	"click" |
	"mouse" |
	"move" |
	"scroll" |
	"drag" |
	// power (mutating)
	"power" |
	"dc-power" |
	"reboot" |
	"wol" |
	// media / usb (mutating)
	"virtual-media" |
	"check-media-url" |
	"usb-device" |
	"usb-emulation" |
	"usb-config" |
	// device config (mutating)
	"set-settings" |
	"set-edid" |
	"set-video" |
	"set-display" |
	"set-audio" |
	"set-network" |
	"set-tailscale" |
	"set-devmode" |
	"set-ssh-key" |
	"set-tls" |
	"set-keyboard-layout" |
	"set-macros" |
	"set-jiggler" |
	"set-extension" |
	"set-wol-devices" |
	"set-log-level" |
	"renew-dhcp" |
	"factory-reset" |
	// lifecycle
	"update" |
	"session" |
	// raw escape hatch
	"rpc"

// #JetkvmMacroStep — one step of a keyboard macro.
#JetkvmMacroStep: {
	keys?:      [...string]
	modifiers?: [...string]
	delay?:     int & >=0
}
