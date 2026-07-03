package wire

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "test-api-key-SECRET"

func fastClient(base string) *Client {
	c := NewClient(base, testKey, "X-Api-Key")
	c.backoff = time.Millisecond
	return c
}

func TestClientSendsKeyHeaderAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"version":"4.0.0"}`))
	}))
	defer srv.Close()

	var out struct {
		Version string `json:"version"`
	}
	if err := fastClient(srv.URL).GetJSON(context.Background(), "/api/v3/system/status", &out); err != nil {
		t.Fatal(err)
	}
	if out.Version != "4.0.0" {
		t.Fatalf("decoded %+v", out)
	}
}

func TestClientRetriesTransientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // starting up
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if err := fastClient(srv.URL).GetJSON(context.Background(), "/ping", &struct{}{}); err != nil {
		t.Fatalf("transient 503s must be retried through: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls, got %d", calls.Load())
	}
}

func TestClientDoesNotRetryAuthFailures(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := fastClient(srv.URL).GetJSON(context.Background(), "/x", nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("want ErrAuth, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("a rejected key must not be retried (called %d times)", calls.Load())
	}
}

func TestClientGivesUpAfterAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	err := fastClient(srv.URL).GetJSON(context.Background(), "/x", nil)
	if err == nil || !strings.Contains(err.Error(), "after 4 attempts") {
		t.Fatalf("want bounded retries surfaced, got %v", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("attempts = %d, want 4", calls.Load())
	}
}

func TestClientErrorsNeverContainTheKey(t *testing.T) {
	// A hostile/echoing server reflects everything it can see back at us.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request, your key " + r.Header.Get("X-Api-Key") + " is malformed"))
	}))
	defer srv.Close()

	err := fastClient(srv.URL).PostJSON(context.Background(), "/x", map[string]string{"a": "b"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatalf("error leaked the key: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("echoed key should be visibly redacted: %v", err)
	}
}

func TestClientHonorsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // would retry forever
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c := fastClient(srv.URL)
	c.backoff = time.Second // force the wait to hit the deadline
	err := c.GetJSON(ctx, "/x", nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestEnsureByNameContract(t *testing.T) {
	type entry struct{ Name string }

	t.Run("existing entry means zero create calls", func(t *testing.T) {
		created := false
		r := EnsureByName("Prowlarr → Sonarr",
			func() ([]entry, error) { return []entry{{Name: "Sonarr"}}, nil },
			func(e entry) string { return e.Name },
			"Sonarr",
			func() error { created = true; return nil },
		)
		if r.Outcome != OutcomeExisted || created {
			t.Fatalf("existing entry must short-circuit: %+v created=%v", r, created)
		}
	})

	t.Run("absent entry is created", func(t *testing.T) {
		created := false
		r := EnsureByName("Prowlarr → Sonarr",
			func() ([]entry, error) { return nil, nil },
			func(e entry) string { return e.Name },
			"Sonarr",
			func() error { created = true; return nil },
		)
		if r.Outcome != OutcomeWired || !created {
			t.Fatalf("absent entry must be created: %+v created=%v", r, created)
		}
	})

	t.Run("list failure reports, never creates", func(t *testing.T) {
		created := false
		r := EnsureByName("Prowlarr → Sonarr",
			func() ([]entry, error) { return nil, errors.New("api down") },
			func(e entry) string { return e.Name },
			"Sonarr",
			func() error { created = true; return nil },
		)
		if r.Outcome != OutcomeFailed || created {
			t.Fatalf("blind creation is forbidden: %+v created=%v", r, created)
		}
	})

	t.Run("create failure surfaces with the name", func(t *testing.T) {
		r := EnsureByName("arr → SABnzbd",
			func() ([]entry, error) { return nil, nil },
			func(e entry) string { return e.Name },
			"SABnzbd",
			func() error { return errors.New("boom") },
		)
		if r.Outcome != OutcomeFailed || !strings.Contains(r.Detail, "SABnzbd") {
			t.Fatalf("failure must name the entry: %+v", r)
		}
	})
}
