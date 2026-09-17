// Package jetkvm is the charly plugin serving the `jetkvm` IP-KVM verb — an
// importable root package plus its own go.mod.
//
// It drives a JetKVM device without a browser, over JetKVM's own WebRTC control
// plane: local login (POST /auth/login-local) -> the signaling websocket
// (/webrtc/signaling/client) -> the "rpc" JSON-RPC 2.0 data channel, the binary
// "hidrpc" input channel, and the H.264 video track decoded to PNG by ffmpeg.
//
// The host go-builds this binary and serves it OUT-OF-PROCESS over go-plugin
// gRPC via the charly plugin SDK, so the `jetkvm:` verb dispatches through the
// provider registry exactly like a built-in.
//
// Dual-placement by construction: the SAME NewProvider()/NewMeta() compile INTO
// charly in-process when listed in compiled_plugins, or cmd/serve serves them
// OUT-OF-PROCESS when they are not — placement is invisible above the registry.
//
// READ-ONLY BY DEFAULT: every mutating method requires allow_control: true (see
// methods.go). The device is a physical appliance and is not disposable.
package jetkvm

import (
	"embed"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
)

//go:embed schema/*.cue
var schemaFS embed.FS

// NewProvider returns the jetkvm provider.
func NewProvider() pb.ProviderServer { return &provider{} }

// NewMeta advertises verb:jetkvm + the plugin's self-contained CUE schema via
// sdk.NewMeta -> BuildCapabilities. The verb's entire authoring contract — the
// #JetkvmMethod catalog plus every jetkvm-exclusive modifier — lives in the
// served #JetkvmInput (schema/jetkvm.cue), which the host splices onto the base
// and validates every authored `jetkvm:` step's plugin_input against.
func NewMeta() pb.PluginMetaServer {
	return sdk.NewMeta("2026.258.1200",
		[]sdk.ProvidedCapability{{
			Class:    "verb",
			Word:     "jetkvm",
			InputDef: "#JetkvmInput",
		}},
		schemaFS)
}
