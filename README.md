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
`check-media-url`, `rpc`.

Mutating (need `allow_control: true`): `key`, `type`, `key-combo`, `macro`,
`click`, `mouse`, `move`, `scroll`, `drag`, `power`, `dc-power`, `reboot`, `wol`,
`virtual-media`, `usb-device`, `usb-emulation`, `set-*`, `renew-dhcp`.

Never autonomous: `factory-reset`, `update`.

Credentials: prefer `password_secret:` (the `verb:credential` store) or the
`JETKVM_AUTH_TOKEN` / `JETKVM_PASSWORD` environment variables. Never commit a
device password to a `charly.yml`.

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
