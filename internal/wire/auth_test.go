package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const adminPass = "correct-horse-SECRET"

// fakeArr serves a host config and records writes.
type fakeArr struct {
	config  map[string]any
	puts    atomic.Int32
	lastPut map[string]any
}

func newFakeArr(method string) *fakeArr {
	return &fakeArr{config: map[string]any{
		"id":                     1,
		"bindAddress":            "*",
		"port":                   8989,
		"authenticationMethod":   method,
		"authenticationRequired": "enabled",
		"futureField":            "must-survive-roundtrip",
	}}
}

func (f *fakeArr) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/config/host", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.config)
	})
	mux.HandleFunc("PUT /api/v3/config/host/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.puts.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&f.lastPut)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

func authClient(url string) *Client {
	c := NewClient(url, "k", "X-Api-Key")
	c.backoff = time.Millisecond
	return c
}

func TestEnsureAuthConfiguresFreshApps(t *testing.T) {
	f := newFakeArr("none")
	srv := f.server(t)
	defer srv.Close()

	r := EnsureAuth(context.Background(), authClient(srv.URL), "Sonarr", "harout", adminPass, false)
	if r.Outcome != OutcomeWired {
		t.Fatalf("fresh app must be wired: %+v", r)
	}
	if f.puts.Load() != 1 {
		t.Fatalf("want exactly one write, got %d", f.puts.Load())
	}
	if f.lastPut["authenticationMethod"] != "forms" || f.lastPut["username"] != "harout" ||
		f.lastPut["password"] != adminPass || f.lastPut["passwordConfirmation"] != adminPass {
		t.Fatalf("auth fields wrong: %+v", f.lastPut)
	}
	if f.lastPut["futureField"] != "must-survive-roundtrip" {
		t.Fatal("unknown fields must ride through the round trip untouched")
	}
}

func TestEnsureAuthLeavesConfiguredAppsAlone(t *testing.T) {
	f := newFakeArr("forms")
	srv := f.server(t)
	defer srv.Close()

	r := EnsureAuth(context.Background(), authClient(srv.URL), "Sonarr", "harout", adminPass, false)
	if r.Outcome != OutcomeExisted || f.puts.Load() != 0 {
		t.Fatalf("already-authed app must see zero writes: %+v puts=%d", r, f.puts.Load())
	}
}

func TestEnsureAuthNeverTouchesAdoptedConfigs(t *testing.T) {
	// The field case: an adopted config with method "none" is a CHOICE
	// (LAN-only behind a tunnel), not an unfinished wizard.
	f := newFakeArr("none")
	srv := f.server(t)
	defer srv.Close()

	r := EnsureAuth(context.Background(), authClient(srv.URL), "Sonarr", "harout", adminPass, true)
	if r.Outcome != OutcomeExisted || f.puts.Load() != 0 {
		t.Fatalf("adopted none-auth must be preserved: %+v puts=%d", r, f.puts.Load())
	}
	if !strings.Contains(r.Detail, "adopted") {
		t.Fatalf("detail should say why it was skipped: %+v", r)
	}
}

func TestEnsureAuthFailureNeverLeaksThePassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "authenticationMethod": "none"})
			return
		}
		// Hostile echo: reflect the request body into the error response.
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "rejected: %v", body)
	}))
	defer srv.Close()

	r := EnsureAuth(context.Background(), authClient(srv.URL), "Sonarr", "harout", adminPass, false)
	if r.Outcome != OutcomeFailed {
		t.Fatalf("expected failure: %+v", r)
	}
	if strings.Contains(r.Detail, adminPass) {
		t.Fatalf("result leaked the password: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "[redacted]") {
		t.Fatalf("the echoed password should be visibly redacted: %s", r.Detail)
	}
}
