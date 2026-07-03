package preflight

import (
	"net"
	"strings"
	"testing"

	"github.com/Haroutio/arrsenal/internal/dockerx"
	"github.com/Haroutio/arrsenal/internal/state"
)

func fakeDeps(containers map[string]string, busyPorts map[int]bool, nonEmpty map[string]bool) ScanDeps {
	return ScanDeps{
		Containers: func() (map[string]string, error) { return containers, nil },
		PortFree:   func(port int, _ string) bool { return !busyPorts[port] },
		AppdataNonEmpty: func(dir string) bool {
			for suffix := range nonEmpty {
				if strings.HasSuffix(dir, suffix) {
					return true
				}
			}
			return false
		},
	}
}

func scanState(apps ...string) *state.State {
	s := state.New()
	s.Apps = apps
	return s
}

func TestScanCleanSystemHasNoFindings(t *testing.T) {
	got, err := ScanConflicts(scanState("sonarr", "sabnzbd"), fakeDeps(nil, nil, nil))
	if err != nil || len(got) != 0 {
		t.Fatalf("clean system: %v, %v", got, err)
	}
}

func TestScanFlagsForeignContainersButNotOurs(t *testing.T) {
	containers := map[string]string{
		"sonarr": "someones-stack", // foreign compose project
		"radarr": ComposeProject,   // ours, previous run
		"lidarr": "",               // not compose-managed at all
	}
	got, err := ScanConflicts(scanState("sonarr", "radarr", "lidarr"), fakeDeps(containers, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	byApp := map[string][]Conflict{}
	for _, c := range got {
		byApp[c.App] = append(byApp[c.App], c)
	}
	if len(byApp["sonarr"]) != 1 || byApp["sonarr"][0].Kind != KindContainerName {
		t.Errorf("foreign-project sonarr must be a name conflict: %+v", byApp["sonarr"])
	}
	if !strings.Contains(byApp["sonarr"][0].Detail, "someones-stack") {
		t.Errorf("detail should name the other project: %s", byApp["sonarr"][0].Detail)
	}
	if len(byApp["radarr"]) != 0 {
		t.Errorf("our own container from a previous run is not a conflict: %+v", byApp["radarr"])
	}
	if len(byApp["lidarr"]) != 1 || byApp["lidarr"][0].Kind != KindContainerName {
		t.Errorf("non-compose container must be a name conflict: %+v", byApp["lidarr"])
	}
}

func TestScanFlagsBusyPortsExceptOurOwn(t *testing.T) {
	busy := map[int]bool{8989: true, 8080: true}
	// sonarr's container is ours and running → it owns 8989, no conflict;
	// sabnzbd is fresh and 8080 is taken by something → conflict.
	containers := map[string]string{"sonarr": ComposeProject}
	got, err := ScanConflicts(scanState("sonarr", "sabnzbd"), fakeDeps(containers, busy, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly one conflict, got %+v", got)
	}
	c := got[0]
	if c.App != "sabnzbd" || c.Kind != KindPort || c.Port != 8080 || !c.Blocking() {
		t.Fatalf("unexpected conflict: %+v", c)
	}
}

func TestScanChecksEffectiveRemappedPorts(t *testing.T) {
	s := scanState("sonarr")
	s.PortRemaps = map[string]map[int]int{"sonarr": {8989: 9989}}
	busy := map[int]bool{8989: true} // old default busy — irrelevant after remap
	got, err := ScanConflicts(s, fakeDeps(nil, busy, nil))
	if err != nil || len(got) != 0 {
		t.Fatalf("remapped port must be the one probed: %+v %v", got, err)
	}
	busy[9989] = true
	got, _ = ScanConflicts(s, fakeDeps(nil, busy, nil))
	if len(got) != 1 || got[0].Port != 9989 {
		t.Fatalf("effective port 9989 busy must be flagged: %+v", got)
	}
}

func TestScanSkipsPortsForHostNetworkedApps(t *testing.T) {
	s := scanState("jellyfin")
	s.JellyfinHostNetwork = true
	busy := map[int]bool{8096: true}
	got, err := ScanConflicts(s, fakeDeps(nil, busy, nil))
	if err != nil {
		t.Fatal(err)
	}
	// Host networking binds directly; if 8096 is busy the culprit may BE
	// jellyfin itself or a real conflict — either way compose can't remap
	// it, so the scan stays quiet and `up` reports honestly.
	if len(got) != 0 {
		t.Fatalf("host-networked apps have no publishable ports to scan: %+v", got)
	}
}

func TestScanReportsAppdataAdoptionAsNonBlocking(t *testing.T) {
	got, err := ScanConflicts(scanState("sonarr"), fakeDeps(nil, nil, map[string]bool{"sonarr": true}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindAppdata || got[0].Blocking() {
		t.Fatalf("existing appdata is an adoption notice, not a blocker: %+v", got)
	}
	if !strings.Contains(got[0].Detail, "adopt") {
		t.Fatalf("notice should explain adoption: %s", got[0].Detail)
	}
}

func TestPortFreeProbesForReal(t *testing.T) {
	// Bind the wildcard address — same address the probe uses — so the
	// collision is visible on every OS's bind semantics.
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port
	if portFree(port, "tcp") {
		t.Fatalf("port %d is bound by this test yet reported free", port)
	}
	_ = l.Close()
	if !portFree(port, "tcp") {
		t.Fatalf("port %d released yet reported busy", port)
	}
}

func TestDockerxContainersParsing(t *testing.T) {
	fake := dockerx.NewWithRunner(func(_ ...string) (string, error) {
		return "sonarr\tarrsenal\nplex\t\nqbittorrent\tsomeones-stack\n\n", nil
	})
	got, err := fake.Containers()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"sonarr": "arrsenal", "plex": "", "qbittorrent": "someones-stack"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s → %q, want %q", k, got[k], v)
		}
	}
}
