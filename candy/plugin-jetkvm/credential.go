package jetkvm

// credential.go resolves the device password without a second credential store.
//
// It reaches verb:credential (candy/plugin-secrets) directly, PEER-TO-PEER over
// the SDK's InvokeProvider reverse leg — the SAME pattern candy/plugin-adb and
// candy/plugin-vm use — rather than having the host pre-resolve it behind a
// core seam. Resolution order:
//
//  1. the authored literal `password:` (ad-hoc/CI use);
//  2. the credential store, via `password_secret:` (or the JETKVM_PASSWORD
//     default key), which is where a real device password belongs;
//  3. the JETKVM_PASSWORD environment variable, for a CI runner that has no
//     store.
//
// A device in noPassword mode legitimately resolves to the empty string, so a
// miss is NOT an error here; the connection attempt is what reports an actual
// authentication failure.

import (
	"context"
	"encoding/json"
	"os"

	"github.com/opencharly/spec/exec"
	"github.com/opencharly/spec/ops"
)

// defaultPasswordKey is the credential-store key consulted when the author did
// not name one.
const defaultPasswordKey = "JETKVM_PASSWORD"

// defaultAuthTokenKey is the env var (and credential-store key) consulted for an
// already-valid authToken session value. A JetKVM in password mode mints a fresh
// authToken per login, so a deployment that already holds one should prefer it
// over embedding the device password — and either way the value belongs in the
// credential store or the environment, never in a committed charly.yml.
const defaultAuthTokenKey = "JETKVM_AUTH_TOKEN"

// resolveAuthToken returns the authored token, else the credential store, else
// the environment. Empty is legitimate (the device is in noPassword mode).
func resolveAuthToken(ctx context.Context, brokerID uint32, literal string) string {
	if literal != "" {
		return literal
	}
	if v := credentialLookup(ctx, brokerID, defaultAuthTokenKey); v != "" {
		return v
	}
	return os.Getenv(defaultAuthTokenKey)
}

// credentialService is the service name verb:credential stores charly secrets
// under (byte-identical to the core adapter and every other consumer).
const credentialService = "charly/secret"

// credentialGetInput / credentialGetReply mirror verb:credential's `get` wire
// shape. The cross-module contract carries no shared Go type, so each consumer
// keeps a JSON-tag-compatible mirror, exactly as the core adapter does.
type credentialGetInput struct {
	Method  string `json:"method"`
	Service string `json:"service,omitempty"`
	Key     string `json:"key,omitempty"`
}

type credentialGetReply struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// resolveSecret returns the device password from the first source that has one.
// brokerID is the reverse-channel broker the host threaded onto this Invoke
// (zero when there is none, in which case only the literal and env sources are
// consulted).
func resolveSecret(ctx context.Context, brokerID uint32, literal, secretKey string) (string, error) {
	if literal != "" {
		return literal, nil
	}
	key := secretKey
	if key == "" {
		key = defaultPasswordKey
	}
	if v := credentialLookup(ctx, brokerID, key); v != "" {
		return v, nil
	}
	if v := os.Getenv(defaultPasswordKey); v != "" {
		return v, nil
	}
	// Empty is legitimate (noPassword mode / no source configured): the connect
	// attempt reports a real auth failure if the device actually needs one.
	return "", nil
}

// credentialLookup fetches one key from verb:credential over the peer reverse
// leg. A missing key, an absent store, or a missing broker all resolve to the
// empty string (the key is optional), never a hard error.
func credentialLookup(ctx context.Context, brokerID uint32, key string) string {
	if key == "" || brokerID == 0 {
		return ""
	}
	ex, err := exec.ExecutorForInvoke(ctx, brokerID)
	if err != nil || ex == nil {
		return ""
	}
	payload, err := json.Marshal(credentialGetInput{Method: "get", Service: credentialService, Key: key})
	if err != nil {
		return ""
	}
	out, err := ex.InvokeProvider(ctx, "verb", "credential", ops.OpRun, payload, nil, ops.InvokeProviderOpts{})
	if err != nil || len(out) == 0 {
		return ""
	}
	var reply credentialGetReply
	if err := json.Unmarshal(out, &reply); err != nil {
		return ""
	}
	return reply.Value
}
