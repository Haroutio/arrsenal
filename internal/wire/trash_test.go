package wire

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Haroutio/arrsenal/internal/quality"
)

func trashSpec(t *testing.T, run func() (string, error)) Spec {
	t.Helper()
	return Spec{
		TRaSH:        &quality.Answers{Resolution: "1080p", Source: "bluray-web"},
		RecyclarrDir: t.TempDir(),
		RunRecyclarr: run,
	}
}

func TestRunTRaSHSyncsSelectedInstances(t *testing.T) {
	spec := trashSpec(t, func() (string, error) { return "sync ok", nil })
	results := runTRaSH(spec, map[string]string{"sonarr": "sonkey", "radarr": "radkey"})

	if len(results) != 2 {
		t.Fatalf("results = %+v, want one synced per instance", results)
	}
	for _, r := range results {
		if r.Outcome != OutcomeSynced {
			t.Fatalf("outcome = %s, want %s (%+v)", r.Outcome, OutcomeSynced, r)
		}
	}
}

// The failure path is where the keys are in danger: dockerx embeds the
// container's combined output inside the returned error (with empty out), so
// redaction must cover the error text itself — pinned here because the
// original code redacted only out and leaked whatever err carried.
func TestRunTRaSHRedactsKeysOnFailure(t *testing.T) {
	failure := errors.New("docker run recyclarr sync: exit status 1\n" +
		"Deserialization error: api_key: sonarr-secret-key rejected\n" +
		"header X-Api-Key: radarr-secret-key refused")
	spec := trashSpec(t, func() (string, error) { return "", failure })

	results := runTRaSH(spec, map[string]string{
		"sonarr": "sonarr-secret-key", "radarr": "radarr-secret-key"})

	if len(results) != 1 || results[0].Outcome != OutcomeFailed {
		t.Fatalf("results = %+v, want a single failure", results)
	}
	detail := results[0].Detail
	if strings.Contains(detail, "sonarr-secret-key") || strings.Contains(detail, "radarr-secret-key") {
		t.Fatalf("API key leaked into the report:\n%s", detail)
	}
	if !strings.Contains(detail, "[redacted]") {
		t.Fatalf("redaction marker missing — is the error text still covered?\n%s", detail)
	}
	if !strings.Contains(detail, "Deserialization error") {
		t.Fatalf("diagnostic content lost:\n%s", detail)
	}
}

func TestRunTRaSHWritesConfigBeforeSync(t *testing.T) {
	var ranAfterWrite bool
	var spec Spec
	spec = trashSpec(t, func() (string, error) {
		if _, err := os.ReadFile(filepath.Join(spec.RecyclarrDir, "recyclarr.yml")); err == nil {
			ranAfterWrite = true
		}
		return "", nil
	})
	runTRaSH(spec, map[string]string{"sonarr": "k"})
	if !ranAfterWrite {
		t.Fatal("recyclarr.yml must exist before the sync container runs")
	}
}
