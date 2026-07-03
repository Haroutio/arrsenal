package preflight

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Haroutio/arrsenal/internal/state"
)

func linuxOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("filesystem semantics under test are POSIX; CI covers this")
	}
}

func testState(t *testing.T, apps ...string) *state.State {
	t.Helper()
	s := state.New()
	root := t.TempDir()
	s.DataRoot = filepath.Join(root, "data")
	s.AppdataRoot = filepath.Join(root, "appdata")
	s.Apps = apps
	// Chown to ourselves so the tests need no root.
	s.PUID = os.Getuid()
	s.PGID = os.Getgid()
	return s
}

func TestEnsureTreeCreatesTheFullLayout(t *testing.T) {
	linuxOnly(t)
	s := testState(t, "sonarr", "sabnzbd")

	dirs, err := EnsureTree(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range DataSubdirs {
		p := filepath.Join(s.DataRoot, sub)
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			t.Errorf("%s missing after EnsureTree", p)
		}
	}
	for _, app := range s.Apps {
		p := filepath.Join(s.AppdataRoot, app)
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			t.Errorf("appdata dir %s missing", p)
		}
	}
	created := 0
	for _, d := range dirs {
		if d.Created {
			created++
		}
	}
	if created != len(DataSubdirs)+len(s.Apps) {
		t.Errorf("created %d dirs, want %d", created, len(DataSubdirs)+len(s.Apps))
	}
}

func TestEnsureTreeIsIdempotentAndNeverTouchesContent(t *testing.T) {
	linuxOnly(t)
	s := testState(t, "sonarr")
	if _, err := EnsureTree(s); err != nil {
		t.Fatal(err)
	}

	// A brownfield config drops files in; a re-run must not disturb them.
	marker := filepath.Join(s.AppdataRoot, "sonarr", "config.xml")
	if err := os.WriteFile(marker, []byte("<Config/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs, err := EnsureTree(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if d.Created {
			t.Errorf("re-run claims it created %s", d.Path)
		}
	}
	b, err := os.ReadFile(marker)
	if err != nil || string(b) != "<Config/>" {
		t.Fatalf("re-run disturbed existing content: %s %v", b, err)
	}
}

func TestEnsureTreeRefusesNonDirectoryCollision(t *testing.T) {
	linuxOnly(t)
	s := testState(t, "sonarr")
	if err := os.MkdirAll(s.AppdataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// A FILE where the appdata dir should be.
	if err := os.WriteFile(filepath.Join(s.AppdataRoot, "sonarr"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := EnsureTree(s)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("want a clear not-a-directory error, got: %v", err)
	}
}

func TestEnsureTreeIncludesJellyfinShmScratch(t *testing.T) {
	linuxOnly(t)
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("/dev/shm not available")
	}
	s := testState(t, "jellyfin")
	t.Cleanup(func() { _ = os.Remove("/dev/shm/jellyfin") })

	dirs, err := EnsureTree(s)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range dirs {
		if d.Path == "/dev/shm/jellyfin" {
			found = true
		}
	}
	if !found {
		t.Fatal("jellyfin selection must ensure its tmpfs transcode scratch")
	}
}

func TestDirModeFromUmask(t *testing.T) {
	cases := map[string]os.FileMode{
		"002":  0o775,
		"022":  0o755,
		"0022": 0o755,
		"077":  0o700,
	}
	for umask, want := range cases {
		if got := dirModeFromUmask(umask); got != want {
			t.Errorf("umask %s → %o, want %o", umask, got, want)
		}
	}
}

func TestEnsureTreeValidatesState(t *testing.T) {
	s := state.New() // Windows-safe: never touches the filesystem
	s.Apps = []string{"nonsense-app"}
	if _, err := EnsureTree(s); err == nil {
		t.Fatal("invalid state must be refused before any directory is made")
	}
}
