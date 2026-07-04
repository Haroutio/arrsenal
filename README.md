# Arrsenal — the self-hosted media server that installs itself

[![CI](https://github.com/Haroutio/arrsenal/actions/workflows/ci.yml/badge.svg)](https://github.com/Haroutio/arrsenal/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/Haroutio/arrsenal)](https://github.com/Haroutio/arrsenal/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

One command installs and wires a complete self-hosted media server: **Jellyfin**
(or Plex/Emby), **Sonarr**, **Radarr**, **Lidarr**, **Prowlarr**, **Bazarr**,
**SABnzbd**, **qBittorrent**, **Seerr** requests, and a **Homepage** dashboard —
connected to each other and configured with TRaSH-guide quality profiles,
automatically.

Plenty of scripts can start a dozen containers. What you're left with afterwards is a
dozen setup wizards: Sonarr doesn't know your download client exists, Prowlarr isn't
connected to anything, SABnzbd rejects requests because of Docker hostname quirks, and
Jellyfin greets you with a first-run wizard. Arrsenal does that part too. It talks to
each app's API after startup and wires the whole thing together, so the first time you
open a browser, everything already works.

Here is the whole thing, start to finish — boot, pick the stack, answer a few
questions, and land on a fully wired system:

![A complete arrsenal install: the boot splash, app selection, TRaSH quality questions, and the final wiring report — 29 wired, zero manual steps](docs/assets/demo.gif)

Here's the full report from that run. A ✓ is a connection Arrsenal made, a ●
is one that already existed and was left alone, and a ↻ is one it re-syncs on
every run — the [TRaSH quality profiles](#trash-guide-quality-profiles). This
demo ran without a GPU; with one, Jellyfin's hardware transcoding gets configured
and reported too.

```
Wiring report:
  ✓ SABnzbd ← host whitelist "sabnzbd"
  ✓ SABnzbd ← download folders under /data/usenet
  ✓ SABnzbd ← category "tv"
  ● SABnzbd ← category "movies"
  ✓ SABnzbd ← category "music"
  ✓ Prowlarr ← admin credential
  ✓ Sonarr ← admin credential
  ✓ Radarr ← admin credential
  ✓ Lidarr ← admin credential
  ✓ Prowlarr → Sonarr
  ✓ Sonarr → SABnzbd
  ✓ Sonarr root folder /data/media/tv
  ✓ Prowlarr → Radarr
  ✓ Radarr → SABnzbd
  ✓ Radarr root folder /data/media/movies
  ✓ Prowlarr → Lidarr
  ✓ Lidarr → SABnzbd
  ✓ Lidarr root folder /data/media/music
  ✓ Jellyfin setup wizard
  ✓ Jellyfin library "Movies"
  ✓ Jellyfin library "Shows"
  ✓ Jellyfin library "Music"
  ✓ Jellyfin API key (dashboard widget)
  ✓ Bazarr ← sonarr/radarr connections
  ✓ Homepage ← service widgets
  ↻ Sonarr ← TRaSH quality profiles
  ↻ Radarr ← TRaSH quality profiles
  ✓ Seerr ← jellyfin sign-in
  ✓ Seerr ← libraries
  ✓ Seerr → Sonarr
  ✓ Seerr → Radarr
  ✓ Seerr initialized

  29 wired · 1 existed · 2 synced
```

## Never heard of the Arr apps? Start here

The end result is your own private streaming service: a Netflix-style app on your TV
and phone, with a library you control. The "servarr" apps — everyone just calls them
the arrs, hence this project's name — are the open-source tools that automate the
library part. Each one does a single job:

- **Sonarr** (TV), **Radarr** (movies) and **Lidarr** (music) keep watch over your
  library. You add a show or film you want, and they search for it, grab it, rename it
  properly, and file it away. When a new episode airs, it shows up on its own.
- **Prowlarr** manages your *indexers* — the search sites where releases are found —
  in one place, and shares them with Sonarr, Radarr and Lidarr, so you configure each
  indexer once.
- **SABnzbd** and **qBittorrent** do the actual downloading, on instructions from the
  arrs. Downloads come from usenet (paid servers — fast, private, no uploading) or
  torrents (free, but you'll want the VPN option); many people run both.
- **Jellyfin**, **Plex** or **Emby** is the part your TV sees: the streaming interface
  for watching your library at home or away.
- Around the edges: **Bazarr** fetches subtitles, **Seerr** gives your family a
  simple "request a movie" page, and **Homepage** puts every service on one
  dashboard.

The chore is introducing all of these to each other: half a dozen API keys to copy
around, download folders that have to line up exactly between apps, categories, root
folders, hostname whitelists — and then the longest part, building out quality
profiles and custom format scores so the arrs grab good releases instead of whatever
shows up first. All of it is what Arrsenal automates.

One thing you bring yourself: access to the sources — an account with a usenet
provider and indexer, or your torrent trackers of choice.

## Install

You need a Linux machine that stays on — an old PC, a mini-PC, a NAS, a VPS — running
Debian 12+ or Ubuntu 22.04+. Other distros work too if Docker is already installed
(details in [Support tiers](#support-tiers)). Windows and WSL2 are not supported.

```bash
curl -fsSL https://github.com/Haroutio/arrsenal/raw/main/install.sh | bash
```

The script detects your distro, offers to install Docker if it's missing, downloads
the `arrsenal` binary for your architecture, verifies its SHA-256 checksum, and hands
over to an interactive terminal UI that walks you through every choice. It never
asks to be piped into sudo; it requests elevation only for the steps that need it.

If you'd rather read before you run:

```bash
curl -fsSL https://github.com/Haroutio/arrsenal/raw/main/install.sh -o install.sh
less install.sh
bash install.sh
```

When it finishes, it prints the address of every app it installed — start with
Homepage, which links to all of them.

Re-run `sudo arrsenal` any time to add or remove apps. Your answers persist in
`/opt/arrsenal/arrsenal.yaml`, Docker Compose reconciles the difference, and the
wiring pass picks up whatever's new. There's also a headless mode for scripted
installs: `sudo arrsenal --yes --apps sonarr,radarr,... --admin-pass ...`
(see `arrsenal --help`).

## What's in the box

<sub>*WHAT'S IN THE BOX?! — calm down, Detective Mills, it's just containers.*</sub>

| App | Role |
|---|---|
| [Jellyfin](https://jellyfin.org) | Media server (free hardware transcoding) |
| [Plex](https://plex.tv) | Media server (hardware transcoding needs Plex Pass) |
| [Emby](https://emby.media) | Media server (hardware transcoding needs Emby Premiere) |
| [Sonarr](https://sonarr.tv) | TV |
| [Radarr](https://radarr.video) | Movies |
| [Lidarr](https://lidarr.audio) | Music |
| [Prowlarr](https://prowlarr.com) | Indexer manager |
| [SABnzbd](https://sabnzbd.org) | Usenet download client |
| [qBittorrent](https://www.qbittorrent.org) | Torrent download client |
| [Bazarr](https://www.bazarr.media) | Subtitles |
| [Seerr](https://docs.seerr.dev) | Requests — works with Jellyfin, Plex and Emby |
| [Homepage](https://gethomepage.dev) | Dashboard, widgets pre-wired |

Hardware transcoding — using your GPU to convert video on the fly so it plays
smoothly on any device — is free in Jellyfin; Plex and Emby charge for it.

qBittorrent can be routed through a [gluetun](https://github.com/qdm12/gluetun)
WireGuard tunnel. It shares gluetun's network namespace, so torrent traffic can only
leave through the tunnel, and gluetun's built-in kill switch drops everything else —
if the VPN goes down, torrenting stops instead of leaking.

If your downloads and your library live on different disks (a fast SSD for incoming
files, a big array for the collection), Arrsenal supports that split — every app
still sees one consistent `/data` layout.

### TRaSH-guide quality profiles

The [TRaSH Guides](https://trash-guides.info) are the community reference for making
Sonarr and Radarr grab the *good* releases: quality profiles, custom format scores,
size limits. Applying them by hand means hours of clicking. Arrsenal asks three
questions instead — 1080p or 4K? Full-quality remuxes? Anime? — then syncs the
matching profiles into Sonarr and Radarr via [Recyclarr](https://recyclarr.dev), and
keeps them current on every re-run. These profiles are the community's opinion, not
a certification — Arrsenal re-syncs them every run (which is why the report marks
them ↻ synced, and why hand-edits to the TRaSH-named profiles don't stick; your own
profiles are never touched).

Saying yes to TRaSH also applies the guides' recommended naming scheme (rename on,
filenames that keep quality, codec, and release group) and file-management defaults
to a fresh Sonarr and Radarr. Adopted installs keep their naming exactly as it was —
renaming an existing library out from under you is not a thing this tool does.

## Ground rules

- **The state file is the source of truth.** Your answers live in
  `/opt/arrsenal/arrsenal.yaml`. The `docker-compose.yml` and `.env` are generated
  from it and regenerated on every run. Your own customizations go in
  `docker-compose.override.yml`, which Compose merges natively and Arrsenal never
  touches.
- **It never deletes data.** Removing an app removes its container; its configuration
  stays on disk until you delete it yourself. Even `arrsenal uninstall --purge`
  removes only what Arrsenal created, and asks for typed confirmation first.
- The shared `/data` layout follows the TRaSH guides so that imports are hardlinks —
  the same file appearing in two places at no extra cost — instead of slow copies.
  The preflight check (the tests Arrsenal runs before touching anything) performs a
  real hardlink across your chosen paths and warns you before anything starts.
- Already running some of these apps? Preflight detects port, container-name, and
  config collisions and offers remaps. Arrsenal wires only the apps it manages and
  won't reach into an install it doesn't own.
- The arr apps store API keys in plaintext; that's how the ecosystem works. Arrsenal
  keeps every secret-bearing file it writes locked to mode `0600`, owned by the one
  user that must read it, never prints a secret, and phones home to nobody; secrets
  never land in the world-readable compose or `.env` files.
- NVIDIA, Intel QuickSync, and AMD VAAPI GPUs are detected and configured through to
  Jellyfin's encoder settings. If the NVIDIA container toolkit is missing, Arrsenal
  prints the exact commands to install it. Kernel drivers are never touched; you get
  a clear pointer to the vendor's instructions instead.

CI enforces the "adds what's missing, changes nothing else" behavior on every commit:
a full install runs against a real Docker daemon, the apps' APIs are queried to
confirm the wiring landed, and a second run must change nothing.

## Support tiers

| Tier | Environment | What you get |
|---|---|---|
| 1 | Debian 12+ / Ubuntu 22.04+ (amd64, arm64) | Full prerequisite installation; bugs here block releases |
| 2 | Any Linux with Docker + Compose already installed | Full functionality, prerequisite install skipped |
| — | WSL2, Docker-in-LXC | Politely refused (they break in ways nobody can debug) |

Other mainstream distros (Fedora, RHEL, openSUSE, …) sit between tiers: the script
offers Docker's own official installer, best-effort. If that makes you nervous,
install Docker yourself and you're Tier 2.

## Roadmap

| Milestone | Theme | Status |
|---|---|---|
| v0.1 | It installs: TUI, preflight, generation, `up -d` | ✅ |
| v0.2 | Auto-wiring through the apps' APIs | ✅ |
| v0.3 | Media-server choice, VPN, `update`/`uninstall`, split storage, headless mode | ✅ |
| v0.4 | TRaSH-guide quality profiles via Recyclarr | ✅ |
| v0.5 | Field hardening: scrolling TUI + boot splash, Lidarr wiring, dashboard fixes | ✅ |
| v0.6 | Seerr (the Overseerr/Jellyseerr merger) sets itself up | ✅ |
| v1.0 | Stability: docs site, hardening, stable state schema | next |

## Docs

- [User guide](docs/GUIDE.md) — the long-form walkthrough: every prompt,
  the report symbols, the data layout, headless mode, day-two commands
- [Override cookbook](docs/COOKBOOK.md) — pin images, add containers,
  mount extra disks, without fighting the generated files
- [Adopting an existing setup](docs/ADOPTING.md) — what adoption
  guarantees, and the two migration paths from a hand-built stack
- [Troubleshooting](docs/TROUBLESHOOTING.md) — where things live, common
  warnings, GPU/SELinux/VPN digging
- [DESIGN.md](docs/DESIGN.md) — architecture and design reasoning, for
  contributors

## FAQ

### Is this just another docker-compose template?

No — the compose file is the easy 10%. Arrsenal's point is the other 90%:
after the containers start, it talks to every app's API and connects them —
Prowlarr to the arrs, the arrs to SABnzbd, root folders, authentication,
Jellyfin's wizard and hardware transcoding, TRaSH quality profiles, the
Seerr request page, the dashboard.

### Does it work with an existing Sonarr/Radarr/Jellyfin setup?

Yes, carefully. Preflight detects port and container-name collisions and
offers remaps, and any app whose configuration predates the run is
*adopted*: Arrsenal wires what was never set up (like an unfinished
authentication screen) but never modifies settings you've made.

### Which media server should I pick — Jellyfin, Plex, or Emby?

Jellyfin is the recommendation: its hardware transcoding is free and Arrsenal
configures it end-to-end — setup wizard, libraries, NVENC/QuickSync/VAAPI. Seerr
is also set up automatically with Jellyfin or Emby. Plex's sign-in is browser
OAuth, which can't be automated, so with Plex you finish Seerr's short wizard
yourself.

### Does it set up TRaSH Guides quality settings?

Yes — three questions (1080p or 4K? remuxes? anime?) and the profiles, custom
format scores, and size limits are synced via Recyclarr. Details in
[TRaSH-guide quality profiles](#trash-guide-quality-profiles).

### What does it run on?

Any Linux with Docker (Debian 12+/Ubuntu 22.04+ get prerequisites installed
for you). One static Go binary, no runtime dependencies. Windows and WSL2
are not supported — a media server wants a real Linux host or VM.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). The issue tracker is organized by milestone,
and the `good-first-issue` labels are meant sincerely.

## Thanks

Arrsenal leans on a lot of other people's work:

- [LinuxServer.io](https://www.linuxserver.io) maintains the container images for
  most of the stack.
- The [TRaSH Guides](https://trash-guides.info) document how to set these apps up
  properly; Arrsenal follows their conventions instead of inventing its own, and uses
  [Recyclarr](https://recyclarr.dev) to sync their quality profiles.
- [DockSTARTer](https://dockstarter.com) has been helping people install this kind of
  stack for years and is worth a look if Arrsenal isn't your style.

## License

[MIT](LICENSE)
