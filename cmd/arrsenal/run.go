package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/Haroutio/arrsenal/internal/dockerx"
	"github.com/Haroutio/arrsenal/internal/generate"
	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/quality"
	"github.com/Haroutio/arrsenal/internal/registry"
	"github.com/Haroutio/arrsenal/internal/state"
	"github.com/Haroutio/arrsenal/internal/tui"
	"github.com/Haroutio/arrsenal/internal/wire"
)

func run(o options) error {
	// Non-interactive sessions must fail with instructions, never hang on a
	// TUI that has no terminal to draw on (DESIGN §10).
	if !o.yes && !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("no terminal attached — for scripted use run with --yes and flags, e.g.\n" +
			"  arrsenal --yes --apps sonarr,radarr,prowlarr,sabnzbd --admin-pass ... [--data-root ... --downloads-root ... --gpu ...]\n" +
			"see arrsenal --help for every option")
	}

	s, fresh, err := loadOrNewState(o.statePath)
	if err != nil {
		return err
	}
	applyFlagOverrides(s, o, fresh)

	if o.yes {
		if err := headlessFill(s, o); err != nil {
			return err
		}
	} else if err := interactiveFill(s, &o); err != nil {
		return err
	}

	return pipeline(s, o)
}

func loadOrNewState(path string) (*state.State, bool, error) {
	s, err := state.Load(path)
	if errors.Is(err, state.ErrNotExist) {
		return state.New(), true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return s, false, nil
}

// applyFlagOverrides copies explicitly-useful flag values into the state.
// Zero-ish values mean "keep what the state has".
func applyFlagOverrides(s *state.State, o options, fresh bool) {
	if o.dataRoot != "" {
		s.DataRoot = o.dataRoot
	}
	if o.appdataRoot != "" {
		s.AppdataRoot = o.appdataRoot
	}
	if o.umask != "" {
		s.Umask = o.umask
	}
	if o.downloadsRoot != "" {
		s.DownloadsRoot = o.downloadsRoot
	}
	if o.jellyfinHostNet {
		s.JellyfinHostNetwork = true
	}
	if o.vpnProvider != "" {
		s.VPN.Provider = o.vpnProvider
	}
	if o.vpnKey != "" {
		s.Secrets.WireguardPrivateKey = o.vpnKey
	}
	if o.vpnCountries != "" {
		s.VPN.Countries = o.vpnCountries
	}
	if o.trash {
		s.TRaSH = state.TRaSH{
			Enabled:    true,
			Resolution: o.trashResolution,
			Source:     o.trashSource,
			Anime:      o.trashAnime,
		}
	}
	if o.tz != "" && (fresh || o.yes) {
		s.TZ = o.tz
	}
	if fresh || o.yes {
		s.PUID, s.PGID = o.puid, o.pgid
	}
	if o.gpu != "" {
		s.GPU = state.GPUMode(o.gpu)
	}
}

// headlessFill completes the state from flags alone (DESIGN.md §10).
func headlessFill(s *state.State, o options) error {
	if o.apps != "" {
		s.Apps = nil
		for _, id := range strings.Split(o.apps, ",") {
			if id = strings.TrimSpace(id); id != "" {
				s.Apps = append(s.Apps, id)
			}
		}
	}
	if len(s.Apps) == 0 {
		return errors.New("headless mode needs --apps (comma-separated IDs) or an existing state file")
	}
	if o.gpu == "" {
		det := preflight.DetectGPU(preflight.DefaultGPUProbes())
		s.GPU = det.Proposal
		fmt.Println("gpu:", det.Detail)
	}
	return s.Validate()
}

// interactiveFill drives the TUI screens in order, writing into the state
// (and the wiring credential into the options).
func interactiveFill(s *state.State, o *options) error {
	// 0. The boot splash — the readout is real (the same probes preflight
	// trusts), the galleon is morale. Any key skips.
	splash := tui.NewSplash(displayVersion(), splashRows())
	if err := runScreen(&splashAdapter{&splash}); err != nil {
		return err
	}
	if splash.Quit() {
		return errors.New("aborted")
	}

	// 1. App selection.
	sel := tui.NewSelect(s.Apps)
	if err := runScreen(&selectAdapter{&sel}); err != nil {
		return err
	}
	if sel.Quit() {
		return errors.New("aborted")
	}
	s.Apps = sel.Selected()
	for _, id := range sel.Removals() {
		fmt.Printf("note: %s will be removed; its config in %s is preserved\n", id, filepath.Join(s.AppdataRoot, id))
	}

	// 2. Identity & environment.
	set := tui.NewSettings(s)
	if err := runScreen(&settingsAdapter{&set, s}); err != nil {
		return err
	}
	if set.Quit() {
		return errors.New("aborted")
	}
	set.Apply(s)

	// 3. Storage.
	mounts, err := preflight.ListMounts()
	if err != nil {
		fmt.Println("warning:", err)
	}
	paths := tui.NewPaths(s, mounts)
	if err := runScreen(&pathsAdapter{&paths, s}); err != nil {
		return err
	}
	if paths.Quit() {
		return errors.New("aborted")
	}
	paths.Apply(s)

	// 4. The admin credential the wiring pass applies everywhere — collected
	// once, used, never persisted (DESIGN §9.2). Enter skips wiring auth
	// (those steps become manual report lines).
	if o.adminPass == "" {
		fmt.Printf("Admin username for the apps [%s]: ", o.adminUser)
		if line, err := stdinReader.ReadString('\n'); err == nil {
			if v := strings.TrimSpace(line); v != "" {
				o.adminUser = v
			}
		}
		fmt.Print("Admin password (applied to every app; enter to skip auto-setup): ")
		if pw, err := term.ReadPassword(int(os.Stdin.Fd())); err == nil {
			o.adminPass = string(pw)
		}
		fmt.Println()
	}

	// 5. Plex claim token (issue #26): only useful on a FRESH plex (an
	// adopted server is already claimed), only valid 4 minutes, so it is
	// asked for at the last responsible moment, skippable.
	if selectedID(s, "plex") && o.plexClaim == "" {
		if entries, err := os.ReadDir(filepath.Join(s.AppdataRoot, "plex")); err != nil || len(entries) == 0 {
			fmt.Println("Plex: get a claim token from https://www.plex.tv/claim (valid 4 minutes).")
			fmt.Print("Claim token (enter to skip — you can claim later in the web UI): ")
			if line, err := stdinReader.ReadString('\n'); err == nil {
				o.plexClaim = strings.TrimSpace(line)
			}
		}
	}

	// 5.5 TRaSH quality settings (issue #60): offered when an eligible arr
	// is selected and nothing is configured yet. Consent here IS the
	// adoption gate — on an existing arr this converges its TRaSH-named
	// profiles, which the prompt says out loud.
	if (selectedID(s, "sonarr") || selectedID(s, "radarr")) && !s.TRaSH.Enabled && !o.trash {
		if confirm("Apply TRaSH-guide quality settings to Sonarr/Radarr (recommended sizes, custom formats, profiles, naming scheme)?", false) {
			t := state.TRaSH{Enabled: true, Resolution: "1080p", Source: "bluray-web"}
			if confirm("Target 4K (2160p) instead of 1080p?", false) {
				t.Resolution = "2160p"
			}
			if confirm("Prefer full-quality remuxes over the standard Bluray/WEB tier (much larger files)?", false) {
				t.Source = "remux"
			}
			t.Anime = confirm("Also apply the anime profiles?", false)
			fmt.Println("note: this creates/updates the TRaSH-named quality profiles in your arrs on every run — existing custom profiles are untouched; fresh installs also get the guides' naming scheme")
			s.TRaSH = t
		}
	}

	// 5.7 Usenet provider (issue #103): the one setting without which the
	// stack downloads nothing. Only offered for a FRESH SABnzbd — an adopted
	// one already has the user's servers, or their deliberate absence.
	if selectedID(s, "sabnzbd") && o.usenetProvider == "" {
		if entries, err := os.ReadDir(filepath.Join(s.AppdataRoot, "sabnzbd")); err != nil || len(entries) == 0 {
			if confirm("Add your usenet provider to SABnzbd now (Newshosting, Eweka, UsenetServer, Frugal, Easynews, or a hostname)?", false) {
				fmt.Print("Provider [newshosting]: ")
				if line, err := stdinReader.ReadString('\n'); err == nil {
					o.usenetProvider = strings.TrimSpace(line)
				}
				if o.usenetProvider == "" {
					o.usenetProvider = "newshosting"
				}
				fmt.Print("Provider username: ")
				if line, err := stdinReader.ReadString('\n'); err == nil {
					o.usenetUser = strings.TrimSpace(line)
				}
				fmt.Print("Provider password: ")
				if pw, err := term.ReadPassword(int(os.Stdin.Fd())); err == nil {
					o.usenetPass = strings.TrimSpace(string(pw))
				}
				fmt.Println()
			}
		}
	}

	// 5.8 Indexers (issue #104): almost every usenet indexer is generic
	// Newznab — URL plus API key — and Prowlarr propagates them to every
	// arr. Prowlarr validates on save, so typos surface in the report.
	if selectedID(s, "prowlarr") && o.indexerName == "" {
		for confirm("Add a usenet indexer to Prowlarr now (most are Newznab: URL + API key)?", false) {
			var ix wire.NewznabIndexer
			fmt.Print("Indexer name: ")
			if line, err := stdinReader.ReadString('\n'); err == nil {
				ix.Name = strings.TrimSpace(line)
			}
			fmt.Print("Indexer URL: ")
			if line, err := stdinReader.ReadString('\n'); err == nil {
				ix.URL = strings.TrimSpace(line)
			}
			fmt.Print("Indexer API key: ")
			if line, err := stdinReader.ReadString('\n'); err == nil {
				ix.APIKey = strings.TrimSpace(line)
			}
			if ix.Name != "" && ix.URL != "" && ix.APIKey != "" {
				o.indexers = append(o.indexers, ix)
			} else {
				fmt.Println("incomplete — skipped (all three fields are needed)")
			}
		}
	}

	// 6. VPN for qBittorrent (issue #27): only offered when qBittorrent is
	// selected and nothing is configured yet; flags outrank the prompt.
	if selectedID(s, "qbittorrent") && !s.VPNEnabled() && o.vpnProvider == "" {
		if confirm("Route qBittorrent through a VPN (gluetun, WireGuard)?", false) {
			fmt.Print("VPN provider (gluetun name, e.g. mullvad, protonvpn): ")
			if line, err := stdinReader.ReadString('\n'); err == nil {
				s.VPN.Provider = strings.TrimSpace(line)
			}
			fmt.Print("WireGuard private key: ")
			if pw, err := term.ReadPassword(int(os.Stdin.Fd())); err == nil {
				s.Secrets.WireguardPrivateKey = strings.TrimSpace(string(pw))
			}
			fmt.Println()
			fmt.Print("Server countries (optional, comma-separated): ")
			if line, err := stdinReader.ReadString('\n'); err == nil {
				s.VPN.Countries = strings.TrimSpace(line)
			}
			fmt.Println("note: gluetun's kill switch means a dropped tunnel takes qBittorrent offline until it reconnects")
		}
	}

	// 6. GPU: an explicit --gpu flag outranks detection entirely; otherwise
	// detection proposes and the user disposes — including a manual pick
	// when the probes miss hardware the machine's owner knows is there.
	if o.gpu != "" {
		fmt.Printf("gpu: %s (set by --gpu)\n", s.GPU)
	} else {
		det := preflight.DetectGPU(preflight.DefaultGPUProbes())
		fmt.Println(det.Detail)
		if det.ToolkitInstallHint != "" {
			fmt.Println(det.ToolkitInstallHint)
			fmt.Print(preflight.FormatToolkitPlan())
		}
		if det.Proposal != state.GPUNone && confirm(fmt.Sprintf("Use the detected GPU (%s)?", det.Proposal), true) {
			s.GPU = det.Proposal
		} else {
			s.GPU = askGPU()
		}
		// Echo the outcome: a bare enter at the manual prompt used to pick
		// "none" without saying so, and nobody noticed until Jellyfin came
		// up without NVENC.
		fmt.Printf("gpu: %s\n", s.GPU)
	}

	return s.Validate()
}

// askGPU is the manual override: detection is a convenience, never a wall.
func askGPU() state.GPUMode {
	fmt.Print("GPU mode — none, nvidia, intel (QuickSync), amd (VAAPI) [none]: ")
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return state.GPUNone
	}
	return parseGPUAnswer(line)
}

func parseGPUAnswer(line string) state.GPUMode {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "nvidia":
		return state.GPUNvidia
	case "intel":
		return state.GPUIntel
	case "amd":
		return state.GPUAMD
	default:
		return state.GPUNone
	}
}

// pipeline is the shared back half: scan → tree → checks → render → up.
func pipeline(s *state.State, o options) error {
	docker := dockerx.New()
	if err := docker.Available(); err != nil {
		return err
	}

	// Conflict scan; interactive mode gets the remap screen, headless fails.
	conflicts, err := preflight.ScanConflicts(s, preflight.DefaultDeps(docker.Containers))
	if err != nil {
		return err
	}
	var blocking []preflight.Conflict
	for _, c := range conflicts {
		if c.Blocking() {
			blocking = append(blocking, c)
		} else {
			fmt.Println("note:", c.Detail)
		}
	}
	if len(blocking) > 0 {
		if o.yes {
			var lines []string
			for _, c := range blocking {
				lines = append(lines, "  "+c.Detail)
			}
			return fmt.Errorf("blocking conflicts:\n%s", strings.Join(lines, "\n"))
		}
		remap := tui.NewRemap(blocking, s)
		if err := runScreen(&remapAdapter{&remap, s}); err != nil {
			return err
		}
		if remap.Quit() || remap.Back() {
			return errors.New("aborted — adjust your selection and re-run")
		}
		remap.Apply(s)
	}

	// Save early: the state is the source of truth from here on.
	if err := s.Save(o.statePath); err != nil {
		return err
	}

	dirs, err := preflight.EnsureTree(s)
	if err != nil {
		return err
	}
	created := 0
	for _, d := range dirs {
		if d.Created {
			created++
		}
	}
	fmt.Printf("directories: %d verified, %d created\n", len(dirs)-created, created)

	hl := preflight.CheckHardlink(filepath.Join(s.EffectiveDownloadsRoot(), "usenet"), filepath.Join(s.DataRoot, "media"))
	switch {
	case hl.OK:
		fmt.Println(hl.Detail)
	case s.SplitStorage():
		// The split is an informed choice (issue #59): downloads on scratch,
		// media on the array. Copy-mode is the accepted tradeoff, not a
		// misconfiguration — no scary warning, no confirmation gate.
		fmt.Println("downloads and media are on separate filesystems (as configured) — imports will copy, not hardlink")
	default:
		fmt.Println("warning:", hl.Detail)
		if !o.yes && !confirm("Continue with copy-mode imports?", false) {
			return errors.New("aborted")
		}
	}
	if se := preflight.CheckSELinux(); se.Enforcing {
		fmt.Println("warning:", se.Warning)
	}

	// qBittorrent's pre-seed happens BEFORE its first start (DESIGN §7):
	// generate + persist the password, write the config only if absent.
	if selectedID(s, "qbittorrent") {
		if s.Secrets.QBittorrentPassword == "" {
			pw, err := wire.GeneratePassword()
			if err != nil {
				return fmt.Errorf("generating qBittorrent password: %w", err)
			}
			s.Secrets.QBittorrentPassword = pw
			if err := s.Save(o.statePath); err != nil {
				return err
			}
		}
		conf, err := wire.QBitConfig(s.Secrets.QBittorrentPassword)
		if err != nil {
			return fmt.Errorf("rendering qBittorrent pre-seed: %w", err)
		}
		r := wire.WriteTailConfig(
			filepath.Join(s.AppdataRoot, "qbittorrent", "qBittorrent", "qBittorrent.conf"),
			conf, 0o600, s.PUID, s.PGID, "qBittorrent ← pre-seeded WebUI password")
		fmt.Printf("%s: %s %s\n", r.Connection, r.Outcome, r.Detail)
	}

	// Bazarr's API key is pre-seeded the same way (issue #107): generated
	// once, persisted, written into the config.yaml the wiring pass renders.
	// The language pre-seed authenticates with it after Bazarr boots.
	if selectedID(s, "bazarr") && s.Secrets.BazarrAPIKey == "" {
		key, err := wire.GeneratePassword()
		if err != nil {
			return fmt.Errorf("generating Bazarr API key: %w", err)
		}
		s.Secrets.BazarrAPIKey = key
		if err := s.Save(o.statePath); err != nil {
			return err
		}
	}

	// Plex's claim token is per-run (4-minute expiry): written into a 0600
	// env-file the compose service references, empty when none was given.
	// After the first claimed boot the token is irrelevant (issue #26).
	if selectedID(s, "plex") {
		claimPath := filepath.Join(s.AppdataRoot, "plex", "claim.env")
		if err := os.MkdirAll(filepath.Dir(claimPath), 0o755); err != nil {
			return err
		}
		content := ""
		if o.plexClaim != "" {
			content = fmt.Sprintf("PLEX_CLAIM=%s\n", o.plexClaim)
		}
		if err := os.WriteFile(claimPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing plex claim file: %w", err)
		}
	}

	// gluetun's credentials file is Arrsenal-owned (derived purely from
	// state) and always regenerated — 0600, outside the world-readable
	// artifacts (issue #27).
	if s.VPNEnabled() {
		credPath := filepath.Join(s.AppdataRoot, "gluetun", "credentials.env")
		if err := os.MkdirAll(filepath.Dir(credPath), 0o755); err != nil {
			return err
		}
		cred := fmt.Sprintf("WIREGUARD_PRIVATE_KEY=%s\n", s.Secrets.WireguardPrivateKey)
		if err := os.WriteFile(credPath, []byte(cred), 0o600); err != nil {
			return fmt.Errorf("writing VPN credentials file: %w", err)
		}
		fmt.Printf("vpn: gluetun (%s) fronts qBittorrent — if the tunnel drops, qBittorrent goes offline (kill switch)\n", s.VPN.Provider)
	}

	artifacts, err := generate.Render(s, o.statePath)
	if err != nil {
		return err
	}
	if err := generate.WriteFiles(o.artifactsDir, artifacts); err != nil {
		return err
	}
	fmt.Printf("generated: %s/docker-compose.yml + .env\n", o.artifactsDir)

	if err := docker.ValidateCompose(o.artifactsDir); err != nil {
		return err
	}
	if o.skipUp {
		fmt.Println("skip-up set: not starting containers")
		return nil
	}

	// Boot phases (DESIGN §7.5): core apps first — their keys and APIs feed
	// the wiring — then tail apps once their configs exist on disk.
	core, tail := partitionByPhase(s)
	fmt.Println("bringing the core apps up…")
	if err := docker.Up(o.artifactsDir, core...); err != nil {
		return err
	}
	ready := docker.WaitReady(core, 3*time.Minute, 2*time.Second)
	printReadiness(ready)

	var wiring []wire.Result
	if !o.skipWiring {
		fmt.Println("wiring the stack together…")
		wiring = wire.Orchestrate(context.Background(), buildSpec(s, o, conflictsAdopted(conflicts)))
	}

	if len(tail) > 0 {
		fmt.Println("bringing the remaining apps up…")
	}
	if err := docker.Up(o.artifactsDir); err != nil { // full up reconciles
		return err
	}
	if len(tail) > 0 {
		printReadiness(docker.WaitReady(tail, 3*time.Minute, 2*time.Second))
		if !o.skipWiring {
			// The tail pass: wiring that needs the tail apps RUNNING —
			// Orchestrate ran before they booted (their configs are its
			// output). Results join the same report.
			wiring = append(wiring,
				wire.OrchestrateTail(context.Background(), buildSpec(s, o, conflictsAdopted(conflicts)))...)
		}
	}

	if len(wiring) > 0 {
		fmt.Print(wire.RenderReport(wiring))
	}
	printAccessTable(s)

	unready := 0
	for _, r := range ready {
		if !r.Ready {
			unready++
		}
	}
	if unready > 0 {
		return fmt.Errorf("%d containers did not become ready", unready)
	}
	if wire.Failed(wiring) {
		return fmt.Errorf("some connections could not be wired — see the report above")
	}
	fmt.Println("done — the stack is up and wired. Re-run arrsenal any time to add or remove apps.")
	return nil
}

func selectedID(s *state.State, id string) bool {
	for _, a := range s.Apps {
		if a == id {
			return true
		}
	}
	return false
}

func partitionByPhase(s *state.State) (core, tail []string) {
	for _, id := range s.Apps {
		app, ok := registry.ByID(id)
		if ok && app.BootPhase == registry.BootTail {
			tail = append(tail, id)
			continue
		}
		core = append(core, id)
	}
	return core, tail
}

func printReadiness(results []dockerx.ReadyResult) {
	for _, r := range results {
		if r.Ready {
			fmt.Printf("  ✓ %s\n", r.App)
		} else {
			fmt.Printf("  ✗ %s\n", r.Detail)
		}
	}
}

// conflictsAdopted extracts the adoption notices the scan produced: those
// apps' configs predate this run, and the wiring engine treats their
// settings as the user's (DESIGN §4, §7).
func conflictsAdopted(conflicts []preflight.Conflict) map[string]bool {
	adopted := map[string]bool{}
	for _, c := range conflicts {
		if c.Kind == preflight.KindAppdata {
			adopted[c.App] = true
		}
	}
	return adopted
}

func buildSpec(s *state.State, o options, adopted map[string]bool) wire.Spec {
	var apps []registry.App
	for _, id := range s.Apps {
		if a, ok := registry.ByID(id); ok {
			apps = append(apps, a)
		}
	}
	qbitContainer := 0
	if qb, ok := registry.ByID("qbittorrent"); ok {
		_, qbitContainer = s.WebPorts(qb)
	}
	qbitHost := "qbittorrent"
	if s.VPNEnabled() {
		qbitHost = "gluetun"
	}
	var trash *quality.Answers
	var runRecyclarr func() (string, error)
	recyclarrDir := filepath.Join(s.AppdataRoot, "recyclarr")
	if s.TRaSH.Enabled {
		trash = &quality.Answers{Resolution: s.TRaSH.Resolution, Source: s.TRaSH.Source, Anime: s.TRaSH.Anime}
		puid, pgid := s.PUID, s.PGID
		runRecyclarr = func() (string, error) {
			// Recyclarr's image runs unprivileged; hand it the config dir.
			chownTree(recyclarrDir, puid, pgid)
			return dockerx.New().RunOneShot(
				quality.Image,
				generate.NetworkName,
				fmt.Sprintf("%d:%d", puid, pgid),
				[]string{recyclarrDir + ":/config"},
				"sync",
			)
		}
	}

	return wire.Spec{
		Apps:        apps,
		Adopted:     adopted,
		AppdataRoot: s.AppdataRoot,
		PUID:        s.PUID, PGID: s.PGID,
		Usenet:   resolveUsenetProvider(o),
		Indexers: resolveIndexers(o),
		TRaSH:    trash, RecyclarrDir: recyclarrDir, RunRecyclarr: runRecyclarr,
		AdminUser:    o.adminUser,
		AdminPass:    o.adminPass,
		QBitPass:     s.Secrets.QBittorrentPassword,
		BazarrAPIKey: s.Secrets.BazarrAPIKey,
		HWAccel:      hwAccelFor(s.GPU),
		Access: func(id string) string {
			app, ok := registry.ByID(id)
			if !ok {
				return ""
			}
			port := s.WebHostPort(app)
			if s.HostNetworked(id) {
				port = app.Web.Container
			}
			// The LAN address, not 127.0.0.1: these URLs end up in the
			// dashboard tiles and report fallbacks, which are clicked from
			// OTHER machines (field report: every Homepage tile pointed at
			// the viewer's own localhost). Wiring calls work either way —
			// the ports bind 0.0.0.0.
			return fmt.Sprintf("http://%s:%d", lanIP(), port)
		},
		QBitContainerPort: qbitContainer,
		QBitHost:          qbitHost,
	}
}

// resolveUsenetProvider turns the flag/prompt values into a server target:
// a preset name fills everything, a bare hostname is a custom provider on
// the standard TLS port. Nil when credentials are missing — the wiring
// never registers a half-configured server.
func resolveUsenetProvider(o options) *wire.UsenetProvider {
	if o.usenetProvider == "" || o.usenetUser == "" || o.usenetPass == "" {
		return nil
	}
	p, ok := wire.UsenetPresets[strings.ToLower(strings.TrimSpace(o.usenetProvider))]
	if !ok {
		// The host doubles as the display name: "[[uswest.newsdemon.com]]"
		// in SAB's server list beats an anonymous "[[Usenet]]".
		p = wire.UsenetProvider{Name: strings.TrimSpace(o.usenetProvider), Host: strings.TrimSpace(o.usenetProvider),
			Port: 563, SSL: true, Connections: 20}
	}
	if o.usenetPort != 0 {
		p.Port = o.usenetPort
	}
	if o.usenetConnections != 0 {
		p.Connections = o.usenetConnections
	}
	p.Username, p.Password = o.usenetUser, o.usenetPass
	return &p
}

// resolveIndexers merges the flag-supplied indexer (if complete) with any
// entered interactively.
func resolveIndexers(o options) []wire.NewznabIndexer {
	out := append([]wire.NewznabIndexer{}, o.indexers...)
	if o.indexerName != "" && o.indexerURL != "" && o.indexerKey != "" {
		out = append(out, wire.NewznabIndexer{Name: o.indexerName, URL: o.indexerURL, APIKey: o.indexerKey})
	}
	return out
}

// chownTree hands a directory tree to the container user (POSIX only; the
// dev platform no-ops). Best effort — the sync surfaces any real problem.
// Lchown, never Chown: this tree is writable by the container user and this
// walk runs as root, so following a planted symlink would re-own arbitrary
// host files (audit finding).
func chownTree(root string, uid, gid int) {
	if runtime.GOOS == "windows" {
		return
	}
	_ = filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err == nil {
			_ = os.Lchown(path, uid, gid)
		}
		return nil
	})
}

func hwAccelFor(mode state.GPUMode) string {
	switch mode {
	case state.GPUNvidia:
		return "nvenc"
	case state.GPUIntel:
		return "qsv"
	case state.GPUAMD:
		return "vaapi"
	default:
		return ""
	}
}

// printAccessTable is the payoff moment: where everything lives, as URLs.
func printAccessTable(s *state.State) {
	host := lanIP()
	fmt.Println("\nYour stack:")
	for _, id := range s.Apps {
		app, ok := registry.ByID(id)
		if !ok {
			continue
		}
		port := s.WebHostPort(app)
		if s.HostNetworked(id) {
			port = app.Web.Container // host networking binds the native port
		}
		fmt.Printf("  %-12s http://%s:%d\n", app.Name, host, port)
	}
}

// lanIP finds the address neighbors reach this box on — best effort, no
// packets sent (UDP "dial" only selects a route).
func lanIP() string {
	conn, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET; never contacted
	if err != nil {
		if h, herr := os.Hostname(); herr == nil {
			return h
		}
		return "localhost"
	}
	defer func() { _ = conn.Close() }()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// stdinReader is THE line reader for every prompt in the run flow. One
// shared reader, never fresh ones: a discarded bufio.Reader keeps whatever
// it buffered, so per-prompt readers can strand a typed answer and let the
// NEXT question consume the leftovers — seen in the field as the GPU
// confirm falling through to the manual mode question.
var stdinReader = bufio.NewReader(os.Stdin)

// confirm asks on the terminal; def is the answer for a bare enter.
func confirm(q string, def bool) bool {
	suffix := " [y/N] "
	if def {
		suffix = " [Y/n] "
	}
	fmt.Print(q + suffix)
	line, err := stdinReader.ReadString('\n')
	if err != nil {
		return def
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return strings.HasPrefix(line, "y")
}

// --- Bubble Tea adapters -------------------------------------------------
// Each screen keeps a pure UpdateWith for tests; these adapters bind them to
// tea.Program with the state threaded through.

func runScreen(m tea.Model) error {
	_, err := tea.NewProgram(m).Run()
	return err
}

type splashAdapter struct{ m *tui.SplashModel }

func (a *splashAdapter) Init() tea.Cmd { return a.m.Init() }
func (a *splashAdapter) View() string  { return a.m.View() }
func (a *splashAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.Update(msg)
	*a.m = next.(tui.SplashModel)
	return a, cmd
}

// displayVersion normalizes the goreleaser stamp for the splash ("0.5.0" →
// "v0.5.0"; local builds show "dev").
func displayVersion() string {
	if version == "dev" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// splashRows gathers the boot readout: cheap, real probes only — the same
// facts preflight verifies for real later. Failures are display states, not
// errors; the splash never blocks an install.
func splashRows() []tui.SplashRow {
	rows := make([]tui.SplashRow, 0, 4)

	host := "linux"
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				host = strings.Trim(v, `"`)
			}
		}
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		host += " · " + strings.TrimSpace(string(out))
	}
	rows = append(rows, tui.SplashRow{Label: "host", Value: host, OK: true})

	if err := dockerx.New().Available(); err == nil {
		rows = append(rows, tui.SplashRow{Label: "docker", Value: "engine + compose plugin", OK: true})
	} else {
		rows = append(rows, tui.SplashRow{Label: "docker", Value: "not detected"})
	}

	det := preflight.DetectGPU(preflight.DefaultGPUProbes())
	gpu := tui.SplashRow{Label: "gpu", Value: string(det.Proposal), OK: det.Proposal != state.GPUNone}
	if det.Proposal == state.GPUNone {
		gpu.Value = "none detected"
	}
	rows = append(rows, gpu)

	if mounts, err := preflight.ListMounts(); err == nil && len(mounts) > 0 {
		m := mounts[0] // biggest free space first
		rows = append(rows, tui.SplashRow{
			Label: "storage", Value: fmt.Sprintf("%s · %s free", m.Target, preflight.HumanBytes(m.FreeBytes)), OK: true})
	}
	return rows
}

type selectAdapter struct{ m *tui.SelectModel }

func (a *selectAdapter) Init() tea.Cmd { return a.m.Init() }
func (a *selectAdapter) View() string  { return a.m.View() }
func (a *selectAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.Update(msg)
	*a.m = next.(tui.SelectModel)
	if a.m.Done() {
		return a, tea.Quit
	}
	return a, cmd
}

type settingsAdapter struct {
	m *tui.SettingsModel
	s *state.State
}

func (a *settingsAdapter) Init() tea.Cmd { return a.m.Init() }
func (a *settingsAdapter) View() string  { return a.m.View() }
func (a *settingsAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.UpdateWith(msg, a.s)
	*a.m = next
	if a.m.Done() {
		return a, tea.Quit
	}
	return a, cmd
}

type pathsAdapter struct {
	m *tui.PathsModel
	s *state.State
}

func (a *pathsAdapter) Init() tea.Cmd { return nil }
func (a *pathsAdapter) View() string  { return a.m.View() }
func (a *pathsAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.UpdateWith(msg, a.s)
	*a.m = next
	if a.m.Done() {
		return a, tea.Quit
	}
	return a, cmd
}

type remapAdapter struct {
	m *tui.RemapModel
	s *state.State
}

func (a *remapAdapter) Init() tea.Cmd { return nil }
func (a *remapAdapter) View() string  { return a.m.View() }
func (a *remapAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.UpdateWith(msg, a.s)
	*a.m = next
	if a.m.Done() || a.m.Back() {
		return a, tea.Quit
	}
	return a, cmd
}
