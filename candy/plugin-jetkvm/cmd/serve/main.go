// Command serve is the OUT-OF-PROCESS entrypoint for the jetkvm plugin: charly's
// loader host-builds this binary and connects it over go-plugin gRPC via
// LocalTransport when the candy is NOT compiled into charly.
//
// The body is three lines by design — sdk.Serve owns the handshake, the
// schema-over-Describe transport, and the Invoke loop, so the provider package
// carries all the behaviour and compiles identically in either placement.
package main

import (
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm"
	"github.com/opencharly/sdk"
)

func main() {
	sdk.Serve(jetkvm.NewProvider(), jetkvm.NewMeta())
}
