# plugin-jetkvm

The OpenCharly plugin for **JetKVM** — drive a JetKVM IP-KVM from charly with no
browser.

`jetkvm` is a declarative check/control **verb** served out-of-process by this
plugin, exactly like `vnc:` / `adb:` / `cdp:`. Author it inside a candy or box
`plan:` and run it against a live deployment with `charly check live`, or run the
repo's own disposable beds with `charly check run`.

```yaml
- check: the JetKVM is reachable and its control channel answers
  context: [runtime]
  jetkvm:
      method: status
      host: jk.example.ts.net
  stdout:
      - contains: "rpc:       true"

- check: a fresh framebuffer is captured as a real PNG
  context: [runtime]
  jetkvm:
      method: screenshot
      host: jk.example.ts.net
      artifact: /tmp/kvm.png
      artifact_min_bytes: 20000
```

## Read-only by default

A JetKVM is a physical appliance wired into a real machine's keyboard, video and
power. It is **not disposable**, so every mutating method (input, power, virtual
media, USB, config writes) requires `allow_control: true`. Without it the method
reports a documented `skip` naming the gate rather than acting. `factory-reset`
and `update` are refused even with `allow_control`, because no unattended plan
may wipe or reimage a device.

The classification is an **allowlist**, so a method added later is
mutating-by-default and cannot touch a device until it is deliberately
classified.

## How it talks to the device

Over JetKVM's own control plane: local login (`POST /auth/login-local`) → the
signaling websocket (`/webrtc/signaling/client`) → the `rpc` JSON-RPC 2.0 data
channel and the binary `hidrpc` input channel, with the H.264 video track decoded
to PNG by `ffmpeg` (required on the host).

The WebRTC/HID client is **vendored** from two MIT/BSD reference implementations
under `internal/kvmclient` and rides the same `github.com/pion/webrtc/v4` stack
the JetKVM firmware uses — see [`third_party/NOTICE`](third_party/NOTICE) for the
exact provenance and licenses. The session, artifact, credential and verdict
plumbing is charly's own SDK surface, never a second implementation.

## Methods

Read-only: `status`, `screenshot`, `version`, `video-state`, `usb-state`,
`atx-state`, `dc-state`, `virtual-media-state`, `storage-files`, `wol-devices`,
`macros`, `keyboard-layout`, `timezones`, `cloud-state`, `network-state`,
`network-settings`, `tailscale-status`, `update-status`, `devmode-state`,
`ssh-key`, `tls-state`, `extensions`, `public-ip`, `diagnostics`,
`check-media-url`, `usb-config`.

Mutating (need `allow_control: true`): `key`, `type`, `key-combo`, `macro`,
`click`, `mouse`, `move`, `scroll`, `drag`, `power`, `dc-power`, `reboot`, `wol`,
`virtual-media`, `usb-device`, `usb-emulation`, `set-settings`, `set-edid`,
`set-video`, `set-display`, `set-audio`, `set-network`, `set-tailscale`,
`set-devmode`, `set-ssh-key`, `set-tls`, `set-keyboard-layout`, `set-macros`,
`set-jiggler`, `set-extension`, `set-wol-devices`, `set-log-level`,
`renew-dhcp`, `rpc`.

`rpc` is MUTATING, deliberately: it can invoke ANY device JSON-RPC method, so an
ungated escape hatch would be a bypass of the safety gate. It requires
`allow_control: true`, and even then refuses the irreversible reimaging methods
(`factoryReset`, `tryUpdate`, `tryUpdateComponents`).

The authoritative catalog is `#JetkvmMethod` in `schema/jetkvm.cue`. Every
method it allows is classified in `methods.go`: the read-only allowlist
(`readOnlyMethods`), the never-autonomous set (`neverAutonomous`), and
everything else as mutating. `dispatch` applies that classification BEFORE
`runMethod` in `catalog.go` routes the method, which is why `factory-reset`
and `update` appear in the schema catalog and the refusal lists but have no
`runMethod` case — they can never reach it.

Never autonomous: `factory-reset`, `update`.

Credentials: prefer `password_secret:` (the `verb:credential` store) or the
`JETKVM_AUTH_TOKEN` / `JETKVM_PASSWORD` environment variables. Never commit a
device password to a `charly.yml`.

Device address: author `host:` for an explicit device, or set **`JETKVM_HOST`**
and author none — the provider falls back to the environment before the deploy
venue. Using `JETKVM_HOST` keeps a device-specific hostname out of a committed
plan, so a bed can be portable and carry no tailnet name.

Input semantics: `move` and `mouse` are **pure position moves** — they never
press a button, whether or not a `button:` is authored. `click` and `drag` press
the button, which defaults to `left` when no `button:` is authored. So `move` and
`click` need no `button:`.

## Development

```sh
cd candy/plugin-jetkvm
gofmt -l .
go vet ./...
go test ./...
golangci-lint run ./...
```

From the repo root, the candy is gated by the real charly:

```sh
charly box validate
```
