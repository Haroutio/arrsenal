package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeSAB(t *testing.T, whitelist string, sets *atomic.Int32, lastValue *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("apikey") != "sab-key-SECRET" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch q.Get("mode") {
		case "get_config":
			list := `"` + strings.Join(strings.Split(whitelist, ","), `","`) + `"`
			if whitelist == "" {
				list = ""
			}
			_, _ = w.Write([]byte(`{"config":{"misc":{"host_whitelist":[` + list + `]}}}`))
		case "set_config":
			sets.Add(1)
			*lastValue = q.Get("value")
			_, _ = w.Write([]byte(`{"config":{}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestEnsureSABWhitelistAppends(t *testing.T) {
	var sets atomic.Int32
	var lastValue string
	srv := fakeSAB(t, "3aac10e0a1b9", &sets, &lastValue)
	defer srv.Close()

	c := NewSABClient(srv.URL, "sab-key-SECRET")
	c.backoff = time.Millisecond
	r := EnsureSABWhitelist(context.Background(), c, "sabnzbd")
	if r.Outcome != OutcomeWired || sets.Load() != 1 {
		t.Fatalf("want wired with one set: %+v sets=%d", r, sets.Load())
	}
	if lastValue != "3aac10e0a1b9,sabnzbd" {
		t.Fatalf("existing entries must survive the append: %q", lastValue)
	}
}

func TestEnsureSABWhitelistExistingZeroWrites(t *testing.T) {
	var sets atomic.Int32
	var lastValue string
	srv := fakeSAB(t, "3aac10e0a1b9,sabnzbd", &sets, &lastValue)
	defer srv.Close()

	c := NewSABClient(srv.URL, "sab-key-SECRET")
	c.backoff = time.Millisecond
	r := EnsureSABWhitelist(context.Background(), c, "sabnzbd")
	if r.Outcome != OutcomeExisted || sets.Load() != 0 {
		t.Fatalf("whitelisted already: %+v sets=%d", r, sets.Load())
	}
}

func fakeSABFull(t *testing.T, downloadDir, completeDir string, categories []string, sets *atomic.Int32, setLog *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("mode") == "get_config" && q.Get("section") == "misc":
			_, _ = w.Write([]byte(`{"config":{"misc":{"host_whitelist":[],"download_dir":"` +
				downloadDir + `","complete_dir":"` + completeDir + `"}}}`))
		case q.Get("mode") == "get_config" && q.Get("section") == "categories":
			out := `{"config":{"categories":[`
			for i, c := range categories {
				if i > 0 {
					out += ","
				}
				out += `{"name":"` + c + `"}`
			}
			_, _ = w.Write([]byte(out + `]}}`))
		case q.Get("mode") == "set_config":
			sets.Add(1)
			*setLog = append(*setLog, q.Get("section")+"/"+q.Get("keyword")+"="+q.Get("value")+q.Get("dir"))
			_, _ = w.Write([]byte(`{"config":{}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestEnsureSABFoldersCorrectsStockDefaultsOnly(t *testing.T) {
	var sets atomic.Int32
	var log []string
	srv := fakeSABFull(t, "Downloads/incomplete", "Downloads/complete", nil, &sets, &log)
	defer srv.Close()

	c := NewSABClient(srv.URL, "k")
	c.backoff = time.Millisecond
	if r := EnsureSABFolders(context.Background(), c); r.Outcome != OutcomeWired || sets.Load() != 2 {
		t.Fatalf("stock defaults must be corrected: %+v sets=%d %v", r, sets.Load(), log)
	}

	// A customized dir is the user's — zero writes, explanation given.
	var sets2 atomic.Int32
	var log2 []string
	srv2 := fakeSABFull(t, "/mnt/custom/incomplete", "Downloads/complete", nil, &sets2, &log2)
	defer srv2.Close()
	c2 := NewSABClient(srv2.URL, "k")
	c2.backoff = time.Millisecond
	r := EnsureSABFolders(context.Background(), c2)
	if r.Outcome != OutcomeExisted || sets2.Load() != 0 || !strings.Contains(r.Detail, "user") {
		t.Fatalf("custom dirs must never be touched: %+v sets=%d", r, sets2.Load())
	}

	// Already on the data tree → plain existed.
	var sets3 atomic.Int32
	var log3 []string
	srv3 := fakeSABFull(t, "/data/usenet/incomplete", "/data/usenet/complete", nil, &sets3, &log3)
	defer srv3.Close()
	c3 := NewSABClient(srv3.URL, "k")
	c3.backoff = time.Millisecond
	if r := EnsureSABFolders(context.Background(), c3); r.Outcome != OutcomeExisted || sets3.Load() != 0 {
		t.Fatalf("correct dirs are existed: %+v", r)
	}
}

func TestEnsureSABCategory(t *testing.T) {
	var sets atomic.Int32
	var log []string
	srv := fakeSABFull(t, "", "", []string{"movies"}, &sets, &log)
	defer srv.Close()
	c := NewSABClient(srv.URL, "k")
	c.backoff = time.Millisecond

	if r := EnsureSABCategory(context.Background(), c, "movies"); r.Outcome != OutcomeExisted || sets.Load() != 0 {
		t.Fatalf("existing category: %+v", r)
	}
	r := EnsureSABCategory(context.Background(), c, "tv")
	if r.Outcome != OutcomeWired || sets.Load() != 1 {
		t.Fatalf("missing category must be created: %+v sets=%d", r, sets.Load())
	}
	if len(log) != 1 || !strings.Contains(log[0], "categories/tv") || !strings.Contains(log[0], "tv") {
		t.Fatalf("category create call wrong: %v", log)
	}
}

func TestSABKeyNeverLeaksFromQueryString(t *testing.T) {
	// SAB carries the key in the URL; a failure must not print it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	c := NewSABClient(srv.URL, "sab-key-SECRET")
	c.backoff = time.Millisecond
	r := EnsureSABWhitelist(context.Background(), c, "sabnzbd")
	if r.Outcome != OutcomeFailed {
		t.Fatalf("want failure: %+v", r)
	}
	if strings.Contains(r.Detail, "sab-key-SECRET") {
		t.Fatalf("query-string key leaked: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "%5Bredacted%5D") && !strings.Contains(r.Detail, "[redacted]") {
		t.Fatalf("key should be visibly redacted in the path: %s", r.Detail)
	}
}

// fakeSABServers models the servers section: a get that lists what exists
// and a set that records the exact registration query.
func fakeSABServers(t *testing.T, existingHost string, sets *atomic.Int32, lastQuery *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("mode") {
		case "get_config":
			servers := `[]`
			if existingHost != "" {
				servers = `[{"host":"` + existingHost + `"}]`
			}
			_, _ = w.Write([]byte(`{"config":{"servers":` + servers + `}}`))
		case "set_config":
			sets.Add(1)
			*lastQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"config":{}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func TestEnsureSABServerRegistersProvider(t *testing.T) {
	var sets atomic.Int32
	var lastQuery string
	srv := fakeSABServers(t, "", &sets, &lastQuery)
	defer srv.Close()

	p := UsenetPresets["newshosting"]
	p.Username, p.Password = "demo", "usenet-pass-SECRET"
	c := NewSABClient(srv.URL, "sab-key-SECRET")
	c.backoff = time.Millisecond
	r := EnsureSABServer(context.Background(), c, p)
	if r.Outcome != OutcomeWired || sets.Load() != 1 {
		t.Fatalf("fresh SAB must get the server: %+v sets=%d", r, sets.Load())
	}
	for _, want := range []string{"host=news.newshosting.com", "port=563", "ssl=1",
		"username=demo", "connections=30", "enable=1"} {
		if !strings.Contains(lastQuery, want) {
			t.Fatalf("registration query missing %q:\n%s", want, lastQuery)
		}
	}
}

func TestEnsureSABServerNeverTouchesExisting(t *testing.T) {
	var sets atomic.Int32
	var lastQuery string
	srv := fakeSABServers(t, "news.newshosting.com", &sets, &lastQuery)
	defer srv.Close()

	p := UsenetPresets["newshosting"]
	p.Username, p.Password = "demo", "other-pass"
	c := NewSABClient(srv.URL, "sab-key-SECRET")
	c.backoff = time.Millisecond
	r := EnsureSABServer(context.Background(), c, p)
	if r.Outcome != OutcomeExisted || sets.Load() != 0 {
		t.Fatalf("existing server must be untouched (even the credentials): %+v sets=%d", r, sets.Load())
	}
}

func TestEnsureSABServerRedactsPasswordOnFailure(t *testing.T) {
	// Hostile echo: the error body reflects the full request URL, password
	// and all — the report must never carry it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "get_config" {
			_, _ = w.Write([]byte(`{"config":{"servers":[]}}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom: " + r.URL.String()))
	}))
	defer srv.Close()

	p := UsenetPresets["newshosting"]
	p.Username, p.Password = "demo", "usenet-pass-SECRET"
	c := NewSABClient(srv.URL, "sab-key-SECRET")
	c.backoff = time.Millisecond
	r := EnsureSABServer(context.Background(), c, p)
	if r.Outcome != OutcomeFailed {
		t.Fatalf("want failure, got %+v", r)
	}
	if strings.Contains(r.Detail, "usenet-pass-SECRET") {
		t.Fatalf("password leaked into the report:\n%s", r.Detail)
	}
}
