package registry

// The v0.1 menu (DESIGN.md §3). Order is menu order: media server first, then
// requests, indexing, PVRs, subtitles, download clients, dashboard.
//
// Images: official where they're the best-maintained (Jellyfin, Jellyseerr,
// Homepage); LinuxServer.io for the arr suite and download clients, whose
// uniform PUID/PGID/UMASK handling is what makes consistent ownership possible.
//
// Deliberately absent, per DESIGN.md: Readarr (retired upstream), Pi-hole and
// Mealie (not media automation), Plex/Emby/Overseerr (v0.3, with paywall
// warnings). Homepage gets no docker.sock mount — even read-only it is
// root-equivalent, and service widgets work through API keys instead (§9).
var apps = []App{
	{
		ID:          "jellyfin",
		Name:        "Jellyfin",
		Description: "Media server — free hardware transcoding",
		Role:        RoleMediaServer,
		Image:       "jellyfin/jellyfin",
		Tag:         "latest",
		Identity:    IdentityUserDirective,
		Web:         PortMap{Container: 8096, Host: 8096, Protocol: "tcp", Purpose: "web UI"},
		ExtraPorts: []PortMap{
			{Container: 7359, Host: 7359, Protocol: "udp", Purpose: "LAN client discovery"},
		},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Sub: "media", Target: "/media"},
			// RAM scratch: fast, no disk wear. A dedicated subdir, not /dev/shm
			// itself — Jellyfin's transcode-cleanup task deletes everything in
			// its transcode dir and must never see other processes' shm files.
			{Kind: SourceHost, Sub: "/dev/shm/jellyfin", Target: "/transcode"},
		},
		WiringTier: WiringFullAuto, // /Startup wizard + libraries + encoder via API (DESIGN §7)
		BootPhase:  BootCore,
		GPU:        true,
	},
	{
		ID:          "plex",
		Name:        "Plex",
		Description: "Media server — polished apps everywhere; hardware transcoding requires Plex Pass",
		Role:        RoleMediaServer,
		Image:       "lscr.io/linuxserver/plex",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Web:         PortMap{Container: 32400, Host: 32400, Protocol: "tcp", Purpose: "web UI"},
		Env:         map[string]string{"VERSION": "docker"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Sub: "media", Target: "/media"},
			{Kind: SourceHost, Sub: "/dev/shm/plex", Target: "/transcode"},
		},
		WiringTier: WiringManual, // claim + server setup live in Plex's own UI
		BootPhase:  BootCore,
		GPU:        true,
		Warnings: []string{
			"Hardware transcoding requires a paid Plex Pass (Jellyfin's is free)",
			"First boot asks for a claim token from plex.tv/claim (valid 4 minutes)",
		},
	},
	{
		ID:          "emby",
		Name:        "Emby",
		Description: "Media server — Jellyfin's commercial sibling; hardware transcoding requires Emby Premiere",
		Role:        RoleMediaServer,
		Image:       "lscr.io/linuxserver/emby",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		// Container 8096 like Jellyfin; host defaults to 8097 so both can
		// run side by side during a migration.
		Web: PortMap{Container: 8096, Host: 8097, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Sub: "media", Target: "/media"},
		},
		WiringTier: WiringManual, // no automatable startup wizard
		BootPhase:  BootCore,
		GPU:        true,
		Warnings: []string{
			"Hardware transcoding requires a paid Emby Premiere subscription (Jellyfin's is free)",
		},
	},
	{
		ID: "jellyseerr",
		// Upstream is the MERGER of Overseerr and the Jellyseerr fork,
		// renamed Seerr in 2026 — one requests app for all three media
		// servers (ghcr.io/seerr-team/seerr; same port, same /app/config
		// layout). The display name follows upstream; the ID does NOT:
		// IDs live in state files and appdata paths, and renaming them
		// breaks every existing install (schema promise, DESIGN.md §11).
		Name:        "Seerr",
		Description: "Requests — users ask, the arrs deliver; works with Jellyfin, Plex and Emby",
		Role:        RoleRequests,
		Image:       "ghcr.io/seerr-team/seerr",
		Tag:         "latest",
		Identity:    IdentityUserDirective, // runs as the node user; honors compose user:, not PUID/PGID
		Web:         PortMap{Container: 5055, Host: 5055, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/app/config"},
		},
		WiringTier: WiringSemiAuto, // best-effort; its own wizard is the 2-minute fallback
		BootPhase:  BootCore,
	},
	{
		ID:          "overseerr",
		Name:        "Overseerr",
		Description: "Requests for Plex — superseded upstream: Seerr now serves Plex too",
		Role:        RoleRequests,
		Image:       "lscr.io/linuxserver/overseerr",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		// Container 5055 like Jellyseerr; host defaults to 5056 so both
		// request apps can coexist.
		Web: PortMap{Container: 5055, Host: 5056, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
		},
		WiringTier: WiringManual, // setup wizard requires a Plex browser login
		BootPhase:  BootCore,
		Warnings: []string{
			"Setup requires signing in with your Plex account (browser) — cannot be automated",
			"Overseerr merged into Seerr upstream — pick Seerr unless you specifically want this",
		},
	},
	{
		ID:          "prowlarr",
		Name:        "Prowlarr",
		Description: "Indexer manager — add indexers once, every arr gets them",
		Role:        RoleIndexer,
		Image:       "lscr.io/linuxserver/prowlarr",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Key:         KeySource{File: "config.xml", Format: KeyXMLApiKey},
		APIBase:     "/api/v1",
		Web:         PortMap{Container: 9696, Host: 9696, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
		},
		WiringTier: WiringFullAuto,
		BootPhase:  BootCore,
	},
	{
		ID:          "sonarr",
		Name:        "Sonarr",
		Description: "TV — monitors series, grabs and imports episodes",
		Role:        RolePVR,
		Image:       "lscr.io/linuxserver/sonarr",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Key:         KeySource{File: "config.xml", Format: KeyXMLApiKey},
		APIBase:     "/api/v3",
		MediaDir:    "tv",
		Web:         PortMap{Container: 8989, Host: 8989, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Target: "/data"}, // full data root: downloads + library on one mount = hardlink imports
		},
		WiringTier: WiringFullAuto,
		BootPhase:  BootCore,
	},
	{
		ID:          "radarr",
		Name:        "Radarr",
		Description: "Movies — monitors, grabs and imports films",
		Role:        RolePVR,
		Image:       "lscr.io/linuxserver/radarr",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Key:         KeySource{File: "config.xml", Format: KeyXMLApiKey},
		APIBase:     "/api/v3",
		MediaDir:    "movies",
		Web:         PortMap{Container: 7878, Host: 7878, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Target: "/data"},
		},
		WiringTier: WiringFullAuto,
		BootPhase:  BootCore,
	},
	{
		ID:          "lidarr",
		Name:        "Lidarr",
		Description: "Music — monitors artists, grabs and imports albums",
		Role:        RolePVR,
		Image:       "lscr.io/linuxserver/lidarr",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Key:         KeySource{File: "config.xml", Format: KeyXMLApiKey},
		APIBase:     "/api/v1",
		MediaDir:    "music",
		Web:         PortMap{Container: 8686, Host: 8686, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Target: "/data"},
		},
		WiringTier: WiringFullAuto,
		BootPhase:  BootCore,
	},
	{
		ID:          "bazarr",
		Name:        "Bazarr",
		Description: "Subtitles — fetches subs for what Sonarr and Radarr import",
		Role:        RoleSubtitles,
		Image:       "lscr.io/linuxserver/bazarr",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Web:         PortMap{Container: 6767, Host: 6767, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Sub: "media", Target: "/data/media"}, // library only; sees the same paths the arrs report
		},
		WiringTier: WiringFullAuto, // file-driven: config written once arr keys are known
		BootPhase:  BootTail,
	},
	{
		ID:          "sabnzbd",
		Name:        "SABnzbd",
		Description: "Usenet download client",
		Role:        RoleDownloadClient,
		Image:       "lscr.io/linuxserver/sabnzbd",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		Key:         KeySource{File: "sabnzbd.ini", Format: KeyINIApiKey},
		Web:         PortMap{Container: 8080, Host: 8080, Protocol: "tcp", Purpose: "web UI"},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Sub: "usenet", Target: "/data/usenet"}, // same /data prefix as the arrs → hardlinks
		},
		WiringTier: WiringFullAuto,
		BootPhase:  BootCore,
	},
	{
		ID:          "qbittorrent",
		Name:        "qBittorrent",
		Description: "Torrent download client",
		Role:        RoleDownloadClient,
		Image:       "lscr.io/linuxserver/qbittorrent",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID,
		// 8081 on BOTH sides plus WEBUI_PORT, per the LSIO image docs: an
		// asymmetric mapping trips qBittorrent's CSRF/host-header validation.
		// 8081 rather than qB's default 8080 because SABnzbd owns 8080 when
		// both are selected. WebPortEnv keeps the env var and both mapping
		// sides moving together under remaps; wiring resolves the container
		// port through the same rule (state.WebPorts).
		Web:        PortMap{Container: 8081, Host: 8081, Protocol: "tcp", Purpose: "web UI"},
		WebPortEnv: "WEBUI_PORT",
		ExtraPorts: []PortMap{
			{Container: 6881, Host: 6881, Protocol: "tcp", Purpose: "torrent inbound"},
			{Container: 6881, Host: 6881, Protocol: "udp", Purpose: "torrent inbound"},
		},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/config"},
			{Kind: SourceData, Sub: "torrents", Target: "/data/torrents"},
		},
		WiringTier: WiringFullAuto, // via the pre-seeded WebUI password — the one pre-seed exception (DESIGN §7)
		BootPhase:  BootCore,
	},
	{
		ID:          "homepage",
		Name:        "Homepage",
		Description: "Dashboard — every service on one page, widgets pre-wired",
		Role:        RoleDashboard,
		Image:       "ghcr.io/gethomepage/homepage",
		Tag:         "latest",
		Identity:    IdentityEnvPUIDGID, // supported since homepage v1
		Web:         PortMap{Container: 3000, Host: 3000, Protocol: "tcp", Purpose: "web UI"},
		Env: map[string]string{
			// Required by homepage; scoping it to the real host is a docs
			// concern until we know the host (override file or v0.3 polish).
			"HOMEPAGE_ALLOWED_HOSTS": "*",
		},
		Mounts: []Mount{
			{Kind: SourceAppdata, Target: "/app/config"},
		},
		WiringTier: WiringFullAuto, // file-driven: services.yaml generated once keys are known
		BootPhase:  BootTail,
	},
}
