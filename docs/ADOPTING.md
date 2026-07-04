# Adopting an existing setup

Arrsenal is safe to point at a machine that already runs some of these
apps. This page explains exactly what that means, because "safe" claims
deserve specifics.

## What adoption is

During preflight, any selected app whose configuration directory already
has content is marked **adopted**. The rule for adopted apps:

> Arrsenal wires what was never set up, and never modifies settings you've
> made.

Concretely, on an adopted app:

- Its config files are never rewritten. The pre-written configs (Bazarr,
  Homepage, qBittorrent's password seed) are write-once — an existing file
  is left byte-for-byte alone.
- Settings lanes back off: TRaSH naming, media-management defaults,
  qBittorrent categories, Bazarr languages all report ● existed with zero
  writes when the app is adopted.
- Additive wiring still happens, idempotently: a download client entry, a
  root folder, a Prowlarr application are *added* if missing — by name,
  never replacing an existing entry. An entry that exists, even one
  configured differently than Arrsenal would, is left exactly as it is.
- One deliberate exception: an adopted arr whose authentication was never
  finished (the factory "no login" state) gets the admin credential
  applied. That's completing an unfinished setup, not changing a choice.

Two settings are convergent *by consent*: the TRaSH quality profiles (and
only the TRaSH-named ones) re-sync on every run and daily in between — the
prompt says so before you enable it, and your own profiles are never
touched.

## Ports and names already in use

Preflight detects two kinds of collision before anything starts:

- **Container names**: an existing `sonarr` container that isn't
  Arrsenal's. You'll be told, and nothing is touched — remove or rename the
  old container, or deselect the app.
- **Ports**: something already listening on 8989. Arrsenal offers a remap
  (Sonarr on 9989, say), and the wiring uses the remapped port everywhere
  automatically.

## Migrating from an existing docker-compose stack

Two honest paths, depending on how your current stack stores things.

### Path A — your layout already matches

If your apps already follow the shared-tree layout (one `/data` with
`media/` and download dirs under it — the TRaSH structure), you can adopt
in place:

1. Stop the old stack: `docker compose down` in your old project (volumes
   and configs stay on disk).
2. Run Arrsenal. Point the appdata root at your existing config directory
   tree and the data root at your existing data root. App config dirs must
   be named by app id (`sonarr`, `radarr`, …) directly under the appdata
   root — rename or symlink if yours differ.
3. Everything is adopted: your settings survive, missing connections get
   wired, and the stack is now Arrsenal-managed (updates, reconciliation,
   dashboard).

### Path B — your layout doesn't match

Split volumes per app, downloads somewhere unrelated, media paths like
`/mnt/media/TV Shows`: adopting in place would mean Arrsenal's wiring and
your paths disagree inside every container. Don't fight it:

1. Run Arrsenal fresh — new appdata, `/data` tree.
2. Move (or hardlink/copy) your media into `/data/media/{tv,movies,music}`.
3. In the fresh Sonarr/Radarr, import the existing library ("Library
   Import"), or restore each app's own backup and fix its root folder to
   `/data/media/...`.
4. Retire the old stack once you've verified the new one sees everything.

Slower, but you end up on the layout every guide assumes, with hardlinked
imports — which is probably why you're migrating in the first place.

## Checking what happened

The wiring report is the audit trail: every ● line is something Arrsenal
found and left alone. If you expected a ✓ and got a ●, the entry already
existed under that name — look at it in the app's UI before assuming it's
configured the way you want.
