# plugin-jetkvm

The OpenCharly plugin for **JetKVM** — drive a [JetKVM](https://jetkvm.com)
IP-KVM from charly with no browser.

`jetkvm` is a declarative check/control **verb** served out-of-process by this
plugin, exactly like `vnc:` / `adb:` / `cdp:`. It also serves a
`kind: jetkvm` device entity. Author it inside a candy or box `plan:` and run it
against a live deployment with `charly check live`, or run the repo's own
disposable beds with `charly check run`.

## What it provides

| Capability | Surface |
|---|---|
| `verb:jetkvm` | the declarative `jetkvm:` check/control step |
| `kind:jetkvm` | a `kind: jetkvm` device entity carrying an installer recipe |

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

Read-only: `status`, `screenshot`, `ocr`, `version`, `video-state`, `usb-state`,
`atx-state`, `dc-state`, `virtual-media-state`, `storage-files`, `wol-devices`,
`macros`, `keyboard-layout`, `timezones`, `cloud-state`, `network-state`,
`network-settings`, `tailscale-status`, `update-status`, `devmode-state`,
`ssh-key`, `tls-state`, `extensions`, `public-ip`, `diagnostics`,
`check-media-url`, `usb-config`.

Mutating (need `allow_control: true`): `key`, `type`, `key-combo`, `macro`,
`click`, `mouse`, `move`, `scroll`, `drag`, `install`, `power`, `dc-power`, `reboot`, `wol`,
`wake-host`, `virtual-media`, `usb-device`, `usb-emulation`, `set-settings`,
`set-edid`,
`set-video`, `set-display`, `set-audio`, `set-network`, `set-tailscale`,
`set-devmode`, `set-ssh-key`, `set-tls`, `set-keyboard-layout`, `set-macros`,
`set-jiggler`, `set-extension`, `set-wol-devices`, `set-log-level`,
`renew-dhcp`, `rpc`.

`wake-host` vs `wol`: `wake-host` sends the device's own USB HID wake report
(upstream's `wakeHost` RPC) and wakes a host that is **display-asleep** (DPMS);
`wol` sends a magic packet and only helps a host that is **powered off**.

`rpc` is MUTATING, deliberately: it can invoke ANY device JSON-RPC method, so an
ungated escape hatch would be a bypass of the safety gate. It requires
`allow_control: true`, and even then refuses the irreversible reimaging methods
(`factoryReset`, `tryUpdate`, `tryUpdateComponents`).

The authoritative catalog is `#JetkvmMethod` in `schema/jetkvm.cue`. Every
method it allows is classified in `methods.go`: the read-only allowlist
(`readOnlyMethods`), the never-autonomous set (`neverAutonomous`), and
everything else as mutating.

Never autonomous: `factory-reset`, `update`.

Credentials: prefer `password_secret:` (the `verb:credential` store) or the
`JETKVM_AUTH_TOKEN` / `JETKVM_PASSWORD` environment variables. Never commit a
device password to a `charly.yml`.

Device address: author `host:` for an explicit device, or set **`JETKVM_HOST`**
and author none — the provider falls back to the environment before the deploy
venue.

Pointer coordinates: `x`/`y` (and `from_x`/`from_y`) are **absolute HID pointer
coordinates in `[0,32767]`**, not desktop pixels. The centre of a 1920×1080
screen is about `(16384,16384)`.

## Console installer

Beyond raw input, the plugin serves two higher-level capabilities: the read-only
`ocr` method (capture + tesseract on the host + assert text — the
wait-for-screen primitive), and the mutating `install` method, a configurable
console-installer DRIVER that connects ONCE and walks an ordered `steps:` recipe,
OCR-waiting for each screen-unique anchor before sending its key/type/combo
input. The recipe is generic DATA supplied by a `kind: jetkvm` device entity, so
one plugin drives any text-console installer with no per-distro code.

## How to use it

Compose the plugin candy in a box or check bed's `candy:` list:

```yaml
- '@github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm:<tag>'
```

The repo's own disposable beds (`jetkvm-verb-probe`, `jetkvm-device-readonly`,
`jetkvm-input-probe`, `jetkvm-ocr-readonly`, `jetkvm-entity-probe`,
`jetkvm-console-session`) live in the root `charly.yml`.

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

## Layout

- `candy/plugin-jetkvm/` — the plugin module: `plugin.go`, `provider.go`,
  `catalog.go`, `methods.go`, `session.go`, `install.go`, `ocr.go`,
  `credential.go`, `kind.go`, `schema/jetkvm.cue`, `params/cue_types_gen.go`,
  `internal/kvmclient/`, `cmd/serve/main.go`.
- `candy/plugin-jetkvm/charly.yml` — the `plugin-jetkvm:` candy entity.
- `charly.yml` — the root project manifest (`discover: candy`) + the disposable
  beds.
- `.github/workflows/ci.yml` — the repo's Go gates (gofmt, golangci-lint, vet,
  test).
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.
- `third_party/NOTICE` — the vendored client provenance.

## Related

- Owning skill: `/charly-check:jetkvm` — the `jetkvm:` verb reference (the candy
  carries no `skill:` entity of its own; the gap is tracked in
  [opencharly/opencharly#291](https://github.com/opencharly/opencharly/issues/291)).
- `/charly-internals:plugin` — the plugin/provider model.
- [`opencharly/charly`](https://github.com/opencharly/charly) — the charly CLI.
