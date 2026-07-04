# Override cookbook

Arrsenal regenerates `docker-compose.yml` and `.env` from the state file on
every run — edits there are lost by design. The supported place for your
customizations is `docker-compose.override.yml`, in the same directory
(default `/opt/arrsenal/`). Docker Compose merges it natively on every
`up`, and Arrsenal never reads, writes, or validates it: it's yours.

The recipes below are starting points. After adding one, apply it with:

```bash
cd /opt/arrsenal && docker compose up -d
```

(or just re-run `arrsenal`, which ends with the same reconciliation).

## Pin an app to a specific image version

```yaml
# docker-compose.override.yml
services:
  sonarr:
    image: lscr.io/linuxserver/sonarr:4.0.15
```

`arrsenal update` pulls whatever tag the merged compose file names, so a
pin here survives updates until you remove it.

## Mount a second media disk

An extra library location that Sonarr and Jellyfin should both see —
mounted at the same path in every container that needs it, because the
arrs exchange paths over their APIs and those paths must agree:

```yaml
services:
  sonarr:
    volumes:
      - /mnt/disk2/tv:/data/media/tv2
  jellyfin:
    volumes:
      - /mnt/disk2/tv:/data/media/tv2
```

Then add `/data/media/tv2` as a root folder in Sonarr and a library folder
in Jellyfin — Arrsenal only manages the default ones.

## Add a container of your own

Anything joining the `arrsenal` network can reach every app by container
name (`http://sonarr:8989`), the same way the apps reach each other:

```yaml
services:
  uptime-kuma:
    image: louislam/uptime-kuma:1
    container_name: uptime-kuma
    restart: unless-stopped
    networks: [arrsenal]
    ports:
      - "3001:3001"
    volumes:
      - /opt/appdata/uptime-kuma:/app/data
```

No `networks:` definition needed — the override merges into the compose
file that already defines it.

## Give a container more (or less) resources

```yaml
services:
  jellyfin:
    mem_limit: 4g
    cpus: 2
```

## Add environment variables

```yaml
services:
  qbittorrent:
    environment:
      DOCKER_MODS: ghcr.io/gabe565/linuxserver-mod-vuetorrent
```

Merges *into* the environment Arrsenal generates — you're adding keys, not
replacing the block.

## Things that belong in Arrsenal, not the override

- **Ports** — re-run `arrsenal`; preflight offers remaps and keeps the
  wiring consistent with them. An override-file port change would fight the
  generated file and confuse the wiring URLs.
- **PUID/PGID, timezone, storage roots** — state-file answers; re-run
  `arrsenal` to change them.
- **Jellyfin host networking** (DLNA/discovery) — a first-class option:
  `--jellyfin-host-network`, or the settings screen.
- **The VPN** — `--vpn-provider` and friends; the kill-switch topology is
  generated, don't hand-build it.

If an override fights the generated file (same key, different value), the
override wins — that's Compose's merge rule — but wiring assumes the
generated topology, so keep overrides additive where you can.
