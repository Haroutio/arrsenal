package generate

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Haroutio/arrsenal/internal/registry"
	"github.com/Haroutio/arrsenal/internal/state"
)

// go test ./internal/generate/ -update rewrites the golden files. A change to
// generation with no golden diff in the PR means the tests didn't cover it.
var update = flag.Bool("update", false, "rewrite golden files")

func baseState() *state.State {
	s := state.New()
	s.TZ = "America/Los_Angeles"
	return s
}

func goldenCases() map[string]*state.State {
	minimal := baseState()
	minimal.Apps = []string{"sonarr", "sabnzbd"}

	// Built from the registry, not a hardcoded list: when the menu grows,
	// the flagship golden grows with it.
	full := baseState()
	for _, a := range registry.All() {
		full.Apps = append(full.Apps, a.ID)
	}

	remapped := baseState()
	remapped.Apps = []string{"sonarr", "qbittorrent"}
	remapped.PortRemaps = map[string]map[int]int{
		"qbittorrent": {8081: 9091, 6881: 16881},
		"sonarr":      {8989: 9989},
	}

	nvidia := baseState()
	nvidia.Apps = []string{"jellyfin"}
	nvidia.GPU = state.GPUNvidia

	intel := baseState()
	intel.Apps = []string{"jellyfin"}
	intel.GPU = state.GPUIntel

	amd := baseState()
	amd.Apps = []string{"jellyfin"}
	amd.GPU = state.GPUAMD

	hostnet := baseState()
	hostnet.Apps = []string{"jellyfin", "jellyseerr"}
	hostnet.JellyfinHostNetwork = true

	// Issue #59: downloads on their own filesystem (NVMe scratch), media on
	// the array. Container paths stay identical to the single-root layout.
	split := baseState()
	split.Apps = []string{"jellyfin", "sonarr", "sabnzbd", "qbittorrent"}
	split.DataRoot = "/mnt/pool/data"
	split.DownloadsRoot = "/mnt/nvme/downloads"

	// Issue #27: gluetun fronting qBittorrent.
	vpn := baseState()
	vpn.Apps = []string{"sonarr", "qbittorrent"}
	vpn.VPN = state.VPN{Provider: "mullvad", Countries: "Netherlands"}
	vpn.Secrets.WireguardPrivateKey = "wg-key-SECRET"

	return map[string]*state.State{
		"minimal":          minimal,
		"full-stack":       full,
		"remapped-ports":   remapped,
		"gpu-nvidia":       nvidia,
		"gpu-intel":        intel,
		"gpu-amd":          amd,
		"jellyfin-hostnet": hostnet,
		"split-storage":    split,
		"vpn-qbittorrent":  vpn,
	}
}

func TestVPNComposeShape(t *testing.T) {
	got := render(t, goldenCases()["vpn-qbittorrent"])
	var doc struct {
		Services map[string]struct {
			NetworkMode string            `yaml:"network_mode"`
			DependsOn   []string          `yaml:"depends_on"`
			CapAdd      []string          `yaml:"cap_add"`
			EnvFile     []string          `yaml:"env_file"`
			Ports       []string          `yaml:"ports"`
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yamlUnmarshal(got.Compose, &doc); err != nil {
		t.Fatal(err)
	}
	qb := doc.Services["qbittorrent"]
	if qb.NetworkMode != "service:gluetun" || len(qb.Ports) != 0 {
		t.Fatalf("qbittorrent must live in gluetun's netns with no own ports: %+v", qb)
	}
	if len(qb.DependsOn) != 1 || qb.DependsOn[0] != "gluetun" {
		t.Fatalf("qbittorrent must depend on gluetun: %+v", qb.DependsOn)
	}
	gt := doc.Services["gluetun"]
	if gt.Environment["VPN_SERVICE_PROVIDER"] != "mullvad" || gt.Environment["SERVER_COUNTRIES"] != "Netherlands" {
		t.Fatalf("gluetun env: %v", gt.Environment)
	}
	if len(gt.CapAdd) != 1 || gt.CapAdd[0] != "NET_ADMIN" {
		t.Fatalf("gluetun caps: %v", gt.CapAdd)
	}
	if len(gt.EnvFile) != 1 || gt.EnvFile[0] != "${APPDATA}/gluetun/credentials.env" {
		t.Fatalf("credentials must ride the 0600 env-file: %v", gt.EnvFile)
	}
	found := false
	for _, p := range gt.Ports {
		if p == "8081:8081" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gluetun must publish qBittorrent's web port: %v", gt.Ports)
	}

	// The tunnel key must never reach the artifacts (they are 0644).
	for name, b := range map[string][]byte{"compose": got.Compose, "env": got.Env} {
		if strings.Contains(string(b), "wg-key-SECRET") {
			t.Fatalf("%s leaked the wireguard key", name)
		}
	}
}

func TestSplitStorageMounts(t *testing.T) {
	got := render(t, goldenCases()["split-storage"])
	var doc struct {
		Services map[string]struct {
			Volumes []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yamlUnmarshal(got.Compose, &doc); err != nil {
		t.Fatal(err)
	}
	has := func(svc, vol string) bool {
		for _, v := range doc.Services[svc].Volumes {
			if v == vol {
				return true
			}
		}
		return false
	}
	// PVR: the full-root mount expands into the three fixed container paths.
	for _, want := range []string{
		"${DATA}/media:/data/media",
		"${DOWNLOADS}/usenet:/data/usenet",
		"${DOWNLOADS}/torrents:/data/torrents",
	} {
		if !has("sonarr", want) {
			t.Errorf("sonarr missing %q: %v", want, doc.Services["sonarr"].Volumes)
		}
	}
	if has("sonarr", "${DATA}:/data") {
		t.Error("split storage must not emit the spanning single mount")
	}
	// Download clients follow the downloads root; media stays on data.
	if !has("sabnzbd", "${DOWNLOADS}/usenet:/data/usenet") {
		t.Errorf("sabnzbd: %v", doc.Services["sabnzbd"].Volumes)
	}
	if !has("qbittorrent", "${DOWNLOADS}/torrents:/data/torrents") {
		t.Errorf("qbittorrent: %v", doc.Services["qbittorrent"].Volumes)
	}
	if !has("jellyfin", "${DATA}/media:/media") {
		t.Errorf("jellyfin: %v", doc.Services["jellyfin"].Volumes)
	}
	// .env carries both roots.
	env := string(got.Env)
	if !strings.Contains(env, "DATA=/mnt/pool/data\n") || !strings.Contains(env, "DOWNLOADS=/mnt/nvme/downloads\n") {
		t.Fatalf(".env roots:\n%s", env)
	}
}

func render(t *testing.T, s *state.State) Artifacts {
	t.Helper()
	got, err := Render(s, state.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestGoldenFiles(t *testing.T) {
	for name, s := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			got := render(t, s)
			dir := filepath.Join("testdata", name)
			compare(t, filepath.Join(dir, "docker-compose.yml"), got.Compose)
			compare(t, filepath.Join(dir, ".env"), got.Env)
		})
	}
}

func compare(t *testing.T, golden string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v — run `go test ./internal/generate/ -update` after intentional changes", err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s drifted from golden; run with -update if intentional.\n--- want ---\n%s\n--- got ---\n%s",
			golden, want, got)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	s := goldenCases()["full-stack"]
	first, err := Render(s, state.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := Render(s, state.DefaultPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Compose, again.Compose) || !bytes.Equal(first.Env, again.Env) {
			t.Fatalf("iteration %d: identical state rendered different bytes", i)
		}
	}
}

func TestArtifactsNeverContainSecrets(t *testing.T) {
	const secret = "hunter2-SUPER-SECRET"
	s := goldenCases()["full-stack"]
	s.Secrets.QBittorrentPassword = secret
	got, err := Render(s, state.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"compose": got.Compose, "env": got.Env} {
		if strings.Contains(string(b), secret) {
			t.Errorf("%s contains the qBittorrent password — secrets are pre-seeded into app config, never into artifacts", name)
		}
	}
}

func TestGeneratedFilesCarryTheHeader(t *testing.T) {
	got, err := Render(goldenCases()["minimal"], state.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"compose": got.Compose, "env": got.Env} {
		if !bytes.HasPrefix(b, []byte("# GENERATED BY ARRSENAL — DO NOT EDIT.")) {
			t.Errorf("%s is missing the DO NOT EDIT header", name)
		}
		if !bytes.Contains(b, []byte("docker-compose.override.yml")) {
			t.Errorf("%s header must point at the override escape hatch", name)
		}
	}
}

func TestRenderRefusesBadInput(t *testing.T) {
	empty := baseState() // no apps
	if _, err := Render(empty, state.DefaultPath); err == nil {
		t.Fatal("empty selection must not render")
	}
	invalid := baseState()
	invalid.Apps = []string{"sonarr"}
	invalid.GPU = "voodoo2"
	if _, err := Render(invalid, state.DefaultPath); err == nil {
		t.Fatal("invalid state must not render")
	}
}

func TestComposeParsesAsYAMLAndHonorsChoices(t *testing.T) {
	// Structural spot-checks through a real YAML parse, not string matching.
	got, err := Render(goldenCases()["jellyfin-hostnet"], state.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]struct {
			NetworkMode string   `yaml:"network_mode"`
			Ports       []string `yaml:"ports"`
			Networks    []string `yaml:"networks"`
		} `yaml:"services"`
		Networks map[string]any `yaml:"networks"`
	}
	if err := yamlUnmarshal(got.Compose, &doc); err != nil {
		t.Fatalf("generated compose does not parse: %v", err)
	}
	jf := doc.Services["jellyfin"]
	if jf.NetworkMode != "host" || len(jf.Ports) != 0 || len(jf.Networks) != 0 {
		t.Fatalf("host-networked jellyfin must publish nothing and join no network: %+v", jf)
	}
	js := doc.Services["jellyseerr"]
	if js.NetworkMode != "" || len(js.Networks) != 1 {
		t.Fatalf("jellyseerr must stay on the bridge: %+v", js)
	}
	if _, ok := doc.Networks[NetworkName]; !ok {
		t.Fatalf("the %s network must exist", NetworkName)
	}
}

// parsedCompose is the reusable shape for structural assertions.
type parsedCompose struct {
	Services map[string]struct {
		User        string            `yaml:"user"`
		NetworkMode string            `yaml:"network_mode"`
		Environment map[string]string `yaml:"environment"`
		Ports       []string          `yaml:"ports"`
		Deploy      *struct {
			Resources struct {
				Reservations struct {
					Devices []struct {
						Driver string `yaml:"driver"`
					} `yaml:"devices"`
				} `yaml:"reservations"`
			} `yaml:"resources"`
		} `yaml:"deploy"`
	} `yaml:"services"`
}

func TestFullStackStructurally(t *testing.T) {
	// Independent of goldens (and therefore of -update): every selected app
	// must publish its effective web port, carry its identity contract, and
	// GPU shape must be exact. A golden regenerated over a broken renderer
	// still fails here.
	s := goldenCases()["full-stack"]
	s.GPU = state.GPUNvidia
	got := render(t, s)
	var doc parsedCompose
	if err := yamlUnmarshal(got.Compose, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Services) != len(registry.All()) {
		t.Fatalf("%d services for %d registry apps", len(doc.Services), len(registry.All()))
	}
	for _, app := range registry.All() {
		svc, ok := doc.Services[app.ID]
		if !ok {
			t.Fatalf("service %s missing", app.ID)
		}
		host, container := s.WebPorts(app)
		want := fmt.Sprintf("%d:%d", host, container)
		found := false
		for _, p := range svc.Ports {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: web mapping %q not published (ports: %v)", app.ID, want, svc.Ports)
		}
		switch app.Identity {
		case registry.IdentityEnvPUIDGID:
			for _, k := range []string{"PUID", "PGID", "UMASK"} {
				if svc.Environment[k] == "" {
					t.Errorf("%s: env identity app missing %s", app.ID, k)
				}
			}
		case registry.IdentityUserDirective:
			if svc.User != "${PUID}:${PGID}" {
				t.Errorf("%s: user directive = %q", app.ID, svc.User)
			}
		}
		if app.WebPortEnv != "" && svc.Environment[app.WebPortEnv] != fmt.Sprintf("%d", container) {
			t.Errorf("%s: %s = %q, want %d", app.ID, app.WebPortEnv, svc.Environment[app.WebPortEnv], container)
		}
		wantGPU := app.GPU // nvidia mode: GPU apps get the reservation
		hasGPU := svc.Deploy != nil && len(svc.Deploy.Resources.Reservations.Devices) == 1 &&
			svc.Deploy.Resources.Reservations.Devices[0].Driver == "nvidia"
		if wantGPU != hasGPU {
			t.Errorf("%s: nvidia reservation = %v, want %v", app.ID, hasGPU, wantGPU)
		}
	}
}

func TestRemappedQBittorrentMovesBothSidesAndEnv(t *testing.T) {
	// The LSIO image rejects asymmetric web mappings (CSRF/host-header), so
	// a remap must produce host:host with WEBUI_PORT following.
	got := render(t, goldenCases()["remapped-ports"])
	var doc parsedCompose
	if err := yamlUnmarshal(got.Compose, &doc); err != nil {
		t.Fatal(err)
	}
	qb := doc.Services["qbittorrent"]
	if qb.Environment["WEBUI_PORT"] != "9091" {
		t.Fatalf("WEBUI_PORT = %q, want 9091", qb.Environment["WEBUI_PORT"])
	}
	for _, p := range qb.Ports {
		if p == "9091:8081" {
			t.Fatal("asymmetric qBittorrent web mapping generated — this configuration rejects logins")
		}
	}
	found := false
	for _, p := range qb.Ports {
		if p == "9091:9091" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want 9091:9091 in ports, got %v", qb.Ports)
	}
}
