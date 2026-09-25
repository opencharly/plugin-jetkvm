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
	// omitted the provider falls back to the JETKVM_HOST environment variable,
	// then to the deploy's venue address. Authoring no host (and setting
	// JETKVM_HOST) keeps a device-specific hostname out of a committed plan.
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
	// x / y — ABSOLUTE HID pointer coordinates for click/mouse/move/drag, in
	// the device's [0,32767] range — NOT desktop pixels. Map a desktop pixel
	// (px,py) on a WxH screen to (px*32767/(W-1), py*32767/(H-1)); the centre of
	// a 1920x1080 screen is about (16384,16384).
	x?: int @go(,type=int)
	y?: int @go(,type=int)
	// from_x / from_y — drag start coordinates.
	from_x?: int @go(FromX,type=int)
	from_y?: int @go(FromY,type=int)
	// button — pointer button (default left). Used by `click` and `drag`.
	// `move`/`mouse` are pure position moves and NEVER press a button, so a
	// `button:` on them is validated but not sent.
	button?: "left" | "right" | "middle"
	// scroll_x / scroll_y — wheel deltas (scroll).
	scroll_x?: int @go(ScrollX,type=int)
	scroll_y?: int @go(ScrollY,type=int)
	// macro — the ordered key-steps a `macro` method executes.
	macro?: [...#JetkvmMacroStep]

	// --- power / ATX / DC -------------------------------------------------
	// action — the per-method action verb. It is a CUE enum of every action the
	// catalog accepts ACROSS the action-bearing methods (power, dc-power,
	// virtual-media, set-video, set-display, set-audio), so a typo is rejected at
	// validate time and the valid verbs are generated into the docs. The
	// method-SPECIFIC subset (e.g. `power` accepts only power-short/power-long/
	// reset) is enforced at dispatch, because a single shared field cannot carry
	// per-method disjunctions without degrading `cue exp gengotypes` (SDD — see
	// #JetkvmAction's note).
	action?: #JetkvmAction

	// --- virtual media ----------------------------------------------------
	// media_url — the HTTP(S) URL `virtual-media` mounts.
	media_url?: string @go(MediaUrl)
	// media_file — the device-storage filename `virtual-media` mounts/deletes.
	media_file?: string @go(MediaFile)
	// media_mode — cdrom | disk.
	media_mode?: "cdrom" | "disk" @go(MediaMode)

	// --- usb --------------------------------------------------------------
	// usb_device — the virtual USB device class to enable/disable.
	usb_device?: "absolute_mouse" | "relative_mouse" | "keyboard" | "mass_storage" | "serial_console" | "audio" @go(UsbDevice)
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

	// --- console OCR / installer -----------------------------------------
	// OCR is the read-only screen-reading method: capture the frame, run OCR
	// over it, and assert `text` (the field above, shared with `type`) is
	// present. It is the wait-for-screen primitive in `check:`/`run:` form (no
	// artifact required), the sibling of the `artifact_contains_text` validator
	// on `screenshot`.
	// install is the configurable console-wizard DRIVER (mutating): connect
	// once and walk an ordered `steps:` recipe, OCR-waiting for each screen's
	// anchor before sending its input. The recipe is generic DATA — the Omarchy
	// (or any) installer's screens are supplied by the entity, never hardcoded
	// here. It drives ANY text-console wizard: an OS installer, a first-boot
	// provisioning flow, a firmware setup screen.
	// steps — an INLINE recipe the `install` method drives (wins over the
	// entity recipe).
	steps?: [...#JetkvmInstallStep] @go(Steps)
	// device — the name of a `kind: jetkvm` device entity whose named recipe
	// (`recipe:`) this step uses INSTEAD of inline steps:. The verb resolves it
	// out-of-process over its reverse channel, so recipes live in one place in
	// charly.yml and a bed references them by name.
	device?: string @go(Device)
	// recipe — WHICH named recipe on the device entity to drive (default
	// "install"). A device can carry several — e.g. `install` for the OS
	// installer and `first_boot` for the post-reboot owner-provisioning wizard.
	recipe?: string @go(Recipe)
	// answers — a name → value map `install` substitutes into every step's
	// `text` via `{{name}}` placeholders, so one recipe serves many machines
	// without editing the steps. The substitution is literal + single-pass
	// (NOT charly `${VAR}` expansion), independent of the check env.
	answers?: {[string]: string} @go(Answers)
	// answer_secrets — a name → CREDENTIAL-STORE KEY map, resolved at run time
	// and merged into `answers`. Use it for anything secret (a password, a LUKS
	// passphrase): the value is read from the credential store over the reverse
	// channel and NEVER appears in charly.yml. Explicit `answers` entries win.
	answer_secrets?: {[string]: string} @go(AnswerSecrets)
	// answers_env — a name → ENVIRONMENT-VARIABLE-NAME map, resolved at run time
	// and merged into `answers`. The fully-configurable-from-the-environment
	// path: the operator sets the env var and the recipe's {{placeholder}}
	// resolves to it, with nothing machine-specific committed. Precedence:
	// answers (authored) > answer_secrets > answers_env.
	answers_env?: {[string]: string} @go(AnswersEnv)

	// --- console terminal session (open / run / close / LUKS) -------------
	// `open-terminal`, `run-command`, `close-terminal` and `luks-unlock` drive
	// an INTERACTIVE shell (or the initramfs prompt) on the controlled machine,
	// reading each result by OCR. They share the transport-agnostic
	// kit.ConsoleSession engine, so the same methods serve the JetKVM and SPICE
	// transports (R3). The command COMPLETION is detected by an opaque marker the
	// shell echoes (never a prompt substring, which the terminal echo would
	// satisfy before the command ran — the RCA behind the marker design).
	//
	// open-terminal opens a terminal: it sends `terminal_combo` (default
	// "super+Return", Omarchy's terminal hotkey; use "ctrl+alt+F3" for a bare
	// text TTY) and OCR-waits for one of `prompt_anchors` (the shell prompt).
	// run-command runs `commands` in the open terminal, OCR-reading each result;
	// with `close_terminal: true` it exits afterwards. close-terminal exits an
	// open terminal (`exit`). luks-unlock types `passphrase` / its secret and
	// waits for one of `outcomes` (the boot proceeding, an error, a login).
	//
	// terminal_combo — the chord `open-terminal` sends (default "super+Return").
	terminal_combo?: string @go(TerminalCombo)
	// prompt_anchors — the substrings that mean a terminal is ready. Default
	// ["$", "#", ">"] (the common shell prompts). Screen-unique enough for the
	// readiness wait; override for an unusual prompt.
	prompt_anchors?: [...string] @go(PromptAnchors)
	// commands — the ordered commands `run-command` executes.
	commands?: [...#JetkvmSessionCommand] @go(Commands)
	// close_terminal — when true, `run-command` exits the terminal after the last
	// command (a convenience so one step opens, runs and closes).
	close_terminal?: bool @go(CloseTerminal)
	// sudo_password — the password `run-command` types at a sudo prompt. Prefer
	// sudo_password_secret (the credential store); this literal exists for
	// ad-hoc/CI use and is never required when no command is `sudo: true`.
	sudo_password?: string @go(SudoPassword)
	// sudo_password_secret — the credential-store key holding the sudo password.
	sudo_password_secret?: string @go(SudoPasswordSecret)
	// passphrase — the LUKS/disk-encryption passphrase `luks-unlock` types.
	// Prefer passphrase_secret.
	passphrase?: string
	// passphrase_secret — the credential-store key holding the LUKS passphrase.
	passphrase_secret?: string @go(PassphraseSecret)
	// outcomes — the SUCCESS anchors `luks-unlock` waits for (the boot
	// proceeding, a login prompt). A wrong passphrase is caught by a built-in
	// failure anchor set and FAILS the step, so this only names success.
	outcomes?: [...string] @go(Outcomes)

	// --- console FLOW (continuous OCR + if/then/else + case/switch + while) --
	// The `flow` method drives a BOUNDED STATE MACHINE over the console: each
	// node OCR-polls CONTINUOUSLY (never a guessed timeout) until one of its
	// NAMED outcomes appears, sends its action, then routes to the next node by
	// the OBSERVED outcome. A transition back to an earlier node is a while
	// loop, bounded by max_loops/max_steps so it can never spin forever.
	//
	// flow_start — the entry node id.
	flow_start?: string @go(FlowStart)
	// flow_nodes — id -> node. Each node: a `wait` list of named outcomes, an
	// optional `action`, `transitions` (outcome -> next id) for if/then/else and
	// case/switch, and a default `next`.
	flow_nodes?: {[string]: #JetkvmFlowNode} @go(FlowNodes)
	// flow_max_steps / flow_max_loops — the loop bounds (defaults 200 / 50).
	flow_max_steps?: int & >=1 @go(FlowMaxSteps,type=int)
	flow_max_loops?: int & >=1 @go(FlowMaxLoops,type=int)

	// --- boot order (EFI boot manager, target-side over the terminal) ------
	// `boot-order` sets the UEFI boot order from INSIDE the running system, using
	// the EFI boot manager `efibootmgr` in an open terminal — the OS-side
	// counterpart to pressing the firmware boot-menu key (F11/F12). Actions:
	//   list — run `efibootmgr` and return the entries (BootCurrent/BootOrder/
	//          BootNNNN lines) read by OCR.
	//   next — one-time next boot: `efibootmgr --bootnext <entry>` (does NOT
	//          change the persistent order; ideal for booting an installer medium
	//          once, then returning to the disk).
	//   set  — persist the order: `efibootmgr --bootorder <sequence>`.
	boot_order_action?: "list" | "next" | "set" @go(BootOrderAction)
	// boot_order_entry — the entry number for action: next (e.g. "0003").
	boot_order_entry?: string @go(BootOrderEntry)
	// boot_order_sequence — the comma-separated entry order for action: set
	// (e.g. "0003,0001,0002").
	boot_order_sequence?: string @go(BootOrderSequence)
	// boot_order_command — overrides the boot-manager binary (default
	// "efibootmgr"), for a system that names it differently.
	boot_order_command?: string @go(BootOrderCommand)

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

	// artifact_min_bytes / artifact_min_dimensions / artifact_not_uniform /
	// artifact_contains_text — the post-run artifact-reality assertions
	// (sdk.RunArtifactValidators) THIS verb can produce. artifact_contains_text is
	// the OCR wait-for-screen primitive in `screenshot` form: it runs tesseract on
	// the HOST over the pulled PNG and asserts the text is present, so a
	// `screenshot` step can gate on what the screen actually shows.
	//
	// artifact_min_cast_events is deliberately NOT here: it validates an asciinema
	// `.cast` file, and the jetkvm verb produces only PNGs — the field would be
	// dead surface, so it is omitted rather than shipped unexercised.
	artifact_min_bytes?:      int & >=0                    @go(ArtifactMinBytes,type=int)
	artifact_min_dimensions?: string & =~"^[0-9]+x[0-9]+$" @go(ArtifactMinDimensions)
	artifact_not_uniform?:    bool                         @go(ArtifactNotUniform)
	artifact_contains_text?:  string                       @go(ArtifactContainsText)
}

// #JetkvmInstallStep — ONE step of a console-installer recipe driven by the
// `install` method. Each step OCR-waits for its `wait_for` anchor to appear on
// the screen, then performs ONE input action.
//
// A REAL WIZARD recipe MUST use a screen-UNIQUE anchor per step: a string present
// on every screen (e.g. a logo) passes vacuously and desynchronises the whole
// drive. A single-step PROBE recipe whose only job is to prove the OCR read and
// the transport's input may deliberately use a broad anchor — there is no next
// step to desynchronise against — and the entity bed's `probe` recipe is exactly
// that.
#JetkvmInstallStep: {
	// wait_for — the text the step waits for before acting. Screen-unique for a
	// multi-step wizard recipe; a single-step probe may use a broad anchor.
	wait_for: string & !="" @go(WaitFor)
	// action — the input to send once `wait_for` is on screen. Omitted means the
	// step only waits (a pure synchronisation/observation step).
	action?: "key" | "type" | "key-combo"
	// key — the named key for action: key (Return, Escape, F5, ...).
	key?: string @go(KeyName)
	// combo — the chord for action: key-combo (ctrl+c, ctrl+alt+Delete, ...).
	combo?: string
	// text — the text for action: type.
	text?: string @go(Text)
	// timeout_sec — how long to wait for `wait_for` before failing (default 120).
	timeout_sec?: int & >=1 @go(TimeoutSec,type=int)
	// optional — when true, a wait that times out (the anchor never appears)
	// SKIPS the step instead of failing the drive, and its action is not sent.
	// Use it for a screen that is only sometimes present (a keyboard picker on a
	// machine that already recorded one, an install-mode picker only offered when
	// free space exists) — so one recipe serves both shapes.
	optional?: bool @go(Optional)
	// artifact — optional host path to save the frame captured for this step.
	artifact?: string
	// description — optional human label for the step's evidence line.
	description?: string @go(Description)
}

// #JetkvmAction — the union of every action verb the action-bearing methods
// accept. It is the CUE-expressible half of the action contract: the SET of legal
// verbs is enforced here at validate time (a typo fails before any device call).
//
// WHY NOT A PER-METHOD DISJUNCTION (SDD): the natural shape would be one action
// field carrying `if method == "power" { action: "power-short" | ... }` rules.
// That is INEXPRESSIBLE without degrading `cue exp gengotypes`: a conditional
// that constrains a field to a method-specific set evaluates with `method` still
// abstract, every branch stays unresolved, and the generator emits the WHOLE
// struct as `any` (measured — the same limitation documented at
// spec/schema/vm.cue's #LibvirtGraphics note). The method-SPECIFIC subset is
// therefore enforced at dispatch, which is also where the error can name the
// method. The union below keeps the generator healthy.
#JetkvmAction: string &
	// ATX power (methodPower)
	"power-short" |
	"power-long" |
	"reset" |
	// DC power (methodDCPower)
	"on" |
	"off" |
	"restore-on" |
	"restore-off" |
	"restore-last" |
	// virtual media (methodVirtualMedia)
	"mount-url" |
	"mount-storage" |
	"unmount" |
	"delete" |
	// set-video (methodSetVideo)
	"codec" |
	"quality" |
	"sleep" |
	"paused" |
	// set-display (methodSetDisplay)
	"rotation" |
	"backlight"

// #JetkvmSessionCommand — ONE command the `run-command` method runs in an OPEN
// terminal session, reading its result by OCR.
#JetkvmSessionCommand: {
	// command — the shell command line to run.
	command: string & !="" @go(Command)
	// sudo — run through sudo (prefixed with `sudo `), entering the session's
	// sudo password at the prompt when the target asks for it.
	sudo?: bool @go(Sudo)
	// expect — optional substring that MUST appear in the OCR-read result; when
	// set and absent the command fails naming what was read. It is the
	// assertion half of "read the results via OCR".
	expect?: string @go(Expect)
	// timeout_sec — how long to wait for the completion marker (default 120).
	timeout_sec?: int & >=1 @go(TimeoutSec,type=int)
	// artifact — optional host path the completion frame is written to.
	artifact?: string
	// description — optional human label.
	description?: string @go(Description)
}

// #JetkvmFlowOutcome — ONE named condition a flow node waits for. The engine
// OCR-polls continuously until any outcome's `match` substring is on screen.
#JetkvmFlowOutcome: {
	// name — the outcome identifier, keyed in a node's `transitions`.
	name: string & !="" @go(Name)
	// match — the case-insensitive substring that identifies this outcome.
	match: string & !="" @go(Match)
	// failure — marks a failure outcome (a wrong-passphrase / error screen). It
	// fails the flow unless a `transitions` entry routes it (a recovery branch).
	failure?: bool @go(Failure)
}

// #JetkvmFlowNode — ONE state of a console flow. It OCR-polls for a `wait`
// outcome, sends `action`, then routes by the observed outcome.
#JetkvmFlowNode: {
	// description — optional human label for the evidence line.
	description?: string @go(Description)
	// wait — the named outcomes to OCR-poll for. Empty = act immediately.
	wait?: [...#JetkvmFlowOutcome] @go(Wait)
	// action — what to send once a wait matched (or immediately). At most one of
	// the fields is used.
	key?: string @go(Key)
	combo?: string @go(Combo)
	text?: string @go(Text)
	// command / sudo / expect — run a shell command in the OPEN terminal and read
	// its output by OCR (the same marker-completed path as `run-command`).
	command?: string @go(Command)
	sudo?: bool @go(Sudo)
	expect?: string @go(Expect)
	close_terminal?: bool @go(CloseTerminal)
	// transitions — outcome name -> next node id (if/then/else, case/switch). A
	// matched outcome with no entry falls through to `next`.
	transitions?: {[string]: string} @go(Transitions)
	// next — the default target when the matched outcome has no transition.
	next?: string @go(Next)
	// artifact — optional host path saving the frame captured at this node.
	artifact?: string
	// timeout_sec — bounds this node's OCR poll (default 120).
	timeout_sec?: int & >=1 @go(TimeoutSec,type=int)
}

// #JetkvmDeviceInput — the authored `kind: jetkvm` DEVICE entity body. It is
// the plugin's configurable surface: the device address + credentials, and an
// optional installer recipe + answers. Consumed by the plugin's OpLoad (to
// store the typed entity) and used by `charly jetkvm install <entity>` and by
// a local/VM deploy that names it.
#JetkvmDeviceInput: {
	// host — the device address. Omit to fall back to JETKVM_HOST at runtime.
	host?: string & !=""
	// insecure — permit the device's self-signed TLS certificate.
	insecure?: bool
	// password_secret — credential-store key holding the device password.
	password_secret?: string @go(PasswordSecret)
	// description — human label for the device.
	description?: string @go(Description)
	// installer — the console-installer recipe + answers for this device.
	installer?: #JetkvmInstaller @go(Installer,optional=nillable)
}

// #JetkvmInstaller — the installer/wizard configuration for a `kind: jetkvm`
// device. One device can carry SEVERAL named recipes (an OS installer AND the
// post-reboot first-boot provisioning wizard are two), each a named `steps:`
// list under `recipes:`.
#JetkvmInstaller: {
	// recipes — name → ordered recipe. The conventional names are `install`
	// (the OS installer, the default) and `first_boot` (the post-reboot owner
	// provisioning wizard), but any name works: a step selects one with
	// `recipe:`.
	recipes?: {[string]: [...#JetkvmInstallStep]} @go(Recipes)
	// steps — a SHORTCUT for recipes.install, so a single-recipe device needs no
	// nesting. If both are set, `recipes.install` wins.
	steps?: [...#JetkvmInstallStep] @go(Steps)
	// answers — a name → value map the install driver substitutes into step
	// `text` fields via `{{name}}` placeholders.
	answers?: {[string]: string} @go(Answers)
	// answer_secrets — a name → credential-store key map, resolved at run time
	// and merged into answers. Use for secrets so nothing plaintext is committed.
	answer_secrets?: {[string]: string} @go(AnswerSecrets)
	// answers_env — a name → environment-variable-name map, resolved at run time
	// and merged into answers (lowest precedence). The environment-configurable
	// path: nothing machine-specific is committed.
	answers_env?: {[string]: string} @go(AnswersEnv)
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
	"ocr" |
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
	// installer (mutating: it drives keyboard input over a recipe)
	"install" |
	// console terminal session (mutating: opens a terminal and drives input)
	"open-terminal" |
	"run-command" |
	"close-terminal" |
	// LUKS/disk-encryption passphrase entry at the initramfs prompt (mutating)
	"luks-unlock" |
	// console FLOW: continuous OCR-until-condition with named outcomes + control
	// flow (if/then/else, case/switch, bounded while) (mutating)
	"flow" |
	// boot order: set the UEFI boot order from inside the running system via the
	// EFI boot manager (efibootmgr) over the terminal (mutating)
	"boot-order" |
	// power (mutating)
	"power" |
	"dc-power" |
	"reboot" |
	"wol" |
	// wake the controlled host via the USB HID wake report (upstream's
	// wakeHost RPC) — the proper wake path when the host is display-asleep
	// (DPMS) rather than powered off; `wol` is for the powered-off case.
	"wake-host" |
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
	// lifecycle (never autonomous — refused before dispatch)
	"update" |
	// raw escape hatch (mutating: it can reach any device method)
	"rpc"

// #JetkvmMacroStep — one step of a keyboard macro.
#JetkvmMacroStep: {
	keys?:      [...string]
	modifiers?: [...string]
	delay?:     int & >=0
}
