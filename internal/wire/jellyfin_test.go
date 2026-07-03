package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeJellyfin models the surfaces the lane touches, with a switchable
// wizard state.
type fakeJellyfin struct {
	wizardDone bool
	calls      []string
	adminName  string
	adminPass  string
	encoding   map[string]any
	libraries  []map[string]any
	apiKeys    []string
}

func (f *fakeJellyfin) server() *httptest.Server {
	mux := http.NewServeMux()
	record := func(r *http.Request) { f.calls = append(f.calls, r.Method+" "+r.URL.Path) }

	mux.HandleFunc("/Startup/", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if f.wizardDone {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/Startup/User":
			if r.Method == http.MethodPost {
				var u struct{ Name, Password string }
				_ = json.NewDecoder(r.Body).Decode(&u)
				f.adminName, f.adminPass = u.Name, u.Password
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"Name": "root"})
		case "/Startup/Complete":
			f.wizardDone = true
			w.WriteHeader(http.StatusNoContent)
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"UICulture": "en-US"})
		}
	})
	mux.HandleFunc("POST /Users/AuthenticateByName", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("X-Emby-Authorization") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"AccessToken": "jf-token"})
	})
	mux.HandleFunc("/Library/VirtualFolders", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Method == http.MethodPost {
			q := r.URL.Query()
			f.libraries = append(f.libraries, map[string]any{
				"Name": q.Get("name"), "type": q.Get("collectionType"), "path": q.Get("paths"),
			})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(f.libraries)
	})
	mux.HandleFunc("/Auth/Keys", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Method == http.MethodPost {
			f.apiKeys = append(f.apiKeys, r.URL.Query().Get("App"))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		type item struct{ AccessToken, AppName string }
		items := []item{}
		for i, app := range f.apiKeys {
			items = append(items, item{AccessToken: fmt.Sprintf("jf-api-key-%d", i), AppName: app})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Items": items})
	})
	mux.HandleFunc("/System/Configuration/encoding", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&f.encoding)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if f.encoding == nil {
			f.encoding = map[string]any{"HardwareAccelerationType": "none", "keepMe": "yes"}
		}
		_ = json.NewEncoder(w).Encode(f.encoding)
	})
	return httptest.NewServer(mux)
}

func jfTarget(url string) JellyfinTarget {
	return JellyfinTarget{
		URL: url, AdminUser: "harout", AdminPass: "jf-pass-SECRET",
		HWAccel: "nvenc", TranscodePath: "/transcode",
		Libraries: []JellyfinLibrary{
			{Name: "Movies", CollectionType: "movies", Path: "/media/movies"},
			{Name: "Shows", CollectionType: "tvshows", Path: "/media/tv"},
		},
	}
}

func TestJellyfinFreshRunsTheWholeLane(t *testing.T) {
	f := &fakeJellyfin{}
	srv := f.server()
	defer srv.Close()

	results, apiKey := EnsureJellyfin(context.Background(), jfTarget(srv.URL))
	if len(results) != 5 { // wizard + 2 libraries + encoder + API key
		t.Fatalf("want 5 results, got %+v", results)
	}
	if apiKey != "jf-api-key-0" {
		t.Fatalf("the dashboard widget key must come back, got %q", apiKey)
	}
	if len(f.apiKeys) != 1 {
		t.Fatalf("exactly one key created, got %v", f.apiKeys)
	}
	for _, r := range results {
		if r.Outcome != OutcomeWired {
			t.Fatalf("everything should wire on fresh: %+v", r)
		}
	}
	if f.adminName != "harout" || f.adminPass != "jf-pass-SECRET" {
		t.Fatalf("admin not created: %q", f.adminName)
	}
	if !f.wizardDone {
		t.Fatal("wizard must be completed")
	}
	if len(f.libraries) != 2 || f.libraries[0]["path"] != "/media/movies" {
		t.Fatalf("libraries: %+v", f.libraries)
	}
	if f.encoding["HardwareAccelerationType"] != "nvenc" ||
		f.encoding["TranscodingTempPath"] != "/transcode" ||
		f.encoding["EnableHardwareEncoding"] != true {
		t.Fatalf("encoder config: %+v", f.encoding)
	}
	if f.encoding["keepMe"] != "yes" {
		t.Fatal("unknown encoding fields must survive the round trip")
	}
}

func TestJellyfinAdoptedIsLeftEntirelyAlone(t *testing.T) {
	f := &fakeJellyfin{wizardDone: true}
	srv := f.server()
	defer srv.Close()

	results, _ := EnsureJellyfin(context.Background(), jfTarget(srv.URL))
	if len(results) != 1 || results[0].Outcome != OutcomeExisted {
		t.Fatalf("adopted server: %+v", results)
	}
	for _, call := range f.calls {
		if strings.Contains(call, "VirtualFolders") || strings.Contains(call, "encoding") ||
			strings.Contains(call, "Authenticate") {
			t.Fatalf("adopted server was touched: %v", f.calls)
		}
	}
}

func TestJellyfinAmbiguousStateRefusesTheWizard(t *testing.T) {
	// The safety invariant: a server that returns anything other than a
	// clean fresh 200 or an adopted 401 must NEVER get the wizard. A flaky
	// 500 on a configured server must not trigger a destructive setup run.
	var wizardCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/Startup/") {
			if r.Method == http.MethodPost {
				wizardCalls++
			}
			// 200 but not the expected shape — a reverse proxy's HTML error
			// page, say. Ambiguous, and not retryable, so it fails fast.
			_, _ = w.Write([]byte("<html>Bad Gateway</html>"))
		}
	}))
	defer srv.Close()

	results, _ := EnsureJellyfin(context.Background(), jfTarget(srv.URL))
	if len(results) != 1 || results[0].Outcome != OutcomeFailed {
		t.Fatalf("ambiguous state must fail closed: %+v", results)
	}
	if !strings.Contains(results[0].Detail, "refusing") {
		t.Fatalf("must explain the refusal: %+v", results[0])
	}
	if wizardCalls != 0 {
		t.Fatalf("NO destructive wizard POST may run on an unconfirmed server (made %d)", wizardCalls)
	}
}

func TestJellyfinRaceMidWizardReportsAdoptedNotFailed(t *testing.T) {
	// Detection says fresh (200), but the server flips to configured (401)
	// before any POST. That is Existed, and nothing destructive ran.
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/Startup/User" && r.Method == http.MethodGet:
			// First GET (detection) 200; the wizard's second GET 401.
			w.Header().Set("X-Probe", "user")
			_ = json.NewEncoder(w).Encode(map[string]string{"Name": "root"})
		case r.URL.Path == "/Startup/Configuration" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusUnauthorized) // "became" configured
		case r.Method == http.MethodPost:
			posts++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	results, _ := EnsureJellyfin(context.Background(), jfTarget(srv.URL))
	if len(results) != 1 || results[0].Outcome != OutcomeExisted {
		t.Fatalf("mid-wizard 401 must resolve to Existed: %+v", results)
	}
	if posts != 0 {
		t.Fatalf("no POST may run once the server reveals it is configured (made %d)", posts)
	}
}

func TestJellyfinCPUModeSkipsEncoder(t *testing.T) {
	f := &fakeJellyfin{}
	srv := f.server()
	defer srv.Close()

	target := jfTarget(srv.URL)
	target.HWAccel, target.TranscodePath = "", ""
	results, _ := EnsureJellyfin(context.Background(), target)
	for _, r := range results {
		if strings.Contains(r.Connection, "transcoding") {
			t.Fatalf("CPU mode must not touch the encoder: %+v", results)
		}
	}
	if f.encoding != nil {
		t.Fatalf("encoding config touched in CPU mode: %+v", f.encoding)
	}
}

func TestJellyfinExistingLibraryShortCircuits(t *testing.T) {
	f := &fakeJellyfin{libraries: []map[string]any{{"Name": "Movies"}}}
	srv := f.server()
	defer srv.Close()

	results, _ := EnsureJellyfin(context.Background(), jfTarget(srv.URL))
	var movies, shows *Result
	for i := range results {
		if strings.Contains(results[i].Connection, "Movies") {
			movies = &results[i]
		}
		if strings.Contains(results[i].Connection, "Shows") {
			shows = &results[i]
		}
	}
	if movies == nil || movies.Outcome != OutcomeExisted {
		t.Fatalf("existing Movies library: %+v", results)
	}
	if shows == nil || shows.Outcome != OutcomeWired {
		t.Fatalf("missing Shows library must wire: %+v", results)
	}
}
