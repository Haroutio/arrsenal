package registry

import (
	"strings"
	"testing"
)

func TestRegistryIsValid(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMenuIsExactlyTheAgreedSet(t *testing.T) {
	// v0.1's ten plus v0.3's media-server choice (#26): Plex, Emby, Overseerr.
	want := []string{
		"jellyfin", "plex", "emby", "jellyseerr", "overseerr",
		"prowlarr", "sonarr", "radarr",
		"lidarr", "bazarr", "sabnzbd", "qbittorrent", "homepage",
	}
	all := All()
	if len(all) != len(want) {
		t.Fatalf("registry has %d apps, want %d", len(all), len(want))
	}
	for i, id := range want {
		if all[i].ID != id {
			t.Errorf("menu position %d = %q, want %q", i, all[i].ID, id)
		}
	}
}

func TestByID(t *testing.T) {
	a, ok := ByID("sonarr")
	if !ok || a.Name != "Sonarr" {
		t.Fatalf("ByID(sonarr) = %+v, %v", a, ok)
	}
	if _, ok := ByID("readarr"); ok {
		t.Fatal("readarr must not be in the registry — retired upstream (DESIGN §3)")
	}
	if _, ok := ByID("pihole"); ok {
		t.Fatal("pihole must not be in the registry — not media automation (DESIGN §3)")
	}
}

func TestRoleQueries(t *testing.T) {
	pvrs := ByRole(RolePVR)
	if len(pvrs) != 3 {
		t.Fatalf("got %d PVRs, want 3 (sonarr, radarr, lidarr)", len(pvrs))
	}
	dls := ByRole(RoleDownloadClient)
	if len(dls) != 2 {
		t.Fatalf("got %d download clients, want 2 (sabnzbd, qbittorrent)", len(dls))
	}
	ms := ByRole(RoleMediaServer)
	if len(ms) != 3 || ms[0].ID != "jellyfin" {
		t.Fatalf("media servers = %+v, want jellyfin (flagship, first), plex, emby", ms)
	}
	req := ByRole(RoleRequests)
	if len(req) != 2 {
		t.Fatalf("requests apps = %+v, want jellyseerr + overseerr", req)
	}
	// The paywall honesty contract (#26): paid-transcode servers must say so
	// at selection time.
	for _, id := range []string{"plex", "emby"} {
		a, _ := ByID(id)
		found := false
		for _, w := range a.Warnings {
			if strings.Contains(w, "free") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s must warn about paid hardware transcoding", id)
		}
	}
}

func TestBootPhasePartition(t *testing.T) {
	// DESIGN §7: Bazarr and Homepage are file-driven and start after core
	// keys are known; everything else is core.
	tail := map[string]bool{}
	for _, a := range ByPhase(BootTail) {
		tail[a.ID] = true
	}
	if len(tail) != 2 || !tail["bazarr"] || !tail["homepage"] {
		t.Fatalf("tail phase = %v, want exactly {bazarr, homepage}", tail)
	}
	if len(ByPhase(BootCore))+len(ByPhase(BootTail)) != len(All()) {
		t.Fatal("boot phases must partition the registry")
	}
}

func TestHardlinkLayoutInvariant(t *testing.T) {
	// The core design promise (DESIGN §5): every PVR mounts the FULL data
	// root at /data, and every download client mounts its download tree at
	// the same path inside the container as the PVRs see it (/data/<sub>).
	// Break this and imports silently fall back to copy+delete.
	for _, a := range ByRole(RolePVR) {
		found := false
		for _, m := range a.Mounts {
			if m.Kind == SourceData && m.Sub == "" && m.Target == "/data" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: PVRs must mount the full data root at /data", a.ID)
		}
	}
	for _, a := range ByRole(RoleDownloadClient) {
		found := false
		for _, m := range a.Mounts {
			if m.Kind != SourceData {
				continue
			}
			found = true
			if m.Target != "/data/"+m.Sub {
				t.Errorf("%s: download mount %q must be at /data/%s inside the container, got %q",
					a.ID, m.Sub, m.Sub, m.Target)
			}
		}
		if !found {
			t.Errorf("%s: download clients must mount their download tree under the data root, or imports copy instead of hardlink", a.ID)
		}
	}
}

func TestOnlyMediaServersTakeTheGPU(t *testing.T) {
	for _, a := range All() {
		if a.GPU != (a.Role == RoleMediaServer) {
			t.Errorf("%s: GPU = %v — exactly the media servers are GPU-capable", a.ID, a.GPU)
		}
	}
}

func TestGeneratedArtifactsNeverHardcodeAppKnowledge(t *testing.T) {
	// Guard for the acceptance criterion "single source of truth": every app
	// must carry enough for generation without special cases — an image ref,
	// a web port, and at least a config mount. (Validate covers the rest.)
	for _, a := range All() {
		if a.Tag == "" {
			t.Errorf("%s: no image tag", a.ID)
		}
		if a.Web.Purpose == "" {
			t.Errorf("%s: web port needs a purpose label for conflict messages", a.ID)
		}
	}
}

func TestLookupsDoNotAliasTheRegistry(t *testing.T) {
	// Consumers mutate what lookups return (generate merges env, preflight
	// remaps ports). None of that may write through to the registry.
	a, _ := ByID("homepage")
	a.Env["MUTATED"] = "yes"
	a.Mounts[0].Target = "/mutated"
	b, _ := ByID("homepage")
	if _, leaked := b.Env["MUTATED"]; leaked {
		t.Fatal("Env mutation leaked into the registry")
	}
	if b.Mounts[0].Target == "/mutated" {
		t.Fatal("Mount mutation leaked into the registry")
	}
	// Env must be non-nil in clones even when the entry declares none,
	// so consumers can merge without a nil check.
	s, _ := ByID("sonarr")
	if s.Env == nil {
		t.Fatal("clone must allocate Env")
	}
	s.Env["PUID"] = "1000" // must not panic
}
