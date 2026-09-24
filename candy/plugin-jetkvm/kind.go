package jetkvm

// kind.go implements the plugin's `kind: jetkvm` DEVICE entity: its OpLoad
// decode, and the resolution of a named entity's console recipe over the reverse
// channel.
//
// WHY THE VERB, NOT A SEPARATE COMMAND, RESOLVES THE ENTITY (RDD, measured):
// a `run:`/`check:` plugin-verb step carries a CheckEnv + a reverse-channel
// broker id, so the out-of-process provider can self-load the project
// (`loaderkit.LoadUnifiedViaExecutor`) and read `uf.PluginKinds["jetkvm"]`. A
// `command:` plugin dispatched out-of-process is syscall.Exec'd with no
// handshake cookie and NO broker, so it cannot reach the loader — which is why
// the resolution lives HERE and the CLI surface (when added) would have to be
// compiled-in. The RDD proof: the verb loaded the project and resolved a
// kind:jetkvm entity (`pluginkinds=[... jetkvm ...]`, `jetkvm-entities=1`).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/sdk/loaderkit"
	"github.com/opencharly/spec/spec"
)

// kindWord is the `kind:` discriminator this plugin serves.
const kindWord = "jetkvm"

// deviceEntity is the typed decode of a `kind: jetkvm` entity body — the
// authored device + recipe configuration. The recipe fields are plain Go data
// passed to the shared sdk/kit console engine (SelectRecipe / MergeAnswers); the
// kit holds no wire type, because the AUTHORED shape is this plugin's own
// CUE-sourced schema (SDD).
type deviceEntity struct {
	Host           string           `json:"host,omitempty"`
	Insecure       bool             `json:"insecure,omitempty"`
	PasswordSecret string           `json:"password_secret,omitempty"`
	Description    string           `json:"description,omitempty"`
	Installer      *installerRecipe `json:"installer,omitempty"`
}

// installerRecipe is the CONSOLE-RECIPE bundle the entity carries, decoded from
// its CUE-sourced #JetkvmInstaller. Its fields are plain Go data passed to the
// shared sdk/kit engine's SelectRecipe / MergeAnswers (no wire type lives in the
// kit — the authored shape is the plugin's own CUE schema, SDD).
type installerRecipe struct {
	Recipes       map[string][]params.JetkvmInstallStep `json:"recipes,omitempty"`
	Steps         []params.JetkvmInstallStep            `json:"steps,omitempty"`
	Answers       map[string]string                     `json:"answers,omitempty"`
	AnswerSecrets map[string]string                     `json:"answer_secrets,omitempty"`
	AnswersEnv    map[string]string                     `json:"answers_env,omitempty"`
}

// defaultRecipeName is the recipe a step drives when it authors no `recipe:`.
const defaultRecipeName = "install"

// projectDir resolves the project directory over the reverse channel — the
// canonical "deploy-plugins-connect" host seam (os.Getwd host-side), the same
// preamble candy/plugin-kube uses before feeding the plugin-side self-load
// helpers.
func projectDir(ctx context.Context, ex *sdk.Executor, name string) (string, error) {
	reqJSON, err := json.Marshal(spec.DeployPluginsConnectRequest{Path: name})
	if err != nil {
		return "", err
	}
	out, err := ex.HostBuild(ctx, "deploy-plugins-connect", reqJSON)
	if err != nil {
		return "", err
	}
	var reply spec.DeployPluginsConnectReply
	if err := json.Unmarshal(out, &reply); err != nil {
		return "", fmt.Errorf("jetkvm: decode deploy-plugins-connect reply: %w", err)
	}
	return reply.Dir, nil
}

// resolveDeviceEntity loads the project out-of-process and returns the decoded
// `kind: jetkvm` entity named name. An absent entity is a clear error, never a
// silent zero value.
func resolveDeviceEntity(ctx context.Context, ex *sdk.Executor, name string) (*deviceEntity, error) {
	if ex == nil {
		return nil, fmt.Errorf("jetkvm: resolving device entity %q needs a host reverse channel (run it inside a deploy/check step, not a bare command)", name)
	}
	dir, err := projectDir(ctx, ex, name)
	if err != nil {
		return nil, fmt.Errorf("jetkvm: resolving device entity %q: %w", name, err)
	}
	uf, ok, err := loaderkit.LoadUnifiedViaExecutor(ctx, ex, dir)
	if err != nil {
		return nil, fmt.Errorf("jetkvm: loading project for device entity %q: %w", name, err)
	}
	if !ok || uf == nil {
		return nil, fmt.Errorf("jetkvm: resolving device entity %q: no charly.yml loaded from %s", name, dir)
	}
	body, found := loaderkit.ResolveKindEntityBody(uf, kindWord, name)
	if !found {
		return nil, fmt.Errorf("jetkvm: no kind:jetkvm entity named %q in %s", name, dir)
	}
	var dev deviceEntity
	if err := json.Unmarshal(body, &dev); err != nil {
		return nil, fmt.Errorf("jetkvm: decoding kind:jetkvm entity %q: %w", name, err)
	}
	return &dev, nil
}

// applyDeviceEntity fills an install step's recipe/answers from a referenced
// device entity. Authored inline `steps:`/`answers:` on the step WIN — the
// entity is the default, so a bed can override one field without restating the
// whole recipe.
//
// Answers come from THREE sources, merged lowest-to-highest by the shared
// kit.ConsoleRecipe.MergeAnswers: `answers_env` (host environment variables,
// the fully-environment-configurable path), `answer_secrets` (the credential
// store, for secrets that must never land in charly.yml), then authored
// `answers`.
func applyDeviceEntity(ctx context.Context, ex *sdk.Executor, brokerID uint32, in *params.JetkvmInput) error {
	if in.Device == "" {
		return nil
	}
	dev, err := resolveDeviceEntity(ctx, ex, in.Device)
	if err != nil {
		return err
	}
	if dev.Installer != nil {
		if len(in.Steps) == 0 {
			recipeName := in.Recipe
			if recipeName == "" {
				recipeName = defaultRecipeName
			}
			steps, rerr := kit.SelectRecipe(paramsRecipesToKit(dev.Installer.Recipes), paramsStepsToKit(dev.Installer.Steps), recipeName)
			if rerr != nil {
				return fmt.Errorf("jetkvm: device %q: %w", in.Device, rerr)
			}
			in.Steps = consoleStepsToParams(steps)
		}
		// Entity-level answers, then the step's authored answers win.
		merged := kit.MergeAnswers(dev.Installer.AnswersEnv, dev.Installer.AnswerSecrets, dev.Installer.Answers,
			os.Getenv, func(key string) string {
				return credentialLookup(ctx, brokerID, key)
			})
		for name, v := range in.Answers {
			merged[name] = v
		}
		if len(merged) > 0 {
			in.Answers = merged
		}
	}
	// Device connection fields are also entity-supplied defaults: an authored
	// step field wins, so a bed can point at a device by entity and still
	// override the host.
	if in.Host == "" {
		in.Host = dev.Host
	}
	if !in.Insecure && dev.Insecure {
		in.Insecure = true
	}
	if in.PasswordSecret == "" {
		in.PasswordSecret = dev.PasswordSecret
	}
	return nil
}

// paramsRecipesToKit converts a map of named schema-generated recipes into the
// shared engine's neutral step form.
func paramsRecipesToKit(recipes map[string][]params.JetkvmInstallStep) map[string][]kit.ConsoleStep {
	if len(recipes) == 0 {
		return nil
	}
	out := make(map[string][]kit.ConsoleStep, len(recipes))
	for name, steps := range recipes {
		out[name] = paramsStepsToKit(steps)
	}
	return out
}

// paramsStepsToKit converts the schema-generated param steps into the shared
// engine's neutral step type.
func paramsStepsToKit(steps []params.JetkvmInstallStep) []kit.ConsoleStep {
	out := make([]kit.ConsoleStep, 0, len(steps))
	for _, s := range steps {
		out = append(out, kit.ConsoleStep{
			WaitFor: s.WaitFor, Action: s.Action, Key: s.KeyName, Combo: s.Combo,
			Text: s.Text, TimeoutSec: s.TimeoutSec, Optional: s.Optional,
			Artifact: s.Artifact, Description: s.Description,
		})
	}
	return out
}

// consoleStepsToParams converts the shared kit recipe steps into the verb's
// decoded param steps (the schema-generated type).
func consoleStepsToParams(steps []kit.ConsoleStep) []params.JetkvmInstallStep {
	out := make([]params.JetkvmInstallStep, 0, len(steps))
	for _, s := range steps {
		out = append(out, params.JetkvmInstallStep{
			WaitFor:     s.WaitFor,
			Action:      s.Action,
			KeyName:     s.Key,
			Combo:       s.Combo,
			Text:        s.Text,
			TimeoutSec:  s.TimeoutSec,
			Optional:    s.Optional,
			Artifact:    s.Artifact,
			Description: s.Description,
		})
	}
	return out
}

// kindCapability advertises the plugin's kind:jetkvm capability — the device
// entity's self-contained CUE schema, served over the same Describe channel as
// the verb.
func kindCapability() sdk.ProvidedCapability {
	return sdk.ProvidedCapability{
		Class:    "kind",
		Word:     kindWord,
		InputDef: "#JetkvmDeviceInput",
	}
}

// deviceCanonicalJSON is the OpLoad body decode: the host validates the authored
// entity against #JetkvmDeviceInput, then this re-marshals it canonically so it
// lands in uf.PluginKinds["jetkvm"][name].
func deviceCanonicalJSON(paramsJSON []byte) (json.RawMessage, error) {
	var dev deviceEntity
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &dev); err != nil {
			return nil, fmt.Errorf("jetkvm: decode device entity: %w", err)
		}
	}
	out, err := json.Marshal(dev)
	if err != nil {
		return nil, fmt.Errorf("jetkvm: marshal device entity: %w", err)
	}
	return out, nil
}

// hasSteps reports whether an install step has a usable recipe (inline or via a
// referenced device entity) — used to fail fast with a clear message.
func hasSteps(in *params.JetkvmInput) bool {
	return len(in.Steps) > 0 || strings.TrimSpace(in.Device) != ""
}
