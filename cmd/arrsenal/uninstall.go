package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Haroutio/arrsenal/internal/dockerx"
	"github.com/Haroutio/arrsenal/internal/state"
)

// runUninstall is `arrsenal uninstall`: stop and remove the containers and
// network, keep every byte of data (DESIGN §1 iron rule). Reversible by
// definition — re-running arrsenal brings the stack back with configs
// intact.
//
// --purge additionally deletes the SELECTED apps' appdata directories, the
// state file, and the generated artifacts — after a typed confirmation that
// names exactly what dies. Scope is deliberate: only the app dirs Arrsenal
// manages, never the whole appdata root (it may host other things), and
// never anything under the media or downloads roots.
func runUninstall(o options, purge bool) error {
	s, err := state.Load(o.statePath)
	if errors.Is(err, state.ErrNotExist) {
		return fmt.Errorf("no state file at %s — nothing installed to uninstall", o.statePath)
	}
	if err != nil {
		return err
	}

	docker := dockerx.New()
	if err := docker.Available(); err != nil {
		return err
	}

	fmt.Println("stopping and removing containers…")
	if err := docker.Down(o.artifactsDir); err != nil {
		return err
	}

	if !purge {
		fmt.Println("\nPreserved (uninstall is reversible — run arrsenal to bring everything back):")
		for _, id := range s.Apps {
			fmt.Printf("  %s\n", filepath.Join(s.AppdataRoot, id))
		}
		if s.TRaSH.Enabled {
			fmt.Printf("  %s\n", filepath.Join(s.AppdataRoot, "recyclarr"))
		}
		fmt.Printf("  %s\n", o.statePath)
		fmt.Printf("  %s/docker-compose.yml + .env\n", o.artifactsDir)
		fmt.Println("Media under the data root was never touched.")
		return nil
	}

	// The purge gate: type the word, or nothing is deleted. Piped stdin
	// works (scriptable) but there is no flag to bypass — deletion of
	// configuration is always an explicit act.
	fmt.Println("\n--purge will PERMANENTLY DELETE:")
	var doomed []string
	for _, id := range s.Apps {
		doomed = append(doomed, filepath.Join(s.AppdataRoot, id))
	}
	if s.TRaSH.Enabled {
		// Arrsenal-managed like any app dir, and it holds the arrs' API
		// keys in recyclarr.yml — a purge that leaves it behind breaks the
		// "deletes exactly the managed scope" promise (audit finding).
		doomed = append(doomed, filepath.Join(s.AppdataRoot, "recyclarr"))
	}
	doomed = append(doomed,
		o.statePath,
		filepath.Join(o.artifactsDir, "docker-compose.yml"),
		filepath.Join(o.artifactsDir, ".env"),
	)
	for _, p := range doomed {
		fmt.Printf("  %s\n", p)
	}
	fmt.Println("Media under the data root is NOT deleted and never will be.")
	fmt.Print("Type 'purge' to confirm: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != "purge" {
		return errors.New("aborted — nothing was deleted")
	}

	var failed []string
	for _, p := range doomed {
		if err := os.RemoveAll(p); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", p, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("some paths could not be removed:\n  %s", strings.Join(failed, "\n  "))
	}
	fmt.Println("purged. The media library was not touched.")
	return nil
}
