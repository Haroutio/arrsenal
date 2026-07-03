// Command arrsenal is the Arrsenal installer: an interactive TUI that stands
// up a complete, auto-wired servarr media stack on a Linux host. With --yes
// it runs headless from flags instead (CI, scripts).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/state"
)

// version is stamped by goreleaser at release time; "dev" in local builds.
var version = "dev"

func main() {
	opts := parseFlags(os.Args[1:], os.Stdout)
	if opts == nil {
		return // --version or --help handled
	}
	if err := run(*opts); err != nil {
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
