//go:build live

package wire

// Live-wire harness: exercises wiring steps against a REAL running stack.
// Never runs in CI (build tag). Usage, from a machine that can reach the
// target box's published ports:
//
//	ARRSENAL_LIVE_PROWLARR_URL=http://box:9696 \
//	ARRSENAL_LIVE_PROWLARR_KEY=... \
//	ARRSENAL_LIVE_SONARR_KEY=... \
//	go test ./internal/wire/ -tags live -run TestLive -v
//
// The calls are the real product calls: idempotent by name, additive only.
// Run twice — the second pass must come back all-Existed.

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveProwlarrApplication(t *testing.T) {
	url := os.Getenv("ARRSENAL_LIVE_PROWLARR_URL")
	prowlarrKey := os.Getenv("ARRSENAL_LIVE_PROWLARR_KEY")
	sonarrKey := os.Getenv("ARRSENAL_LIVE_SONARR_KEY")
	if url == "" || prowlarrKey == "" || sonarrKey == "" {
		t.Skip("live env vars not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := NewClient(url, prowlarrKey, "X-Api-Key")

	r := EnsureApplication(ctx, c, ArrTarget{
		Name: "Sonarr", Implementation: "Sonarr",
		URL: "http://sonarr:8989", APIKey: sonarrKey,
		ProwlarrURL: "http://prowlarr:9696",
	})
	t.Logf("first pass: %s → %s %s", r.Connection, r.Outcome, r.Detail)
	if r.Outcome == OutcomeFailed {
		t.Fatalf("live wiring failed: %s", r.Detail)
	}

	r2 := EnsureApplication(ctx, c, ArrTarget{
		Name: "Sonarr", Implementation: "Sonarr",
		URL: "http://sonarr:8989", APIKey: sonarrKey,
		ProwlarrURL: "http://prowlarr:9696",
	})
	t.Logf("second pass: %s → %s", r2.Connection, r2.Outcome)
	if r2.Outcome != OutcomeExisted {
		t.Fatalf("second pass must be Existed (idempotency), got %s", r2.Outcome)
	}
}
