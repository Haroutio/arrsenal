```text
█▀█ █▀█ █▀█ █▀ █▀▀ █▄ █ █▀█ █
█▀█ █▀▄ █▀▄ ▄█ ██▄ █ ▀█ █▀█ █▄▄   the arr stack, under one flag.
────────────────────────────────────────────────
                                     .                   .
                                 _..-''"""\          _.--'"""\
                                 |         L         |        \
                     _           / _.-.---.\.        / .-.----.\
                   _/-|---     _/<"---'"""""\\`.    /-'--''"""""\
                  |       \     |            L`.`-. |            L
                  /_.-.---.L    |            \  \  `|            J`.
                _/--'""""  \    F            \L  \  |             L
                  L      `. L  J  _.---.-"""-.\`. L_/ _.--|"""""--.\ `.
                  |        \+. /-"__.--'""""   \ `./'"---'""""""   \`. `.
                  F   _____ \ `F"        `.     \  \                L `.
                 /.-'"_|---'"\ |           `    JL |                 L  `.`.
                <-'""         \|    _.-.------._ A J    _.-.-----`.--|   ``.`.
                 L         `. |/.-'"_.-`---'""\."| /-'"---'"""""   \`.\.  \ `.`.
                 |  _.------\.<'"""            L\ L\                `.`\`. \  `.
            _.-'//'"--'"""   L\|       ________\ `.F     ___.-------._L \ `-\   \`.
           /___| F             F _.--'"_|-------L  /_.-'"_.-|-'"""""""\  L   L   `.`.
               | F  _.-'|"""""/'"-'"""          J <'"""                L J   |     `.`.
               |/-'-''/|""\ )-|\                 F \                   |  L .'"""`\""-\\_
                F`-'-'-(`-')  | \                F  \                  |___`"""`.""`.-'"
   ------------/        `-'---|  F               L   L             __     |"""""`-'"__________
             .'_         |    |__L          __  J__  |    _.--'""""   `".----'".'
            '""""""""""""|--._+--F _.-'""||"   """___/.-'"   ||-'"/""""" \_. .'
            J------------(___\__/'_____.--------'-------'""""""""           /
            `-.                                        _.__.__.__.____     J_.-._
       .'`-._ (-`--`---.'--._`---._.-'`-._.-'_.-'``-._'               `-''-'
~≈≈≈≈≈~      ~≈≈≈≈≈~     ~≈≈≈≈≈~      ~≈≈≈≈≈~     ~~≈≈≈≈~      ~≈≈≈≈≈~     ~~≈≈≈≈~~     ~≈≈≈≈≈
```

[![CI](https://github.com/Haroutio/arrsenal/actions/workflows/ci.yml/badge.svg)](https://github.com/Haroutio/arrsenal/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/Haroutio/arrsenal)](https://github.com/Haroutio/arrsenal/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Arrsenal installs a complete self-hosted media server in one command, then configures
all the apps to work with each other.

Plenty of scripts can start a dozen containers. What you're left with afterwards is a
dozen setup wizards: Sonarr doesn't know your download client exists, Prowlarr isn't
connected to anything, SABnzbd rejects requests because of Docker hostname quirks, and
Jellyfin greets you with a first-run wizard. Arrsenal does that part too. It talks to
each app's API after startup and wires the whole thing together, so the first time you
open a browser, everything already works.

A fresh install ends like this (TRaSH is a community guide, not a verdict — more on
that [below](#trash-guide-quality-profiles-new-in-v04)):

```
Wiring report:
  ✓ SABnzbd ← host whitelist "sabnzbd"
  ✓ SABnzbd ← download folders under /data/usenet
  ✓ SABnzbd ← category "tv"
  ✓ Sonarr ← admin credential
  ✓ Prowlarr → Sonarr
  ✓ Sonarr → SABnzbd
  ✓ Sonarr root folder /data/media/tv
  ↻ Sonarr ← TRaSH quality profiles

  7 wired · 1 synced
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
- Around the edges: **Bazarr** fetches subtitles, **Jellyseerr**/**Overseerr** give
  your family a simple "request a movie" page, and **Homepage** puts every service on
  one dashboard.

The chore is introducing all of these to each other: half a dozen API keys to copy
around, download folders that have to line up exactly between apps, categories, root
folders, hostname whitelists — and then the longest part, building out quality
profiles and custom format scores so the arrs grab good releases instead of whatever
shows up first. All of it is what Arrsenal automates, and re-runs stay safe: it only
ever adds what's missing and never overwrites settings you've already changed.

One thing you bring yourself: access to the sources. That means an account with a
usenet provider and indexer, or your torrent trackers of choice. Arrsenal wires the
plumbing; what flows through it is up to you.

## Install

You need a Linux machine that stays on — an old PC, a mini-PC, a NAS, a VPS — running
Debian 12+ or Ubuntu 22.04+. Other distros work too if Docker is already installed
(details in [Support tiers](#support-tiers)). Windows and WSL2 are not supported.

```bash
curl -fsSL https://github.com/Haroutio/arrsenal/raw/main/install.sh | bash
```

The script detects your distro, offers to install Docker if it's missing, downloads
the `arrsenal` binary for your architecture, verifies its SHA-256 checksum, and hands
over to an interactive terminal UI that walks you through every choice. It's not meant
to be piped into sudo — it asks for elevation only at the steps that need it, and each
one says why.

If you'd rather read before you run:

```bash
curl -fsSL https://github.com/Haroutio/arrsenal/raw/main/install.sh -o install.sh
less install.sh
bash install.sh
```

When it finishes, it prints the address of every app it installed — start with
Homepage, which links to all of them. Your admin credentials are the ones you chose
during setup.

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
| [Jellyseerr](https://docs.seerr.dev) | Requests (pairs with Jellyfin/Emby) |
| [Overseerr](https://overseerr.dev) | Requests (pairs with Plex) |
| [Homepage](https://gethomepage.dev) | Dashboard, widgets pre-wired |

Jellyfin is the recommended media server because its hardware transcoding — using
your GPU to convert video on the fly so it plays smoothly on any device — is free.
Plex and Emby charge for the same feature, and the warnings appear right on the
selection screen.

Two options during setup deserve a mention. qBittorrent can be routed through a
[gluetun](https://github.com/qdm12/gluetun) WireGuard tunnel: it shares gluetun's
network namespace, so torrent traffic can only leave through gluetun, and gluetun's
built-in kill switch drops everything that isn't the tunnel. If the VPN goes down,
torrenting stops instead of leaking. And if your downloads and your library live on
different disks (a fast SSD for incoming files, a big array for the collection),
Arrsenal supports that split — every app still sees one consistent `/data` layout.

### TRaSH-guide quality profiles (new in v0.4)

The [TRaSH Guides](https://trash-guides.info) are the community reference for making
Sonarr and Radarr grab the *good* releases: quality profiles, custom format scores,
size limits. Applying them by hand means hours of clicking. Arrsenal asks three
questions instead — 1080p or 4K? Full-quality remuxes? Anime? — then syncs the
matching profiles into Sonarr and Radarr via [Recyclarr](https://recyclarr.dev), and
keeps them current on every re-run.

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
  keeps every secret-bearing file it writes readable by root only (mode `0600`) —
  secrets never land in the world-readable compose or `.env` files — and it never
  prints them, and phones home to nobody.
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
| v1.0 | Stability: docs site, hardening, stable state schema | next |

Architecture and design reasoning live in [docs/DESIGN.md](docs/DESIGN.md).

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
