#!/usr/bin/env bash
# Arrsenal bootstrap — https://github.com/Haroutio/arrsenal
#
# What this script does, in order (and nothing else):
#   1. Refuses environments that cannot work (WSL2, Docker-in-LXC).
#   2. Detects your distro; offers to install Docker if it is missing —
#      per item, with a prompt. Nothing installs silently.
#   3. Downloads the arrsenal release binary for your architecture and
#      verifies its SHA-256 against the release checksums file.
#   4. Hands over to the arrsenal TUI.
#
# It never asks to be piped into sudo; it requests privilege per action and
# says why first. Prefer to read before you run? Good instinct — this file
# is short on purpose.
set -euo pipefail

REPO="Haroutio/arrsenal"
INSTALL_DIR="/usr/local/bin"
# Pin a version: ARRSENAL_VERSION=v0.1.0 ./install.sh   (default: latest release)
ARRSENAL_VERSION="${ARRSENAL_VERSION:-latest}"
# Non-interactive: ARRSENAL_YES=1 answers every prompt's default-yes; prompts
# whose default is No still refuse (best-effort paths need a human).
ARRSENAL_YES="${ARRSENAL_YES:-}"

say()  { printf '\033[1;36marrsenal>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33marrsenal!\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31marrsenal✗\033[0m %s\n' "$*" >&2; exit 1; }

# ask "question" default(y|n) → 0 yes / 1 no. Reads /dev/tty because stdin
# is the script itself under curl|bash. With no terminal to ask on, it DIES
# rather than assume: a headless pipe must never trigger a default-yes
# install ("nothing installs silently" is the whole promise).
ask() {
  local q="$1" def="${2:-n}" reply
  if [ -n "$ARRSENAL_YES" ]; then
    [ "$def" = "y" ] && return 0 || return 1
  fi
  if [ "$def" = "y" ]; then q="$q [Y/n] "; else q="$q [y/N] "; fi
  if ! read -r -p "$q" reply < /dev/tty; then
    die "no terminal to ask on — re-run interactively, or set ARRSENAL_YES=1 to accept the default-yes steps."
  fi
  reply="${reply:-$def}"
  case "$reply" in [Yy]*) return 0 ;; *) return 1 ;; esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64)  echo amd64 ;;
    aarch64) echo arm64 ;;
    *) return 1 ;;
  esac
}

# Probe inputs are overridable so the bats suite tests the REAL functions
# against fixture files, not copies of the logic.
PROC_VERSION_FILE="${ARRSENAL_PROC_VERSION:-/proc/version}"
PID1_ENVIRON_FILE="${ARRSENAL_PID1_ENVIRON:-/proc/1/environ}"
OS_RELEASE_FILE="${ARRSENAL_OS_RELEASE:-/etc/os-release}"

is_wsl() {
  grep -qi microsoft "$PROC_VERSION_FILE" 2>/dev/null
}

is_lxc() {
  if [ -z "${ARRSENAL_PID1_ENVIRON:-}" ] && command -v systemd-detect-virt >/dev/null 2>&1; then
    [ "$(systemd-detect-virt --container 2>/dev/null)" = "lxc" ] && return 0
  fi
  grep -qa 'container=lxc' "$PID1_ENVIRON_FILE" 2>/dev/null
}

# distro_tier → tier1 | coverable | manual (echoes; reads os-release)
distro_tier() {
  local id="" version_id=""
  if [ -r "$OS_RELEASE_FILE" ]; then
    # shellcheck disable=SC1090,SC1091
    . "$OS_RELEASE_FILE"
    id="${ID:-}"
    version_id="${VERSION_ID:-0}"
  fi
  local major="${version_id%%.*}"
  case "$id" in
    debian) [ "${major:-0}" -ge 12 ] && echo tier1 || echo coverable ;;
    ubuntu) [ "${major:-0}" -ge 22 ] && echo tier1 || echo coverable ;;
    fedora|rhel|centos|rocky|almalinux|sles|opensuse*|raspbian) echo coverable ;;
    *) echo manual ;;
  esac
}

docker_ready() {
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1
}

install_docker() {
  say "Docker will be installed using Docker's own official installer (get.docker.com)."
  say "That script adds Docker's apt/yum repository and installs the engine + compose plugin."
  say "This needs root: the next step runs it with sudo."
  curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
  sudo sh /tmp/get-docker.sh
  rm -f /tmp/get-docker.sh
}

resolve_version() {
  if [ "$ARRSENAL_VERSION" != "latest" ]; then
    echo "$ARRSENAL_VERSION"
    return
  fi
  # The releases/latest redirect carries the tag; no API token, no jq.
  local location
  location=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
  local tag="${location##*/}"
  if [ -z "$tag" ] || [ "$tag" = "releases" ]; then
    die "cannot resolve the latest release — no releases published yet?"
  fi
  echo "$tag"
}

download_and_verify() {
  local tag="$1" arch="$2" tmp="$3"
  local version="${tag#v}"
  local asset="arrsenal_${version}_linux_${arch}.tar.gz"
  local base="https://github.com/$REPO/releases/download/$tag"

  say "Downloading $asset ($tag)…"
  curl -fsSL -o "$tmp/$asset" "$base/$asset"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

  say "Verifying SHA-256 checksum…"
  (cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c -) \
    || die "checksum verification FAILED — refusing to run the binary. Re-download or open an issue."

  tar -xzf "$tmp/$asset" -C "$tmp" arrsenal
}

main() {
  say "Arrsenal bootstrap"

  [ "$(uname -s)" = "Linux" ] || die "arrsenal runs media servers on Linux hosts only."
  is_wsl && die "WSL2 detected. A media server needs a real Linux host or VM — WSL2 networking and storage break in ways nobody can debug. See: https://github.com/$REPO#support-tiers"
  is_lxc && die "Docker-in-LXC detected. This is unsupported — nested container runtimes fail in subtle ways. Use a VM: https://github.com/$REPO#support-tiers"

  local arch
  arch=$(detect_arch) || die "unsupported architecture: $(uname -m) (amd64 and arm64 only)"

  if docker_ready; then
    say "Docker + compose plugin: found."
  else
    case "$(distro_tier)" in
      tier1)
        say "Docker is not installed. This distro is fully supported (Tier 1)."
        ask "Install Docker now?" y || die "Docker is required. Install it and re-run."
        install_docker
        ;;
      coverable)
        warn "This distro is not fully tested with Arrsenal (Tier 2 once Docker is present)."
        ask "Try installing Docker via Docker's official installer, best-effort?" n \
          || die "Install Docker yourself, then re-run — everything after that point is fully supported. https://docs.docker.com/engine/install/"
        install_docker
        ;;
      *)
        die "Docker is missing and this distro has no supported auto-install. Install Docker + the compose plugin (https://docs.docker.com/engine/install/), then re-run."
        ;;
    esac
    docker_ready || die "Docker still is not usable after installation — check 'docker compose version'."
  fi

  local tag
  tag=$(resolve_version)
  # tmp is deliberately NOT local: the EXIT trap can fire after main returns
  # (ARRSENAL_NO_EXEC skips the exec), where a local would be unbound under
  # set -u. On the exec path the trap never fires at all — so also clean up
  # explicitly before handing over, instead of leaking a /tmp dir per install.
  tmp=$(mktemp -d)
  trap 'rm -rf "${tmp:-}"' EXIT

  download_and_verify "$tag" "$arch" "$tmp"

  say "Installing to $INSTALL_DIR/arrsenal (needs sudo for the copy)…"
  sudo install -m 0755 "$tmp/arrsenal" "$INSTALL_DIR/arrsenal"

  say "Done. Starting arrsenal — re-run it any time with: sudo arrsenal"
  if [ -z "${ARRSENAL_NO_EXEC:-}" ]; then
    rm -rf "$tmp"
    trap - EXIT
    exec sudo "$INSTALL_DIR/arrsenal"
  fi
}

# Sourcing guard so tests can exercise the functions without running main.
if [ -z "${ARRSENAL_SOURCED:-}" ]; then
  main "$@"
fi
