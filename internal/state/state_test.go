package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Haroutio/arrsenal/internal/registry"
)

func full() *State {
	s := New()
	s.Apps = []string{"jellyfin", "sonarr", "radarr", "sabnzbd", "qbittorrent"}
	s.TZ = "America/Los_Angeles"
	s.DataRoot = "/mnt/das/data"
	s.GPU = GPUNvidia
	// Several entries, inserted in non-sorted order: the round-trip guarantee
	// must hold under map iteration, not by luck of a single key.
	s.PortRemaps = map[string]map[int]int{
		"sonarr":      {8989: 9989},
		"qbittorrent": {8081: 9091, 6881: 16881},
		"jellyfin":    {7359: 17359},
	}
	s.JellyfinHostNetwork = false
	s.Secrets.QBittorrentPassword = "not-a-real-password"
	return s
}

func TestSaveLoadRoundTripIsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "arrsenal.yaml")

	if err := full().Save(p); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(p); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("save→load→save changed bytes:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	// Determinism under repetition (map ordering would betray itself here).
	for i := 0; i < 20; i++ {
		if err := full().Save(p); err != nil {
			t.Fatal(err)
		}
		again, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("iteration %d: identical state produced different bytes", i)
		}
	}
}

func TestHostPortResolution(t *testing.T) {
	s := full()
	qb, _ := lookApp(t, "qbittorrent")
	if got := s.WebHostPort(qb); got != 9091 {
		t.Fatalf("qbittorrent web = %d, want remapped 9091", got)
	}
	for _, p := range qb.ExtraPorts {
		if got := s.HostPort(qb, p); got != 16881 {
			t.Fatalf("qbittorrent %s %d = %d, want remapped 16881 (both protocols move together)",
				p.Protocol, p.Container, got)
		}
	}
	son, _ := lookApp(t, "sonarr")
	if got := s.WebHostPort(son); got != 9989 {
		t.Fatalf("sonarr web = %d, want 9989", got)
	}
	sab, _ := lookApp(t, "sabnzbd")
	if got := s.WebHostPort(sab); got != 8080 {
		t.Fatalf("sabnzbd web = %d, want registry default 8080 (no remap)", got)
	}
}

func TestSaveIsAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "arrsenal.yaml")
	if err := full().Save(p); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("state file mode = %o, want 0600 — it can hold secrets", perm)
		}
	}

	// No temp debris left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".arrsenal-state-") {
			t.Fatalf("temp file %s left behind", e.Name())
		}
	}
}

func TestLoadTightensLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "arrsenal.yaml")
	if err := full().Save(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("Load left a secret-bearing file at %o, want re-tightened 0600", perm)
	}
}

func TestLoadMissingFileIsErrNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("err = %v, want ErrNotExist (fresh install is not a failure)", err)
	}
}

func TestLoadToleratesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "arrsenal.yaml")
	doc := `version: 1
apps: [sonarr]
puid: 1000
pgid: 1000
tz: Etc/UTC
umask: "002"
data_root: /data
appdata_root: /opt/appdata
gpu: none
some_future_field: {added: "in v9"}
`
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(p)
	if err != nil {
		t.Fatalf("unknown fields must not break loading: %v", err)
	}
	if len(s.Apps) != 1 || s.Apps[0] != "sonarr" {
		t.Fatalf("known fields mis-parsed: %+v", s)
	}
}

func TestLoadErrorsNameThePathAndTheFix(t *testing.T) {
	dir := t.TempDir()

	corrupt := filepath.Join(dir, "corrupt.yaml")
	if err := os.WriteFile(corrupt, []byte(":\t this is not yaml {{{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(corrupt)
	if err == nil {
		t.Fatal("corrupt file must not load")
	}
	for _, want := range []string{corrupt, "delete it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("corrupt-file error %q should mention %q", err, want)
		}
	}

	future := filepath.Join(dir, "future.yaml")
	if err := os.WriteFile(future, []byte("version: 99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(future)
	if err == nil || !strings.Contains(err.Error(), "upgrade arrsenal") {
		t.Fatalf("future-version error should tell the user to upgrade, got: %v", err)
	}

	unversioned := filepath.Join(dir, "random.yaml")
	if err := os.WriteFile(unversioned, []byte("hello: world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(unversioned)
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("a versionless file is not ours to touch, got: %v", err)
	}
}

func TestLoadErrorsNeverQuoteSecrets(t *testing.T) {
	// goccy's annotated errors excerpt the file around the failure point;
	// Load must strip that or a parse error near the secrets block prints
	// the password to the terminal (DESIGN §9).
	const secret = "hunter2-SUPER-SECRET"
	dir := t.TempDir()
	p := filepath.Join(dir, "arrsenal.yaml")
	doc := `version: 1
secrets:
  qbittorrent_webui_password: ` + secret + `
puid: not-an-int
`
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatal("file must not load")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error message leaked the secret: %q", err)
	}
}

func TestValidateRejectsBadStates(t *testing.T) {
	cases := map[string]func(*State){
		"unknown app":        func(s *State) { s.Apps = []string{"readarr"} },
		"duplicate app":      func(s *State) { s.Apps = []string{"sonarr", "sonarr"} },
		"negative puid":      func(s *State) { s.PUID = -1 },
		"empty tz":           func(s *State) { s.TZ = "" },
		"decimal umask":      func(s *State) { s.Umask = "22" },
		"relative path":      func(s *State) { s.DataRoot = "data" },
		"colon in path":      func(s *State) { s.DataRoot = "/mnt/a:b" },
		"space in path":      func(s *State) { s.AppdataRoot = "/opt/app data" },
		"newline in tz":      func(s *State) { s.TZ = "Etc/UTC\nEVIL=1" },
		"hash in tz":         func(s *State) { s.TZ = "Etc/UTC # comment" },
		"bad gpu":            func(s *State) { s.GPU = "voodoo2" },
		"remap unknown app":  func(s *State) { s.PortRemaps["winamp"] = map[int]int{32400: 32400} },
		"remap unknown port": func(s *State) { s.PortRemaps["sonarr"] = map[int]int{1234: 5678} },
		"remap port range":   func(s *State) { s.PortRemaps["sonarr"] = map[int]int{8989: 70000} },
		"remap collision": func(s *State) {
			s.PortRemaps["sonarr"] = map[int]int{8989: 7878} // radarr's default, both selected
		},
		"default collision unfixed": func(s *State) {
			// sabnzbd (8080) + a qbittorrent remapped ONTO 8080
			s.PortRemaps["qbittorrent"] = map[int]int{8081: 8080}
		},
	}
	for name, mutate := range cases {
		s := full()
		mutate(s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: Validate accepted it", name)
		}
	}
	if err := full().Validate(); err != nil {
		t.Fatalf("baseline state must be valid: %v", err)
	}
}

func TestJellyfinHostNetworkStillOwnsItsHostPorts(t *testing.T) {
	// Host networking publishes nothing on the bridge, but Jellyfin binds
	// 8096/7359 directly on the host — another app moved onto 8096 still
	// collides at `up`, so Validate must still reject it.
	s := full()
	s.JellyfinHostNetwork = true
	s.PortRemaps["sonarr"] = map[int]int{8989: 8096}
	err := s.Validate()
	if err == nil {
		t.Fatal("host-networked jellyfin binds host 8096; sonarr remapped onto it must be rejected")
	}
	if !strings.Contains(err.Error(), "host networking") {
		t.Fatalf("the error should say the port is owned via host networking: %v", err)
	}
	// And jellyfin's own remaps are meaningless in host mode: the claim
	// stays on the container port.
	s = full()
	s.JellyfinHostNetwork = true
	s.PortRemaps["jellyfin"] = map[int]int{8096: 18096}
	s.PortRemaps["sonarr"] = map[int]int{8989: 8096}
	if err := s.Validate(); err == nil {
		t.Fatal("remapping a host-networked app must not free its real host port")
	}
}

func TestWebPortsFollowHostForWebPortEnvApps(t *testing.T) {
	s := full()
	qb, _ := lookApp(t, "qbittorrent")
	host, container := s.WebPorts(qb)
	if host != 9091 || container != 9091 {
		t.Fatalf("qbittorrent WebPorts = %d:%d, want 9091:9091 — WEBUI_PORT apps move both sides", host, container)
	}
	son, _ := lookApp(t, "sonarr")
	host, container = s.WebPorts(son)
	if host != 9989 || container != 8989 {
		t.Fatalf("sonarr WebPorts = %d:%d, want 9989:8989 — normal apps keep their container port", host, container)
	}
}

func TestSplitStorageSemantics(t *testing.T) {
	s := full()
	if s.SplitStorage() {
		t.Fatal("unset downloads root is not split")
	}
	if s.EffectiveDownloadsRoot() != s.DataRoot {
		t.Fatal("effective root defaults to the data root")
	}
	s.DownloadsRoot = s.DataRoot
	if s.SplitStorage() {
		t.Fatal("downloads root equal to data root is not split")
	}
	s.DownloadsRoot = "/mnt/nvme/dl"
	if !s.SplitStorage() || s.EffectiveDownloadsRoot() != "/mnt/nvme/dl" {
		t.Fatalf("split semantics: %v %s", s.SplitStorage(), s.EffectiveDownloadsRoot())
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	// Round-trips like every other field.
	p := filepath.Join(t.TempDir(), "arrsenal.yaml")
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DownloadsRoot != "/mnt/nvme/dl" {
		t.Fatalf("lost on round-trip: %q", loaded.DownloadsRoot)
	}
	// And obeys the charset rules.
	s.DownloadsRoot = "/mnt/a b"
	if err := s.Validate(); err == nil {
		t.Fatal("space in downloads_root must be rejected")
	}
}

func TestV1StateFilesStillLoad(t *testing.T) {
	// A file written by a v0.1/v0.2 binary (schema v1) loads under v2:
	// downgrades are gated, upgrades are seamless.
	p := filepath.Join(t.TempDir(), "arrsenal.yaml")
	v1 := `version: 1
apps: [sonarr]
puid: 1000
pgid: 1000
tz: Etc/UTC
umask: "002"
data_root: /data
appdata_root: /opt/appdata
gpu: none
`
	if err := os.WriteFile(p, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(p)
	if err != nil {
		t.Fatalf("v1 file must load under CurrentVersion=%d: %v", CurrentVersion, err)
	}
	if s.SplitStorage() {
		t.Fatal("v1 files are single-root by definition")
	}
	// Saving upgrades the stamp so old binaries refuse (never silently drop
	// newer fields).
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Version != CurrentVersion {
		t.Fatalf("saved version = %d, want %d", upgraded.Version, CurrentVersion)
	}
}

func TestTRaSHValidation(t *testing.T) {
	s := full() // has sonarr + radarr
	s.TRaSH = TRaSH{Enabled: true, Resolution: "1080p", Source: "bluray-web", Anime: true}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.TRaSH.Resolution = "480i"
	if err := s.Validate(); err == nil {
		t.Fatal("bad resolution must be rejected")
	}
	s.TRaSH = TRaSH{Enabled: true, Resolution: "1080p", Source: "bluray-web"}
	s.Apps = []string{"jellyfin"} // no arrs
	if err := s.Validate(); err == nil {
		t.Fatal("trash without an eligible arr must be rejected")
	}
	// Round-trips.
	s = full()
	s.TRaSH = TRaSH{Enabled: true, Resolution: "2160p", Source: "remux"}
	p := filepath.Join(t.TempDir(), "arrsenal.yaml")
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(p)
	if err != nil || !loaded.TRaSH.Enabled || loaded.TRaSH.Resolution != "2160p" {
		t.Fatalf("round-trip: %+v %v", loaded.TRaSH, err)
	}
}

func TestSaveRefusesInvalidState(t *testing.T) {
	s := full()
	s.GPU = "voodoo2"
	p := filepath.Join(t.TempDir(), "arrsenal.yaml")
	if err := s.Save(p); err == nil {
		t.Fatal("invalid state must never reach disk")
	}
	if _, statErr := os.Stat(p); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("failed save must not leave a file")
	}
}

func lookApp(t *testing.T, id string) (registry.App, bool) {
	t.Helper()
	a, found := registry.ByID(id)
	if !found {
		t.Fatalf("registry lost %q", id)
	}
	return a, true
}
