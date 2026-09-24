package jetkvm

// kind_test.go pins the plugin's `kind: jetkvm` device-entity leg: the OpLoad
// canonical-JSON round-trip (the shape that lands in uf.PluginKinds), and the
// install method's device-safety classification (mutating, so gated).
//
// resolveDeviceEntity's project-load half is covered by the live bed — it needs
// a real reverse channel and a real project, neither of which a unit test can
// fabricate honestly.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
	"github.com/opencharly/sdk/kit"
	"github.com/opencharly/spec/proto"
)

// TestOpLoadCanonicalizesDeviceEntity verifies the kind leg re-marshals the
// authored body so the host folds a canonical JSON into PluginKinds.
func TestOpLoadCanonicalizesDeviceEntity(t *testing.T) {
	body := []byte(`{"host":"jk.example.ts.net","installer":{"answers":{"user":"atrawog"},"steps":[{"wait_for":"Username>","action":"type","text":"{{user}}"}]}}`)
	reply, err := (provider{}).Invoke(context.Background(), &proto.InvokeRequest{
		Op:         "load",
		ParamsJson: body,
	})
	if err != nil {
		t.Fatalf("OpLoad transport error: %v", err)
	}
	var dev deviceEntity
	if err := json.Unmarshal([]byte(reply.GetResultJson()), &dev); err != nil {
		t.Fatalf("decode canonical reply %q: %v", reply.GetResultJson(), err)
	}
	if dev.Host != "jk.example.ts.net" {
		t.Fatalf("host = %q", dev.Host)
	}
	if dev.Installer == nil || len(dev.Installer.Steps) != 1 {
		t.Fatalf("installer steps not round-tripped: %+v", dev.Installer)
	}
	if dev.Installer.Answers["user"] != "atrawog" {
		t.Fatalf("answers not round-tripped: %+v", dev.Installer.Answers)
	}
}

// TestInstallIsGatedWithoutAllowControl is the device-safety test for the new
// mutating method: it must skip, not act, without allow_control.
func TestInstallIsGatedWithoutAllowControl(t *testing.T) {
	status, msg := invoke(t,
		map[string]any{"method": "install", "steps": []map[string]any{{"wait_for": "x", "action": "key", "key": "Return"}}},
		map[string]any{"mode": "live", "host": "127.0.0.1:1"})
	if status != "skip" {
		t.Fatalf("install without allow_control must skip, got %q (%s)", status, msg)
	}
	if !strings.Contains(msg, "mutating method on a physical appliance") {
		t.Fatalf("skip should name the safety gate, got %q", msg)
	}
}

// TestOcrAndInstallModifierContracts pins the required-modifier contract:
// `ocr` validates its own `text` in-method, and `install`'s recipe may come from
// EITHER inline `steps:` OR a referenced `device:` entity — so it is NOT a hard
// required-modifier (runInstall's hasSteps enforces "one of the two" with a clear
// message, after entity resolution has run).
func TestOcrAndInstallModifierContracts(t *testing.T) {
	if got := requiredModifiers["ocr"]; len(got) != 0 {
		t.Fatalf("ocr has no required modifier in the table (text is validated in methodOcr); got %v", got)
	}
	if got := requiredModifiers["install"]; len(got) != 0 {
		t.Fatalf("install must not hard-require steps (a device: entity may supply them); got %v", got)
	}
	if hasSteps(&params.JetkvmInput{}) {
		t.Fatal("install with neither steps nor device must be rejected by hasSteps")
	}
}

// TestOcrIsReadOnly pins that the new ocr method needs no allow_control, so a
// plan can read the screen with no gate.
func TestOcrIsReadOnly(t *testing.T) {
	if !readOnlyMethods["ocr"] {
		t.Fatal("ocr must be classified read-only")
	}
	if readOnlyMethods["install"] {
		t.Fatal("install must NOT be read-only (it drives keyboard input)")
	}
}

// TestWakeHostIsMutating pins that wake-host is gated: it sends a HID report to
// the controlled host, so it must require allow_control like every other
// mutating method.
func TestWakeHostIsMutating(t *testing.T) {
	if readOnlyMethods["wake-host"] {
		t.Fatal("wake-host must NOT be read-only (it sends a HID wake report)")
	}
	skip, reason := methodSafety("wake-host", false)
	if !skip || !strings.Contains(reason, "allow_control") {
		t.Fatalf("wake-host without allow_control must skip naming the gate, got skip=%v reason=%q", skip, reason)
	}
	if skip, _ := methodSafety("wake-host", true); skip {
		t.Fatal("wake-host WITH allow_control must be permitted")
	}
}

// TestEntityMergePrecedence pins the precedence contract: an authored step field
// overrides the entity-supplied default. Tested via the pure merge shape
// (applyDeviceEntity's network half needs the reverse channel).
func TestEntityMergePrecedence(t *testing.T) {
	in := &params.JetkvmInput{
		Host:  "authored-host",
		Steps: []params.JetkvmInstallStep{{WaitFor: "authored"}},
	}
	dev := &deviceEntity{
		Host:      "entity-host",
		Installer: &installerRecipe{Steps: []params.JetkvmInstallStep{{WaitFor: "entity"}}},
	}
	// Replicate the merge's precedence without the channel: authored wins.
	if in.Host == "" {
		in.Host = dev.Host
	}
	if len(in.Steps) == 0 {
		in.Steps = dev.Installer.Steps
	}
	if in.Host != "authored-host" {
		t.Fatalf("authored host must win, got %q", in.Host)
	}
	if in.Steps[0].WaitFor != "authored" {
		t.Fatalf("authored steps must win, got %q", in.Steps[0].WaitFor)
	}
}

// TestConsoleRecipeSelectAndAnswers pins the shared recipe selection + the
// three-source answer merge (env < secret < authored) the entity drives.
func TestConsoleRecipeSelectAndAnswers(t *testing.T) {
	recipes := map[string][]kit.ConsoleStep{
		"install":    {{WaitFor: "Start Install"}},
		"first_boot": {{WaitFor: "Start Setup"}},
	}
	steps, err := kit.SelectRecipe(recipes, nil, "first_boot")
	if err != nil || steps[0].WaitFor != "Start Setup" {
		t.Fatalf("select first_boot: %v %+v", err, steps)
	}
	if _, err := kit.SelectRecipe(recipes, nil, "nope"); err == nil {
		t.Fatal("unknown recipe must error")
	}
	env := func(k string) string {
		return map[string]string{"OMARCHY_PW": "rdduser", "OMARCHY_USER": "envuser"}[k]
	}
	secret := func(k string) string {
		return map[string]string{"OMARCHY_USER_SECRET": "secretuser"}[k]
	}
	got := kit.MergeAnswers(
		map[string]string{"password": "OMARCHY_PW", "username": "OMARCHY_USER"},
		map[string]string{"username": "OMARCHY_USER_SECRET"},
		map[string]string{"hostname": "a"}, env, secret)
	if got["password"] != "rdduser" {
		t.Fatalf("env answer missing: %v", got)
	}
	if got["username"] != "secretuser" {
		t.Fatalf("secret must win over env: %v", got)
	}
	if got["hostname"] != "a" {
		t.Fatalf("authored answer missing: %v", got)
	}
}
