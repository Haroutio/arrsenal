# Troubleshooting

Where things live, what the common warnings mean, and how to dig when
something fails. General rule: **re-running `arrsenal` is always safe** —
wiring is idempotent, and a transient failure (an app that was slow to
start, a network blip) usually just wires clean on the second pass.

## Where everything lives

| Thing | Place |
|---|---|
| Your answers (source of truth) | `/opt/arrsenal/arrsenal.yaml` |
| Generated compose + env | `/opt/arrsenal/docker-compose.yml`, `.env` |
| Your customizations | `/opt/arrsenal/docker-compose.override.yml` ([cookbook](COOKBOOK.md)) |
| App configurations | `/opt/appdata/<app>/` |
| An app's logs | `docker logs <app>` (e.g. `docker logs sonarr`) |
| Recyclarr's sync logs | `/opt/appdata/recyclarr/logs/` |

(Defaults shown; your roots are whatever you answered.)

## "Downloads and media are on different filesystems"

Not an error — a heads-up. Hardlink imports only work within one
filesystem; across two, every import is a full copy (double I/O, double
disk until the seed/cleanup). If you split storage on purpose
(`--downloads-root` on scratch NVMe), confirm and carry on. If you didn't
mean to, check for a mount boundary inside your data root — a common
surprise is `/data` on the root disk with `/data/media` mounted from the
array.

## GPU / hardware transcoding

**NVIDIA**: the container needs the NVIDIA container toolkit on the host.
Check `nvidia-smi` works on the host first; then
`docker exec jellyfin nvidia-smi` should work too. Arrsenal sets
`NVIDIA_DRIVER_CAPABILITIES=all` (older toolkits default to no-video,
which silently breaks NVENC). Playback that works but transcodes on CPU
usually means the client is direct-playing — force a quality change to
test — or Jellyfin's dashboard → Playback shows the encoder isn't set.

**Intel/AMD**: the container gets `/dev/dri`. If transcodes fail, check
the render device permissions on the host (`ls -l /dev/dri`) — the
container user needs access to `renderD128`, typically via the `render`
group's GID.

**Plex/Emby**: hardware transcoding is a paid feature (Plex Pass / Emby
Premiere) — Arrsenal plumbs the device through, the app decides whether to
use it.

## SELinux

On enforcing hosts (Fedora, RHEL), preflight prints a warning: bind mounts
may need the right context. If apps can't read their config or `/data`,
either label the trees (`chcon -Rt container_file_t /data /opt/appdata`)
or consult your distro's container policy. Arrsenal doesn't relabel
anything itself.

## A wiring line failed (✗)

The line's detail says what happened. The common ones:

- **"the app rejected the API key"** — the app's config predates the run
  and its key changed, or the app regenerated it. Re-run `arrsenal`; keys
  are re-read from the app configs each pass.
- **Connection refused / timeout** — the app was still starting. Re-run.
- **A 400 with a payload echo** — likely a real bug; the redacted output
  is safe to paste into an issue:
  <https://github.com/Haroutio/arrsenal/issues>.

## "no indexers configured — the stack can't search for anything yet"

Exactly what it says: the wiring is done but Prowlarr has no indexers, so
the arrs have nothing to search. Add your indexer in Prowlarr's UI
(Indexers → Add), or re-run `arrsenal` and answer the indexer prompt.
Prowlarr pushes indexers to every arr automatically.

## TRaSH sync failed

The report line carries Recyclarr's own error tail (keys redacted). Full
logs: `/opt/appdata/recyclarr/logs/`. The usual causes are network (the
sync clones the guide repos) and — rarely — an upstream guide change;
re-running is the first move. The scheduled daily sync uses the same
config, so a fixed sync stays fixed.

## Image pulls fail with `toomanyrequests`

Registry rate limiting on shared IPs (VPS providers especially). Wait a
few minutes and re-run; already-pulled images are cached, so each retry
makes progress.

## qBittorrent behind the VPN has no connectivity

`docker logs gluetun` first — a bad WireGuard key or an unsupported
country name fails there, and qBittorrent (which shares its network) shows
it as "no internet". The kill switch working as designed looks exactly
like this: no tunnel, no traffic.

## I ran the first install with --skip-wiring

`--skip-wiring` is a debug flag, and on a FIRST run it skips more than
API calls: the pre-written configs (Bazarr's connections and API key,
notably) are wiring output too. Bazarr then boots with its own defaults
and is treated as adopted from that point on — Arrsenal won't rewrite a
config it didn't write. If that wasn't what you wanted, stop the stack,
delete `<appdata>/bazarr`, and re-run arrsenal without the flag.

## Starting over

```bash
arrsenal uninstall           # containers gone, configs and media stay
arrsenal uninstall --purge   # + configs, state, artifacts (typed confirmation)
```

`--purge` removes the managed scope — the managed apps' config directories
(adopted ones included), the state file, and the generated artifacts.
Foreign directories in the appdata root survive; media under `/data/media`
is never deleted by anything.

## Still stuck

Open an issue with the wiring report and the relevant `docker logs`
output: <https://github.com/Haroutio/arrsenal/issues>. Secrets are
redacted from the report by design, but skim before pasting anyway.
