package wire

import (
	"strings"
	"testing"
)

func sampleResults() []Result {
	return []Result{
		{Connection: "Prowlarr → Sonarr", Outcome: OutcomeWired},
		{Connection: "Sonarr → SABnzbd", Outcome: OutcomeExisted},
		{Connection: "Jellyseerr requests", Outcome: OutcomeManual,
			Detail: "finish its 2-minute setup wizard", FallbackURL: "http://192.168.1.10:5055"},
		{Connection: "Radarr root folder /data/media/movies", Outcome: OutcomeFailed,
			Detail: "creating \"/data/media/movies\": HTTP 400"},
	}
}

func TestRenderReportSnapshot(t *testing.T) {
	got := RenderReport(sampleResults())
	// Alignment is by RUNE count (labels carry → arrows); pinned from the
	// verified visual rendering.
	want := "\nWiring report:\n  ✓ Prowlarr → Sonarr                    \n  ● Sonarr → SABnzbd                     \n  ⚠ Jellyseerr requests                    finish its 2-minute setup wizard → http://192.168.1.10:5055\n  ✗ Radarr root folder /data/media/movies  creating \"/data/media/movies\": HTTP 400\n\n  1 wired · 1 existed · 1 manual · 1 failed\n  Containers stay up — fix the ✗ items above by hand; everything else is done.\n"
	if got != want {
		t.Fatalf("snapshot drifted:\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
}

func TestRenderReportAllCleanHasNoInstructions(t *testing.T) {
	got := RenderReport([]Result{
		{Connection: "A", Outcome: OutcomeWired},
		{Connection: "B", Outcome: OutcomeExisted},
	})
	for _, forbidden := range []string{"fix", "Finish"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("clean run needs no instructions:\n%s", got)
		}
	}
	if !strings.Contains(got, "1 wired · 1 existed") {
		t.Fatalf("summary line: %s", got)
	}
}

func TestRenderReportManualOnlyGuidance(t *testing.T) {
	got := RenderReport([]Result{
		{Connection: "A", Outcome: OutcomeWired},
		{Connection: "B", Outcome: OutcomeManual, FallbackURL: "http://x"},
	})
	if !strings.Contains(got, "Finish the ⚠ items") || strings.Contains(got, "✗") {
		t.Fatalf("manual-only guidance wrong:\n%s", got)
	}
}

func TestRenderReportEmpty(t *testing.T) {
	if got := RenderReport(nil); !strings.Contains(got, "nothing to wire") {
		t.Fatalf("%q", got)
	}
}

func TestFailedPredicate(t *testing.T) {
	if Failed(sampleResults()) != true {
		t.Fatal("sample contains a failure")
	}
	if Failed([]Result{{Outcome: OutcomeManual}, {Outcome: OutcomeExisted}}) {
		t.Fatal("manual and existed are not failures")
	}
}
