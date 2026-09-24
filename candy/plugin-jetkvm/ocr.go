package jetkvm

// ocr.go implements the read-only `ocr` method.
//
// OCR RUNS ON THE HOST, over the PNG the device's capture produced: the plugin
// process runs on the host (out-of-process), tesseract is a host tool, and the
// captured bytes are already in this process. So there is no venue round-trip.
// The OCR itself is the SDK's single shared implementation (kit.OCRBytes, R3) —
// the SAME one the artifact validator and every console transport use — so this
// file owns only the capture + assertion, never a second tesseract invocation.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/opencharly/sdk/kit"

	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/internal/kvmclient"
	"github.com/opencharly/plugin-jetkvm/candy/plugin-jetkvm/params"
)

// methodOcr captures one frame, OCRs it, and asserts the authored text is
// present (case-insensitive substring). It is the wait-for-screen primitive in
// method form: no artifact is required (the `artifact` field, when authored,
// still saves the capture for evidence).
func methodOcr(ctx context.Context, cl *kvmclient.Client, in *params.JetkvmInput) (string, error) {
	if strings.TrimSpace(in.Text) == "" {
		return "", fmt.Errorf("jetkvm: ocr requires text (the expected on-screen string)")
	}
	shot, err := cl.CaptureScreenshot(ctx)
	if err != nil {
		return "", err
	}
	if in.Artifact != "" {
		if err := os.WriteFile(in.Artifact, shot.PNG, 0o644); err != nil {
			return "", fmt.Errorf("jetkvm: ocr: writing %s: %w", in.Artifact, err)
		}
	}
	got, err := kit.OCRBytes(shot.PNG)
	if err != nil {
		return "", err
	}
	if !strings.Contains(strings.ToLower(got), strings.ToLower(in.Text)) {
		return "", fmt.Errorf("jetkvm: ocr: the screen text does not contain %q (read %d characters; first 200: %q)",
			in.Text, len(got), kit.ConsolePreview(got, 200))
	}
	return fmt.Sprintf("The screen text contains %q (read %d characters)", in.Text, len(got)), nil
}
