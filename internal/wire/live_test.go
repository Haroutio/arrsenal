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

func TestLiveDownloadClientAndRootFolder(t *testing.T) {
	url := os.Getenv("ARRSENAL_LIVE_SONARR_URL")
	sonarrKey := os.Getenv("ARRSENAL_LIVE_SONARR_KEY")
	sabKey := os.Getenv("ARRSENAL_LIVE_SAB_KEY")
	if url == "" || sonarrKey == "" || sabKey == "" {
		t.Skip("live env vars not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := NewClient(url, sonarrKey, "X-Api-Key")

	// SAB must accept container-name requests before any arr can register
	// it — the 403 hostname-verification gotcha, found on this very box.
	if sabURL := os.Getenv("ARRSENAL_LIVE_SAB_URL"); sabURL != "" {
		sab := NewSABClient(sabURL, sabKey)
		for _, step := range []func() Result{
			func() Result { return EnsureSABWhitelist(ctx, sab, "sabnzbd") },
			func() Result { return EnsureSABFolders(ctx, sab) },
			func() Result { return EnsureSABCategory(ctx, sab, "tv") },
		} {
			r := step()
			t.Logf("sab prep: %s → %s %s", r.Connection, r.Outcome, r.Detail)
			if r.Outcome == OutcomeFailed {
				t.Fatalf("SAB preparation failed: %s", r.Detail)
			}
		}
	}

	target := DownloadClientTarget{
		ArrName: "Sonarr", ClientName: "SABnzbd", Implementation: "Sabnzbd",
		Host: "sabnzbd", Port: 8080, Category: "tv", APIKey: sabKey,
	}
	r := EnsureDownloadClient(ctx, c, target)
	t.Logf("dl client first pass: %s → %s %s", r.Connection, r.Outcome, r.Detail)
	if r.Outcome == OutcomeFailed {
		t.Fatalf("live download-client wiring failed: %s", r.Detail)
	}
	if r2 := EnsureDownloadClient(ctx, c, target); r2.Outcome != OutcomeExisted {
		t.Fatalf("second pass must be Existed, got %s", r2.Outcome)
	}

	rf := EnsureRootFolder(ctx, c, "Sonarr", "/data/media/tv")
	t.Logf("root folder first pass: %s → %s %s", rf.Connection, rf.Outcome, rf.Detail)
	if rf.Outcome == OutcomeFailed {
		t.Fatalf("live root-folder wiring failed: %s", rf.Detail)
	}
	if rf2 := EnsureRootFolder(ctx, c, "Sonarr", "/data/media/tv"); rf2.Outcome != OutcomeExisted {
		t.Fatalf("root folder second pass must be Existed, got %s", rf2.Outcome)
	}
}

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
