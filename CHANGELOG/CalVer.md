# <CalVer> — feat: the jetkvm plugin — drive a JetKVM IP-KVM from charly

## Summary

Adds `opencharly/plugin-jetkvm`, a new out-of-tree charly plugin serving the
`jetkvm` IP-KVM verb. It drives a JetKVM device (https://jetkvm.com) with no
browser, over JetKVM's own WebRTC control plane — local login → signaling
websocket → the `rpc` JSON-RPC 2.0 data channel, the binary `hidrpc` input
channel, and the H.264 video track decoded to a PNG by ffmpeg — and is served
out-of-process over go-plugin gRPC via the charly plugin SDK, exactly like
`plugin-vnc` / `plugin-adb`.

- **New repo, scaffolded from the plugin pattern**: MIT `LICENSE`, the two
  org-wide workflow dispatchers (`pr-validator.yml`, `tag-on-merge.yml`), the
  `candy` gate (`deploy.yml`, pinned to charly `v2026.258.0219`), and a Go CI
  gate (`ci.yml`) — the last is new here because, unlike the thin stock plugin
  candies, this repo ships a real Go module with a vendored device client.
- **Root `charly.yml`** declares `discover: candy` so the repo is a project and
  `charly box validate` actually gates the candy instead of exiting 0 on any
  manifest.
- **`verb:jetkvm`** with its self-contained `#JetkvmInput` CUE schema
  (`schema/jetkvm.cue`), generated params (SDD), and a method catalog covering
  observation, HID input, power/ATX/DC, virtual media, USB gadget, device
  config, and a raw JSON-RPC escape hatch.
- **READ-ONLY BY DEFAULT.** A JetKVM is a physical appliance wired into a real
  machine's keyboard, video and power, so it is not disposable. Every mutating
  method requires `allow_control: true`; `factory-reset` and `update` are
  refused even then, because no unattended plan may wipe or reimage a device.
  The classification is an allowlist, so an unclassified method is
  mutating-by-default.
- **Client reuse, not reinvention.** The WebRTC/HID wire is vendored from
  `LeeroyDing/jetkvm-mcp` (MIT) and `conallob/mcp-jetkvm` (BSD-3) under
  `internal/kvmclient` (see `third_party/NOTICE`) and rides the same
  `github.com/pion/webrtc/v4` stack the JetKVM firmware itself uses. The
  session/artifact/credential plumbing is charly's own (`sdk.VerbVerdict`,
  `sdk.RunArtifactValidators`, `sdk.CheckRequiredModifiers`, `verb:credential`
  over the reverse channel) — no second implementation.
- **Tests that can fail**: 41 vendored upstream client tests plus 14 new
  provider tests that drive the real `Invoke` path against an in-process fake
  JetKVM device (`internal/fakedevice`, vendored), including the device-safety
  gate, the never-autonomous refusal, required-modifier enforcement, and URL
  normalization.

### RCA findings fixed in this change (R1)

- **Bare-hostname contract was documented but not implemented.** The provider's
  `host` accepts a bare hostname or `host:port`, but the vendored client
  requires an explicit scheme, so the first live bed failed with
  `device URL scheme must be http or https`. Fixed with
  `kvmclient.NormalizeDeviceURL` (plaintext default, explicit scheme preserved)
  and `TestNormalizeDeviceURL`.
- **A `getVideoState` screenshot pre-flight was a false negative.** Measured
  against firmware 0.5.9: `ready` describes whether the device's native capture
  *pipeline* is running (started per WebRTC session, stopped when idle) — not
  whether a signal is available. The device reports `ready:false,
  error:no_signal` while screenshots succeed. The gate rejected every real
  capture; it is removed and `CaptureScreenshot` is the authority, with the
  measured reasoning recorded in a comment.
- **`insecure: true` was a dead flag.** A JetKVM ships a self-signed
  certificate, so the option must actually work; it is now wired through both
  the HTTP client and the signaling websocket dial, with a test proving the flag
  is not a no-op.

## How tested

- `gofmt -l .` → empty; `go vet ./...` → clean; `golangci-lint run ./...` → 0
  issues; `go test ./...` → ok. The lint config excludes only the vendored
  paths, proven by injecting a deliberate defect in an authored file and
  observing it reported.
- `charly box validate` → `OK — checked 1 candy, 2 deploys, 7 distros, 7
  builders; 0 warnings, 0 errors`.
- `charly check run jetkvm-verb-probe` (disposable local bed) → `PASS (steps=6)`.
- `charly check run jetkvm-device-readonly` (LIVE device, read-only) → `PASS
  (steps=6)`, 29s, 0 errors / 0 warnings; `check-live` 2 passed / 0 failed / 0
  skipped; the screenshot step produced a real 1920x1080 8-bit RGB PNG of
  106461 bytes (verified with `file(1)`), not merely a returned call.
