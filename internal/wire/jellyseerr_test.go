package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func jellyseerrServer(initialized bool, reachable bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reachable {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/api/v1/settings/public" {
			if initialized {
				_, _ = w.Write([]byte(`{"initialized":true,"applicationTitle":"Seerr"}`))
			} else {
				_, _ = w.Write([]byte(`{"initialized":false,"applicationTitle":"Seerr"}`))
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestJellyseerrFreshNeedsTheWizard(t *testing.T) {
	srv := jellyseerrServer(false, true)
	defer srv.Close()

	r := EnsureJellyseerr(context.Background(), srv.URL, "http://host:5055")
	if r.Outcome != OutcomeManual {
		t.Fatalf("fresh Jellyseerr is a manual step, not %s: %+v", r.Outcome, r)
	}
	if r.FallbackURL != "http://host:5055" {
		t.Fatalf("must carry the wizard URL: %+v", r)
	}
	for _, want := range []string{"2-minute", "browser"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail should explain the manual step (%q): %s", want, r.Detail)
		}
	}
}

func TestJellyseerrConfiguredIsExisted(t *testing.T) {
	srv := jellyseerrServer(true, true)
	defer srv.Close()

	r := EnsureJellyseerr(context.Background(), srv.URL, "http://host:5055")
	if r.Outcome != OutcomeExisted {
		t.Fatalf("configured Jellyseerr must be left alone: %+v", r)
	}
}

func TestJellyseerrUnreachableIsManualNotFailed(t *testing.T) {
	srv := jellyseerrServer(false, false)
	defer srv.Close()

	c := NewClient(srv.URL, "", "")
	c.backoff = 0
	// Use the real entrypoint; unreachable must degrade to Manual (never
	// block the run) with the fallback URL.
	r := EnsureJellyseerr(context.Background(), srv.URL, "http://host:5055")
	if r.Outcome != OutcomeManual || r.FallbackURL == "" {
		t.Fatalf("unreachable Jellyseerr must be a manual pointer, not a hard failure: %+v", r)
	}
}
