package kvmclient

import (
	"strings"
	"testing"
)

// noSignalHint is keyed ONLY to the diagnostics boundary: it names the two
// upstream wake paths when the device sent NO media (the signature of a host with
// no HDMI signal), and stays silent for every other boundary. A unit test, not a
// bed, because the boundary is a pure function of the snapshot — but the fields
// it reads must really produce that boundary, so the fixture drives Boundary().
func TestNoSignalHint(t *testing.T) {
	// No media: the track negotiated but zero RTP arrived → BoundaryNoRTP.
	noMedia := VideoDiagnostics{TrackObserved: true, PeerConnectionState: "connected", RTPPackets: 0}
	if got := noMedia.Boundary(); got != BoundaryNoRTP {
		t.Fatalf("fixture boundary = %q, want %q", got, BoundaryNoRTP)
	}
	hint := noSignalHint(noMedia)
	if !strings.Contains(hint, "wake-host") || !strings.Contains(hint, "wol") {
		t.Fatalf("a no-media boundary must name both wake paths; got %q", hint)
	}

	// A captured frame → BoundaryNone → no hint.
	ok := VideoDiagnostics{TrackObserved: true, PeerConnectionState: "connected", RTPPackets: 10, FramesAssembled: 5}
	if got := ok.Boundary(); got != BoundaryNone {
		t.Fatalf("fixture boundary = %q, want %q", got, BoundaryNone)
	}
	if h := noSignalHint(ok); h != "" {
		t.Fatalf("a captured frame must produce no hint; got %q", h)
	}
}
