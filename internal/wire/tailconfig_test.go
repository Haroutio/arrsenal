package wire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/Haroutio/arrsenal/internal/registry"
)

func TestBazarrConfigBothArrs(t *testing.T) {
	got := BazarrConfig(
		&ArrConn{Host: "sonarr", Port: 8989, APIKey: "sk"},
		&ArrConn{Host: "radarr", Port: 7878, APIKey: "rk"},
	)
	// Must parse and carry the connection Bazarr proved it consumes live.
	var cfg map[string]any
	if err := yaml.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("generated config must parse: %v\n%s", err, got)
	}
	gen := cfg["general"].(map[string]any)
	if gen["use_sonarr"] != true || gen["use_radarr"] != true {
		t.Fatalf("enable flags: %v", gen)
	}
	son := cfg["sonarr"].(map[string]any)
	if son["ip"] != "sonarr" || son["port"] != uint64(8989) && son["port"] != int64(8989) && son["port"] != 8989 {
		t.Fatalf("sonarr section: %v", son)
	}
	if son["apikey"] != "sk" {
		t.Fatalf("sonarr key missing: %v", son)
	}
}

func TestBazarrConfigSonarrOnly(t *testing.T) {
	got := BazarrConfig(&ArrConn{Host: "sonarr", Port: 8989, APIKey: "sk"}, nil)
	var cfg map[string]any
	if err := yaml.Unmarshal(got, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, hasRadarr := cfg["radarr"]; hasRadarr {
		t.Fatal("radarr section must be absent when Radarr is not selected")
	}
	if cfg["general"].(map[string]any)["use_radarr"] != false {
		t.Fatal("use_radarr must be false")
	}
}

func TestHomepageServicesGroupingAndWidgets(t *testing.T) {
	apps := func(id string) registry.App {
		a, _ := registry.ByID(id)
		return a
	}
	services := BuildHomepageServices([]HomepageInput{
		{App: apps("jellyfin"), HostURL: "http://host:8096", Key: "jk"},
		{App: apps("sonarr"), HostURL: "http://host:8989", Key: "sk"},
		{App: apps("qbittorrent"), HostURL: "http://host:8081", Username: "admin", Password: "qpass"},
		{App: apps("homepage"), HostURL: "http://host:3000"}, // must be skipped
	})

	// Homepage tile excluded.
	for _, s := range services {
		if s.Name == "Homepage" {
			t.Fatal("Homepage must not be a tile on its own dashboard")
		}
	}

	yamlOut := HomepageServices(services)
	var doc []map[string][]map[string]map[string]any
	if err := yaml.Unmarshal(yamlOut, &doc); err != nil {
		t.Fatalf("services.yaml must parse: %v\n%s", err, yamlOut)
	}

	flat := map[string]map[string]any{}
	group := map[string]string{}
	for _, grp := range doc {
		for gname, items := range grp {
			for _, item := range items {
				for svcName, fields := range item {
					flat[svcName] = fields
					group[svcName] = gname
				}
			}
		}
	}
	if group["Jellyfin"] != "Media" || group["Sonarr"] != "Management" || group["qBittorrent"] != "Downloads" {
		t.Fatalf("grouping wrong: %v", group)
	}
	// API-key widget.
	sw := flat["Sonarr"]["widget"].(map[string]any)
	if sw["type"] != "sonarr" || sw["url"] != "http://sonarr:8989" || sw["key"] != "sk" {
		t.Fatalf("sonarr widget: %v", sw)
	}
	// qBittorrent uses username/password, not key.
	qw := flat["qBittorrent"]["widget"].(map[string]any)
	if qw["username"] != "admin" || qw["password"] != "qpass" {
		t.Fatalf("qbittorrent widget auth: %v", qw)
	}
	if _, hasKey := qw["key"]; hasKey {
		t.Fatal("qBittorrent widget must not carry an API key")
	}
	// Container URL, not the host URL.
	if flat["Jellyfin"]["href"] != "http://host:8096" {
		t.Fatalf("href should be the host URL: %v", flat["Jellyfin"]["href"])
	}
}

func TestHomepageServicesDeterministic(t *testing.T) {
	in := []HomepageInput{
		{App: mustApp(t, "sabnzbd"), HostURL: "h", Key: "k"},
		{App: mustApp(t, "jellyfin"), HostURL: "h", Key: "k"},
		{App: mustApp(t, "sonarr"), HostURL: "h", Key: "k"},
	}
	first := HomepageServices(BuildHomepageServices(in))
	for i := 0; i < 10; i++ {
		if string(HomepageServices(BuildHomepageServices(in))) != string(first) {
			t.Fatal("services.yaml must be deterministic")
		}
	}
	// Group order: Media before Management before Downloads.
	s := string(first)
	if strings.Index(s, "Media") > strings.Index(s, "Management") ||
		strings.Index(s, "Management") > strings.Index(s, "Downloads") {
		t.Fatalf("group order not stable:\n%s", s)
	}
}

func mustApp(t *testing.T, id string) registry.App {
	t.Helper()
	a, ok := registry.ByID(id)
	if !ok {
		t.Fatalf("registry lost %s", id)
	}
	return a
}

func TestWriteTailConfigNeverClobbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")

	r := WriteTailConfig(path, []byte("fresh"), 0o600, "Bazarr config")
	if r.Outcome != OutcomeWired {
		t.Fatalf("fresh write: %+v", r)
	}
	if b, _ := os.ReadFile(path); string(b) != "fresh" {
		t.Fatalf("content: %s", b)
	}

	// Second call must not overwrite — the adoption iron rule.
	r = WriteTailConfig(path, []byte("REGENERATED"), 0o600, "Bazarr config")
	if r.Outcome != OutcomeExisted {
		t.Fatalf("existing file must be left alone: %+v", r)
	}
	if b, _ := os.ReadFile(path); string(b) != "fresh" {
		t.Fatalf("existing content was clobbered: %s", b)
	}
}

func TestWriteTailConfigSecretMode(t *testing.T) {
	if os.Getenv("SKIP_PERM") != "" {
		t.Skip()
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "services.yaml")
	WriteTailConfig(path, []byte("widget: secret"), 0o600, "Homepage")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows dev has no POSIX perms; the CI/Linux run is the gate.
	if info.Mode().Perm()&0o077 != 0 && os.Getenv("OS") == "" {
		t.Fatalf("secret-bearing config too open: %o", info.Mode().Perm())
	}
}
