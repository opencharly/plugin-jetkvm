# AGENTS.md — plugin-jetkvm

Standalone out-of-tree plugin repo for the JetKVM IP-KVM surface (`verb:jetkvm`
+ `kind:jetkvm`). The plugin is a Go module at `candy/plugin-jetkvm/` (module
path `github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm`); the root
`charly.yml` declares `discover: candy` and the repo's own disposable beds.

Canonical files:

- `candy/plugin-jetkvm/charly.yml` — the `plugin-jetkvm:` candy entity
  (`plugin:` block, `plan:` checks).
- `candy/plugin-jetkvm/` — the Go source: `plugin.go`, `provider.go`,
  `catalog.go`, `methods.go`, `session.go`, `install.go`, `ocr.go`,
  `credential.go`, `kind.go`, `schema/jetkvm.cue`, `params/cue_types_gen.go`,
  `internal/kvmclient/`, `cmd/serve/main.go`.
- `charly.yml` — the root manifest (`discover: candy`) + the disposable beds.
- `.github/workflows/ci.yml` — the repo's Go gates.
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.
- `third_party/NOTICE` — the vendored client provenance.
- `README.md` — user overview only; never agent guidance.

## Load these skills first (R0)

- `/charly-check:jetkvm` — the `jetkvm:` verb + `kind: jetkvm` device entity
  reference (the plugin's user-facing surface). Load before changing a method or
  its input schema.
- `/charly-internals:plugin` — the plugin authoring reference: the `plugin:`
  block, the unified Provider model, the per-plugin CUE-schema contract, the
  `verb` + `kind` classes. Load before touching the provider or schema.
- `/charly-check:check` — the declarative check-step surface the `jetkvm:` verb
  is authored through.
- `/charly-internals:git-workflow` — before any git/PR action.

## Build / validate / test

- `go build ./...` in `candy/plugin-jetkvm/` — compile the plugin module.
- `go test ./...` in `candy/plugin-jetkvm/` — the plugin's fake-device tests.
- `gofmt -l .` and `golangci-lint run ./...` in `candy/plugin-jetkvm/` — the
  repo's `.github/workflows/ci.yml` Go gates.
- `charly box validate` at the repo root — the structural check (the candy +
  `plugin:` block, CUE schema).
- The merge gate is the **org-wide** `charly/pr-validator` (required check
  `validate / validate`, defined in `opencharly/.github`); this repo has **no**
  per-repo candy gate.
- The R10 witness is the repo's own disposable beds in the root `charly.yml`
  (`jetkvm-verb-probe` is the registry-dispatch bed; the device beds are
  env-driven and read-only).

## Modify this repo

- Edit the `plugin-jetkvm:` candy entity, the Go source, and `schema/jetkvm.cue`
  **together** — the schema is the single source for the verb's `params/` struct
  and the authoritative method catalog (`#JetkvmMethod`).
- Every method the schema allows MUST be classified in `methods.go` (read-only
  allowlist, never-autonomous set, everything else mutating). A new method is
  mutating-by-default.
- A JetKVM is a physical, NON-disposable appliance: keep `allow_control` gating
  on every mutating path and never exercise `factory-reset`/`update` from a bed.

## Landing

- The authoritative rulebook is the umbrella `AGENTS.md` in
  `opencharly/opencharly` and `charly/AGENTS.md` in the charly repo. Load
  `/charly-internals:git-workflow` before any git/PR action; history lives in
  `CHANGELOG/`. Do not restate its rules here.
