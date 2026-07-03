# Arrsenal

[![CI](https://github.com/Haroutio/arrsenal/actions/workflows/ci.yml/badge.svg)](https://github.com/Haroutio/arrsenal/actions/workflows/ci.yml)

**One command. A complete, self-hosted media-automation stack. Actually wired together.**

Arrsenal is an interactive installer for the *servarr* ecosystem: pick your apps in a
pretty terminal UI, and it does everything — installs prerequisites, generates a clean
`docker-compose.yml` + `.env`, brings the stack up, and then **auto-wires the apps to each
other through their own APIs**: Prowlarr knows your Sonarr and Radarr, every arr has its
download client and root folders configured, Jellyfin's setup wizard is completed with
hardware transcoding enabled — before you've opened a single web UI.

Most stack installers stop at "containers running." Finishing the configuration is the
whole point of this one.

> **Status: early.** **v0.1 is out** and covers install through `docker compose up` —
> the TUI, preflight, generation, and bring-up. The auto-wiring described above is the
> **v0.2** milestone, in progress now; until it lands you connect the apps to each other
> the classic way. Follow the [milestones](../../milestones) to watch it arrive.

## What you get

The v0.1 menu — pick any subset:

| App | Role |
|---|---|
| [Jellyfin](https://jellyfin.org) | Media server (free hardware transcoding) |
| [Jellyseerr](https://github.com/fallenbagel/jellyseerr) | Requests |
| [Prowlarr](https://prowlarr.com) | Indexer manager |
| [Sonarr](https://sonarr.tv) | TV |
| [Radarr](https://radarr.video) | Movies |
| [Lidarr](https://lidarr.audio) | Music |
| [Bazarr](https://www.bazarr.media) | Subtitles |
| [SABnzbd](https://sabnzbd.org) | Usenet download client |
| [qBittorrent](https://www.qbittorrent.org) | Torrent download client |
| [Homepage](https://gethomepage.dev) | Dashboard (widgets pre-wired in v0.2) |

Plex and Emby arrive in v0.3 — Plex paired with Overseerr, Emby with Jellyseerr —
including honest warnings that their hardware transcoding sits behind Plex Pass /
Emby Premiere. Jellyfin's does not; that's why it's the flagship path.

## Install

```bash
# Release-pinned, checksum-verified:
curl -fsSL https://raw.githubusercontent.com/Haroutio/arrsenal/v0.1.0/install.sh | bash
```

The bootstrap script detects your distro, offers to install Docker if it's missing
(inform-then-prompt — nothing installs silently), downloads the `arrsenal` binary for
your architecture, verifies its SHA-256 checksum, and hands over to the TUI.

Prefer to read before you run? Good instinct:

```bash
curl -fsSL https://raw.githubusercontent.com/Haroutio/arrsenal/v0.1.0/install.sh -o install.sh
less install.sh
bash install.sh
```

Re-run `sudo arrsenal` any time to add or remove apps — your answers persist in
`/opt/arrsenal/arrsenal.yaml`, and Compose reconciles the difference. Headless use:
`arrsenal --yes --apps sonarr,radarr,... ` (see `arrsenal --help`).

## Design principles

- **Declarative, re-runnable, never a surprise.** Your answers live in a state file
  (`/opt/arrsenal/`); `docker-compose.yml` and `.env` are regenerated build artifacts.
  Run Arrsenal again anytime to add or remove apps — Docker Compose itself reconciles.
  Your customizations belong in `docker-compose.override.yml`, which Arrsenal never
  touches and Compose merges natively.
- **Never deletes data.** Removing an app removes its container. Its config stays on
  disk until *you* delete it.
- **TRaSH-guide layout, verified — not assumed.** One shared `/data` mount so imports
  are instant hardlinks, never slow copies. Preflight performs a *real* hardlink test
  across your chosen paths and warns you before anything starts.
- **Coexistence over conquest.** Already run some of these apps? Preflight detects port,
  container-name, and config collisions, and offers remaps. Arrsenal only wires the apps
  it manages — it will never reach into an install it doesn't own.
- **Honest security posture.** The arr apps store API keys in plaintext — that's the
  ecosystem, and Arrsenal doesn't pretend otherwise. It keeps its own files `0600`,
  never prints secrets, phones home to nobody, and its install script is release-pinned
  with checksum-verified binaries.
- **GPU without the swamp.** NVIDIA (container toolkit auto-install), Intel QuickSync,
  and AMD VAAPI are detected and configured end-to-end — including Jellyfin's encoder
  settings via API. Kernel drivers are never installed; you'll get a clear pointer
  instead.

## Support tiers

| Tier | Environment | What you get |
|---|---|---|
| 1 | Debian 12+ / Ubuntu 22.04+ (amd64, arm64) | Full prerequisite installation; bugs here will block releases |
| 2 | Any Linux with Docker + Compose already installed | Full functionality, prereq install skipped |
| — | WSL2, Docker-in-LXC | Politely refused (they break in ways nobody can debug) |

## Roadmap

| Milestone | Theme |
|---|---|
| v0.1 | It installs: TUI, preflight, generation, `up -d` |
| v0.2 | The killer feature: full API auto-wiring |
| v0.3 | Media-server choice (Plex/Emby), VPN, update/uninstall, headless mode |
| v1.0 | Stable: docs, hardening, schema stability |

Full architecture and every design decision (with reasoning): [docs/DESIGN.md](docs/DESIGN.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The issue tracker is organized by milestone;
`good-first-issue` labels are real, not decorative.

## Prior art & thanks

[DockSTARTer](https://dockstarter.com) proved people want this. The
[TRaSH Guides](https://trash-guides.info) define the correct layout — Arrsenal follows
them. The [LinuxServer.io](https://www.linuxserver.io) team maintains the images that
make consistent PUID/PGID handling possible.

## License

[MIT](LICENSE)
