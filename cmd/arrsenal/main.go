// Command arrsenal is the Arrsenal installer: an interactive TUI that stands up
// a complete, auto-wired servarr media stack on a Linux host.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

// version is stamped by goreleaser at release time; "dev" in local builds.
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	fmt.Printf("arrsenal %s\n", version)
	fmt.Println("Nothing to drive yet — the TUI lands with the v0.1 milestone.")
	fmt.Println("Track progress: https://github.com/Haroutio/arrsenal/milestones")
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "note: arrsenal targets Linux hosts; this build is for development only")
	}
}
