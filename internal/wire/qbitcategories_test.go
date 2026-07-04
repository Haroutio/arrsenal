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

// fakeQBit mimics the WebUI API surface: cookie login with CSRF Referer
// check, category listing, category creation.
type fakeQBit struct {
	password string
	existing string // JSON object of current categories
	creates  atomic.Int32
	lastForm map[string]string
}

func (f *fakeQBit) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		// qBittorrent's CSRF guard: no acceptable Referer → banned.
		if r.Header.Get("Referer") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.FormValue("password") != f.password {
			_, _ = w.Write([]byte("Fails."))
			return
		}
		// Path=/ matches real qBittorrent; without it the jar would scope
		// the cookie to /api/v2/auth/ only.
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session-1", Path: "/"})
		_, _ = w.Write([]byte("Ok."))
	})
	requireSession := func(r *http.Request) bool {
		c, err := r.Cookie("SID")
		return err == nil && c.Value == "session-1"
	}
	mux.HandleFunc("GET /api/v2/torrents/categories", func(w http.ResponseWriter, r *http.Request) {
		if !requireSession(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(f.existing))
	})
	mux.HandleFunc("POST /api/v2/torrents/createCategory", func(w http.ResponseWriter, r *http.Request) {
		if !requireSession(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.creates.Add(1)
		_ = r.ParseForm()
		f.lastForm = map[string]string{
			"category": r.FormValue("category"),
			"savePath": r.FormValue("savePath"),
		}
		_, _ = w.Write(nil)
	})
	return httptest.NewServer(mux)
}

func qbitSession(t *testing.T, srv *httptest.Server, pass string) *Client {
	t.Helper()
	c, err := NewQBitSession(context.Background(), srv.URL, "admin", pass)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	c.backoff = time.Millisecond
	return c
}

func TestEnsureQBitCategoryCreates(t *testing.T) {
	f := &fakeQBit{password: "qbit-pass-SECRET", existing: `{}`}
	srv := f.server()
	defer srv.Close()

	c := qbitSession(t, srv, "qbit-pass-SECRET")
	r := EnsureQBitCategory(context.Background(), c, "tv", "/data/torrents/tv")
	if r.Outcome != OutcomeWired || f.creates.Load() != 1 {
		t.Fatalf("fresh category must be created: %+v creates=%d", r, f.creates.Load())
	}
	if f.lastForm["category"] != "tv" || f.lastForm["savePath"] != "/data/torrents/tv" {
		t.Fatalf("create form wrong: %v", f.lastForm)
	}
}

func TestEnsureQBitCategoryExistingUntouched(t *testing.T) {
	f := &fakeQBit{password: "p", existing: `{"tv":{"name":"tv","savePath":"/somewhere/else"}}`}
	srv := f.server()
	defer srv.Close()

	c := qbitSession(t, srv, "p")
	r := EnsureQBitCategory(context.Background(), c, "tv", "/data/torrents/tv")
	if r.Outcome != OutcomeExisted || f.creates.Load() != 0 {
		t.Fatalf("existing category must be untouched even with a different path: %+v creates=%d",
			r, f.creates.Load())
	}
}

func TestQBitLoginRejectsBadPassword(t *testing.T) {
	f := &fakeQBit{password: "right"}
	srv := f.server()
	defer srv.Close()

	_, err := NewQBitSession(context.Background(), srv.URL, "admin", "wrong-pass-SECRET")
	if err == nil {
		t.Fatal("a 200 'Fails.' body must still be a login failure")
	}
	if strings.Contains(err.Error(), "wrong-pass-SECRET") {
		t.Fatalf("error leaked the password: %v", err)
	}
}
