package jetkvm

// provider.go is the out-of-process jetkvm verb provider — charly's host
// dispatches a `jetkvm:` check step to it through the registry
// (ResolveVerb("jetkvm") -> this grpcProvider -> Provider.Invoke) with the FULL
// #Op marshaled as params_json and a CheckEnv snapshot as env.
//
// Because the out-of-process path runs NO host-side matcher pipeline, this
// Invoke OWNS the whole verdict: it decodes the typed input, resolves the
// device address + credentials (the input, or the CheckEnv the host shipped),
// runs the method, and evaluates the stdout/stderr/exit_status matchers and the
// artifact validators itself — via the SHARED sdk implementation (R3), never a
// second copy.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/kit"
	pb "github.com/opencharly/spec/proto"
	"github.com/opencharly/spec/spec"
)

// jetkvmEnv is the plugin-side decode of the CheckEnv the host ships as
// Operation.Env for a `jetkvm:` check step. Mode distinguishes a live
// deployment from `charly check box` (where no device is reachable). Host is
// the deployment's resolved address when a deploy supplied one, so an authored
// step need not repeat it.
type jetkvmEnv struct {
	Box  string `json:"box"`
	Mode string `json:"mode"` // "live" | "box"
	Host string `json:"host"`
}

type provider struct{ pb.UnimplementedProviderServer }

// Invoke runs one `jetkvm:` operation.
func (provider) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeReply, error) {
	var op spec.Op
	if len(req.GetParamsJson()) > 0 {
		if err := json.Unmarshal(req.GetParamsJson(), &op); err != nil {
			return sdk.ResultJSON("fail", "jetkvm: decode op: "+err.Error())
		}
	}
	var in params.JetkvmInput
	kit.DecodeInput(op.PluginInput, &in)
	var env jetkvmEnv
	if len(req.GetEnvJson()) > 0 {
		_ = json.Unmarshal(req.GetEnvJson(), &env)
	}
	method := in.Method

	// Live-device verb: skip under `charly check box` (a disposable container
	// has no JetKVM attached) — mirrors every other live-verb plugin's box-mode
	// skip, so a plan carrying `jetkvm:` steps still passes the build check.
	if env.Mode == "box" {
		return sdk.ResultJSON("skip", fmt.Sprintf("jetkvm: %s requires a live device (skip under charly check box)", method))
	}

	// Resolve the device address: the authored host first, then the deploy
	// venue's address when the step omitted it (the "connect to whatever this
	// deployment is" shape). No address at all is the honest no-context skip.
	host := in.Host
	if host == "" {
		host = env.Host
	}
	if host == "" {
		return sdk.ResultJSON("skip", fmt.Sprintf("jetkvm: %s has no device address (author `host:`, or run against a deploy that supplies one; box=%q)", method, env.Box))
	}

	password, err := resolveSecret(ctx, req.GetExecutorBrokerId(), in.Password, in.PasswordSecret)
	if err != nil {
		return sdk.ResultJSON("fail", "jetkvm: "+err.Error())
	}
	authToken := resolveAuthToken(ctx, req.GetExecutorBrokerId(), in.AuthToken)

	out, runErr := dispatch(ctx, &op, &in, host, password, authToken)

	// A policy skip is not a failure: the method was well-formed but the
	// device-safety gate declined. Report it as the documented skip.
	if se, ok := isSkip(runErr); ok {
		return sdk.ResultJSON("skip", se.reason)
	}
	// An RPC method the firmware does not implement is absence of capability,
	// not a broken deployment — skip with the reason rather than fail.
	if isMethodNotFound(runErr) {
		return sdk.ResultJSON("skip", fmt.Sprintf("jetkvm: %s: the device firmware does not implement this method", method))
	}

	// The shared exit/stdout/stderr verdict pipeline (R3). screenshot is
	// jetkvm's one artifact-producing method.
	return sdk.VerbVerdict("jetkvm", string(method), out, runErr, &op, string(method) == "screenshot")
}

// isMethodNotFound reports whether the device rejected the call because the
// firmware has no such JSON-RPC method (JSON-RPC error code -32601). The
// vendored client exposes the code as a typed *kvmclient.RPCError, so the
// classification is a type assertion rather than a parse of the message text.
func isMethodNotFound(err error) bool {
	var re *kvmclient.RPCError
	if !errors.As(err, &re) || re.Code == nil {
		return false
	}
	return *re.Code == jsonrpcMethodNotFound
}

// jsonrpcMethodNotFound is the JSON-RPC 2.0 "method not found" error code the
// device returns for a method its firmware does not implement.
const jsonrpcMethodNotFound = -32601
