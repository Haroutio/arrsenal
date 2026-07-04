# User guide

This is the long-form walkthrough. If you just want the stack, the
[README's install section](../README.md#install) is enough — the installer
explains itself as it goes. Come here when you want to know what a prompt
actually decides, what the report symbols mean, or how the lifecycle
commands behave.

- [Before you start](#before-you-start)
- [The interactive install, prompt by prompt](#the-interactive-install-prompt-by-prompt)
- [The wiring report](#the-wiring-report)
- [The data layout](#the-data-layout)
- [Headless mode](#headless-mode)
- [Day two: re-running, updating, removing](#day-two-re-running-updating-removing)

## Before you start

You need:

- A Linux machine that stays on. Debian 12+ or Ubuntu 22.04+ get the full
  prerequisite install; any Linux with Docker + Compose already on it works
  too. WSL2 and Docker-in-LXC are refused — they break in ways nobody can
  debug.
- Somewhere to put media. One big filesystem is the happy path (imports
  become hardlinks — instant, no double disk usage). Two filesystems —
  downloads on scratch, media on the array — is supported with
  `--downloads-root` or the storage screen.
- Accounts with your sources: a usenet provider and indexer, torrent
  trackers, or both. Arrsenal wires everything *around* these, but the
  accounts are yours to bring.

Optional but worth having ready at the keyboard:

- Your usenet provider credentials and your indexer's URL + API key — the
  installer offers to wire both.
- A GPU in the machine, if you want hardware transcoding. Detection is
  automatic; you just confirm.
- If you picked Plex: a claim token from <https://plex.tv/claim>. It
  expires in 4 minutes, so the installer asks for it at the last moment.

## The interactive install, prompt by prompt

```bash
curl -fsSL https://github.com/Haroutio/arrsenal/raw/main/install.sh | bash
```

**App selection.** Space toggles, enter confirms. Nothing is hard-required
— pick a lone Jellyfin if that's what you want — but the classic stack is
Jellyfin + Seerr + Sonarr/Radarr + Prowlarr + SABnzbd or qBittorrent (or
both), with Bazarr and Homepage around the edges. Deselecting an app on a
later run removes its container and keeps its configuration on disk.

**Identity.** PUID/PGID are the user and group every container runs as —
they default to whoever ran the installer, which is almost always right.
Timezone and umask likewise default sensibly.

**Storage.** The data root (default `/data`) holds downloads and media on
one tree; the appdata root (default `/opt/appdata`) holds every app's
configuration. If your downloads should live on a different filesystem
(NVMe scratch, say), set the separate downloads root here — the apps still
see one `/data` inside the containers.

**Admin credential.** One username + password, applied to every app that
supports it during wiring: the arrs' login screens, Jellyfin's wizard,
Seerr's sign-in. Collected, used, never written to disk. Enter skips it —
the arrs then keep their no-login default, and Jellyfin/Seerr appear as ⚠
manual lines in the report.

**Plex claim token** (Plex only, fresh installs only): paste it within its
4-minute life, or skip and claim later in Plex's own UI.

**TRaSH quality settings.** Three questions — 1080p or 4K, standard tier
or remuxes, anime or not — and Recyclarr syncs the matching [TRaSH
Guides](https://trash-guides.info) profiles into Sonarr and Radarr, now and
on a daily schedule. Fresh installs also get the guides' naming scheme and
file-management defaults. Details and caveats: [the README
section](../README.md#trash-guide-quality-profiles).

**Usenet provider** (fresh SABnzbd only): pick a preset — Newshosting,
Eweka, UsenetServer, Frugal, Easynews — or type any hostname, add your
credentials, and SABnzbd is ready to download. Presets fill in the right
port, TLS, and connection count.

**Usenet indexers**: name + URL + API key per indexer (most are Newznab).
They land in Prowlarr, which shares them with every arr. You can add as
many as you like, or none — the report will warn you if the stack ends up
with no way to search.

**VPN** (qBittorrent only): route it through gluetun with WireGuard. The
private key is stored 0600 in the state file, never in the compose
artifacts, and the kill switch is structural — qBittorrent lives inside the
tunnel's network namespace, so packets ride the VPN or go nowhere.

**GPU.** Detection runs first; you confirm what it found (NVIDIA, Intel
QuickSync, AMD VAAPI) or pick manually. Jellyfin's hardware transcoding is
then configured end to end.

Then containers come up in two phases — core apps first, whose keys feed
the wiring, then the tail apps (Bazarr, Homepage) once their generated
configs exist — and the wiring pass runs.

## The wiring report

Every connection Arrsenal makes is one line with one verdict:

| Symbol | Meaning |
|---|---|
| ✓ wired | Made this run. |
| ● existed | Already there — left exactly as it was. |
| ↻ synced | Re-synced every run (the TRaSH profiles). |
| ⚠ manual | Needs a human — the line says what and where. |
| ✗ failed | Didn't work — the line says why. Re-run after fixing; wiring is idempotent. |

A ⚠ is not an error. Plex's browser sign-in, a missing admin password, an
empty indexer list — these are things Arrsenal *can't* or *shouldn't* do
for you, and it says so rather than pretending the stack is finished.

## The data layout

```
/data
├── media
│   ├── tv        ← Sonarr's root folder, Jellyfin's Shows library
│   ├── movies    ← Radarr's root folder, Jellyfin's Movies library
│   └── music     ← Lidarr's root folder, Jellyfin's Music library
├── usenet
│   ├── incomplete
│   └── complete/{tv,movies,music}    ← SABnzbd's categories
└── torrents/{tv,movies,music}        ← qBittorrent's categories
```

This is the [TRaSH Guides layout](https://trash-guides.info/File-and-Folder-Structure/),
and the reason for it is hardlinks: when downloads and media share one
filesystem, an "import" is a rename — instant, atomic, no second copy, and
torrents keep seeding from the original path. Every app mounts the same
`/data`, so the paths they exchange over their APIs agree.

If downloads live on a separate filesystem (`--downloads-root`), imports
are copies. When that happens by accident — one data root quietly spanning
a mount boundary — the interactive install warns and asks you to confirm;
a deliberate `--downloads-root` split is taken at your word. A fine trade
if the scratch disk is the point.

## Headless mode

Everything the TUI asks has a flag. `--yes` runs the whole install from
them — CI, scripts, reprovisioning:

```bash
arrsenal --yes \
  --apps jellyfin,jellyseerr,sonarr,radarr,prowlarr,sabnzbd,bazarr,homepage \
  --admin-user admin --admin-pass '…' \
  --trash --trash-resolution 1080p \
  --usenet-provider newshosting --usenet-user you --usenet-pass '…' \
  --indexer-name NZBgeek --indexer-url https://api.nzbgeek.info --indexer-key '…' \
  --data-root /data --appdata-root /opt/appdata
```

`arrsenal --help` lists every flag. Re-runs with an existing state file
need no flags at all: `arrsenal --yes` reconciles what the state describes.

## Day two: re-running, updating, removing

**The state file is the source of truth**: `/opt/arrsenal/arrsenal.yaml`
holds your answers; `docker-compose.yml` and `.env` are regenerated from it
on every run. Hand-edits to the generated files are lost by design — your
customizations go in `docker-compose.override.yml`
([cookbook](COOKBOOK.md)), which Arrsenal never touches.

**Re-run `arrsenal`** any time: add apps, remove apps, change answers. The
wiring pass is idempotent — everything already connected reports ● and is
not touched again.

**`arrsenal update`** pulls fresh images and recreates whatever changed.
Nothing else: your configuration and wiring are untouched.

**`arrsenal uninstall`** removes the containers and network. Configuration,
state, and your media stay on disk — running `arrsenal` again brings the
same stack back.

**`arrsenal uninstall --purge`** also deletes the app configurations, state
file, and generated artifacts — after a typed confirmation. The scope is
the managed apps' config directories, *including adopted ones*: if
Arrsenal manages an app, purge removes its config, whether Arrsenal
created it or inherited it. Foreign neighbors in the appdata root are
untouched, and your media under `/data/media` is never deleted by
anything.
