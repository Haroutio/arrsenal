package wire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeArrDC mimics the download-client API surface captured from a live
// Sonarr (per-arr category field names, qBittorrent's ImportedCategory).
type fakeArrDC struct {
	existing []downloadClient
	posts    atomic.Int32
	lastPost downloadClient
}

func (f *fakeArrDC) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/downloadclient", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.existing)
	})
	mux.HandleFunc("GET /api/v3/downloadclient/schema", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]downloadClient{
			{Implementation: "Sabnzbd", ConfigContract: "SabnzbdSettings", Protocol: "usenet", Fields: []appField{
				{Name: "host", Value: "localhost"}, {Name: "port", Value: 8080},
				{Name: "apiKey"}, {Name: "username"}, {Name: "password"},
				{Name: "tvCategory", Value: "tv"}, {Name: "recentTvPriority", Value: -100},
			}},
			{Implementation: "QBittorrent", ConfigContract: "QBittorrentSettings", Protocol: "torrent", Fields: []appField{
				{Name: "host", Value: "localhost"}, {Name: "port", Value: 8080},
				{Name: "username"}, {Name: "password"},
				{Name: "tvCategory", Value: "tv-sonarr"}, {Name: "tvImportedCategory"},
				{Name: "initialState", Value: 0},
			}},
		})
	})
	mux.HandleFunc("POST /api/v3/downloadclient", func(w http.ResponseWriter, r *http.Request) {
		f.posts.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&f.lastPost)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /api/v3/rootfolder", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]rootFolder{{Path: "/data/media/tv"}})
	})
	return httptest.NewServer(mux)
}

func fieldsOf(d downloadClient) map[string]any {
	out := map[string]any{}
	for _, f := range d.Fields {
		out[f.Name] = f.Value
	}
	return out
}

func TestEnsureDownloadClientSABnzbd(t *testing.T) {
	f := &fakeArrDC{}
	srv := f.server()
	defer srv.Close()

	r := EnsureDownloadClient(context.Background(), wireClient(srv.URL), DownloadClientTarget{
		APIBase: "/api/v3", ArrName: "Sonarr", ClientName: "SABnzbd", Implementation: "Sabnzbd",
		Host: "sabnzbd", Port: 8080, Category: "tv", APIKey: "sab-key",
	})
	if r.Outcome != OutcomeWired || f.posts.Load() != 1 {
		t.Fatalf("want one wired post: %+v posts=%d", r, f.posts.Load())
	}
	p := f.lastPost
	if !p.Enable || p.Name != "SABnzbd" || p.ConfigContract != "SabnzbdSettings" {
		t.Fatalf("envelope: %+v", p)
	}
	got := fieldsOf(p)
	if got["host"] != "sabnzbd" || got["apiKey"] != "sab-key" || got["tvCategory"] != "tv" {
		t.Fatalf("fields: %v", got)
	}
	if got["recentTvPriority"] != float64(-100) {
		t.Fatalf("schema defaults must survive: %v", got["recentTvPriority"])
	}
}

func TestEnsureDownloadClientQBittorrentCategoryDiscipline(t *testing.T) {
	f := &fakeArrDC{}
	srv := f.server()
	defer srv.Close()

	r := EnsureDownloadClient(context.Background(), wireClient(srv.URL), DownloadClientTarget{
		APIBase: "/api/v3", ArrName: "Sonarr", ClientName: "qBittorrent", Implementation: "QBittorrent",
		Host: "qbittorrent", Port: 8081, Category: "tv",
		Username: "admin", Password: "qbit-pass-SECRET",
	})
	if r.Outcome != OutcomeWired {
		t.Fatalf("%+v", r)
	}
	got := fieldsOf(f.lastPost)
	if got["tvCategory"] != "tv" {
		t.Fatalf("category not set: %v", got)
	}
	if got["tvImportedCategory"] != nil {
		t.Fatalf("ImportedCategory must be left alone: %v", got["tvImportedCategory"])
	}
	if got["username"] != "admin" || got["password"] != "qbit-pass-SECRET" || got["port"] != float64(8081) {
		t.Fatalf("connection fields: %v", got)
	}
}

func TestEnsureDownloadClientExistingZeroWrites(t *testing.T) {
	f := &fakeArrDC{existing: []downloadClient{{Name: "SABnzbd"}}}
	srv := f.server()
	defer srv.Close()

	r := EnsureDownloadClient(context.Background(), wireClient(srv.URL), DownloadClientTarget{
		APIBase: "/api/v3", ArrName: "Sonarr", ClientName: "SABnzbd", Implementation: "Sabnzbd",
	})
	if r.Outcome != OutcomeExisted || f.posts.Load() != 0 {
		t.Fatalf("existing client must short-circuit: %+v posts=%d", r, f.posts.Load())
	}
}

func TestEnsureRootFolder(t *testing.T) {
	f := &fakeArrDC{}
	var rfPosts atomic.Int32
	srv := f.server()
	defer srv.Close()

	// Existing path → zero writes.
	r := EnsureRootFolder(context.Background(), wireClient(srv.URL), "/api/v3", "Sonarr", "/data/media/tv")
	if r.Outcome != OutcomeExisted {
		t.Fatalf("existing root folder: %+v", r)
	}

	// A fresh server that records rootfolder posts.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/rootfolder", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("POST /api/v3/rootfolder", func(w http.ResponseWriter, r *http.Request) {
		rfPosts.Add(1)
		body, _ := new(rootFolder), json.NewDecoder(r.Body).Decode(new(rootFolder))
		_ = body
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	srv2 := httptest.NewServer(mux)
	defer srv2.Close()

	r = EnsureRootFolder(context.Background(), wireClient(srv2.URL), "/api/v3", "Sonarr", "/data/media/tv")
	if r.Outcome != OutcomeWired || rfPosts.Load() != 1 {
		t.Fatalf("fresh root folder must be created: %+v posts=%d", r, rfPosts.Load())
	}
	if !strings.Contains(r.Connection, "/data/media/tv") {
		t.Fatalf("report label should carry the path: %s", r.Connection)
	}
}

// Lidarr speaks /api/v1 — the family is not uniform, and assuming v3 handed
// a real install three 404s. The base must reach every request.
func TestEnsureLidarrUsesAPIV1(t *testing.T) {
	var v1Hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/rootfolder", func(w http.ResponseWriter, _ *http.Request) {
		v1Hits.Add(1)
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("GET /api/v1/qualityprofile", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":7}]`))
	})
	mux.HandleFunc("GET /api/v1/metadataprofile", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":3}]`))
	})
	var posted rootFolder
	mux.HandleFunc("POST /api/v1/rootfolder", func(w http.ResponseWriter, r *http.Request) {
		v1Hits.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&posted)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r := EnsureRootFolder(context.Background(), wireClient(srv.URL), "/api/v1", "Lidarr", "/data/media/music")
	if r.Outcome != OutcomeWired || v1Hits.Load() != 2 {
		t.Fatalf("lidarr root folder must ride /api/v1: %+v hits=%d", r, v1Hits.Load())
	}
	// Lidarr rejects a bare path: name + profile defaults are required, and
	// the defaults must come from the app's own profile lists.
	if posted.Name == "" || posted.DefaultQualityProfileID != 7 || posted.DefaultMetadataProfileID != 3 {
		t.Fatalf("lidarr payload must carry name + profile defaults: %+v", posted)
	}
}
