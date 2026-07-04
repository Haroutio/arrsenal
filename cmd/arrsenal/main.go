// Command arrsenal is the Arrsenal installer: an interactive TUI that stands
// up a complete, auto-wired servarr media stack on a Linux host. With --yes
// it runs headless from flags instead (CI, scripts). Subcommands handle the
// lifecycle: `arrsenal update` pulls fresh images and reconciles.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/state"
	"github.com/Haroutio/arrsenal/internal/wire"
)

// version is stamped by goreleaser at release time; "dev" in local builds.
var version = "dev"

func main() {
	args := os.Args[1:]
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}

	opts := parseFlags(args, os.Stdout)
	if opts == nil {
		return // --version or --help handled
	}

	var err error
	switch command {
	case "":
		err = run(*opts)
	case "update":
		err = runUpdate(*opts)
	case "uninstall":
		err = runUninstall(*opts, opts.purge)
	default:
		err = fmt.Errorf("unknown command %q (commands: update, uninstall; no command = install/reconfigure)", command)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "arrsenal: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	yes          bool
	apps         string
	dataRoot     string
	appdataRoot  string
	puid, pgid   int
	tz           string
	umask        string
	gpu          string
	statePath    string
	artifactsDir string
	skipUp       bool
	skipWiring   bool
	adminUser    string
	adminPass    string
	purge        bool

	downloadsRoot   string
	jellyfinHostNet bool

	vpnProvider       string
	usenetProvider    string
	usenetUser        string
	usenetPass        string
	usenetPort        int
	usenetConnections int
	indexerName       string
	indexerURL        string
	indexerKey        string
	// indexers collects interactively-entered indexers (no flag; the flag
	// trio above feeds one more via resolveIndexers).
	indexers     []wire.NewznabIndexer
	vpnKey       string
	vpnCountries string
	plexClaim    string

	trash           bool
	trashResolution string
	trashSource     string
	trashAnime      bool
}

// parseFlags returns nil when the invocation was informational (--version).
func parseFlags(args []string, out *os.File) *options {
	var o options
	fs := flag.NewFlagSet("arrsenal", flag.ExitOnError)
	showVersion := fs.Bool("version", false, "print version and exit")

	defaultUID, defaultGID := preflight.DetectIdentity()
	fs.BoolVar(&o.yes, "yes", false, "run non-interactively from flags (no TUI)")
	fs.StringVar(&o.apps, "apps", "", "comma-separated app IDs (headless mode)")
	fs.StringVar(&o.dataRoot, "data-root", "", "shared data root (default from state file or /data)")
	fs.StringVar(&o.appdataRoot, "appdata-root", "", "app config root (default from state file or /opt/appdata)")
	fs.IntVar(&o.puid, "puid", defaultUID, "user ID the containers run as")
	fs.IntVar(&o.pgid, "pgid", defaultGID, "group ID the containers run as")
	fs.StringVar(&o.tz, "tz", preflight.DetectTZ(), "IANA timezone for the containers")
	fs.StringVar(&o.umask, "umask", "", "umask for created files (default from state or 002)")
	fs.StringVar(&o.gpu, "gpu", "", "gpu mode: none|nvidia|intel|amd (default: detect)")
	fs.StringVar(&o.statePath, "state", state.DefaultPath, "state file location")
	fs.StringVar(&o.artifactsDir, "artifacts-dir", "", "where compose/.env land (default: state file's directory)")
	fs.BoolVar(&o.skipUp, "skip-up", false, "generate artifacts but do not run docker compose up")
	fs.BoolVar(&o.skipWiring, "skip-wiring", false, "bring containers up but skip the API auto-wiring pass")
	fs.StringVar(&o.adminUser, "admin-user", "admin", "admin username applied to the apps during wiring")
	fs.StringVar(&o.adminPass, "admin-pass", "", "admin password for wiring (headless; interactive mode prompts)")
	fs.BoolVar(&o.purge, "purge", false, "with the uninstall command: also delete app configs, state, and artifacts (typed confirmation required)")
	fs.StringVar(&o.downloadsRoot, "downloads-root", "", "put the download trees on their own filesystem (blank = under data root, hardlink imports)")
	fs.BoolVar(&o.jellyfinHostNet, "jellyfin-host-network", false, "run Jellyfin on host networking (DLNA/discovery)")
	fs.StringVar(&o.vpnProvider, "vpn-provider", "", "route qBittorrent through gluetun with this VPN provider (wireguard)")
	fs.StringVar(&o.usenetProvider, "usenet-provider", "", "news server for SABnzbd: a preset (newshosting, eweka, usenetserver, frugal, easynews) or a hostname")
	fs.StringVar(&o.usenetUser, "usenet-user", "", "usenet provider username")
	fs.StringVar(&o.usenetPass, "usenet-pass", "", "usenet provider password")
	fs.IntVar(&o.usenetPort, "usenet-port", 0, "usenet provider port (default: the preset's, or 563 TLS)")
	fs.IntVar(&o.usenetConnections, "usenet-connections", 0, "usenet connection count (default: the preset's, or 20)")
	fs.StringVar(&o.indexerName, "indexer-name", "", "a usenet indexer to add to Prowlarr (with --indexer-url and --indexer-key)")
	fs.StringVar(&o.indexerURL, "indexer-url", "", "the indexer's URL (generic Newznab)")
	fs.StringVar(&o.indexerKey, "indexer-key", "", "the indexer's API key")
	fs.StringVar(&o.vpnKey, "vpn-wireguard-key", "", "wireguard private key for the VPN (persisted 0600 in the state file)")
	fs.StringVar(&o.vpnCountries, "vpn-countries", "", "optional comma-separated server countries for the VPN")
	fs.StringVar(&o.plexClaim, "plex-claim", "", "claim token from https://plex.tv/claim (valid 4 minutes; first boot only)")
	fs.BoolVar(&o.trash, "trash", false, "apply TRaSH-guide quality settings and naming scheme to Sonarr/Radarr")
	fs.StringVar(&o.trashResolution, "trash-resolution", "1080p", "TRaSH profile resolution: 1080p or 2160p")
	fs.StringVar(&o.trashSource, "trash-source", "bluray-web", "TRaSH profile source: bluray-web or remux")
	fs.BoolVar(&o.trashAnime, "trash-anime", false, "also apply the TRaSH anime profiles")
	_ = fs.Parse(args)

	if *showVersion {
		_, _ = fmt.Fprintln(out, version)
		return nil
	}
	if o.artifactsDir == "" {
		o.artifactsDir = dirOf(o.statePath)
	}
	return &o
}

func dirOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return "."
}
