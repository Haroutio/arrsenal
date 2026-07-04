package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Haroutio/arrsenal/internal/dockerx"
	"github.com/Haroutio/arrsenal/internal/quality"
	"github.com/Haroutio/arrsenal/internal/state"
)

// runUpdate is `arrsenal update`: pull fresh images for the generated stack
// and let compose recreate whatever changed. The state file is the source of
// truth for what "the stack" is; no state file means nothing to update.
func runUpdate(o options) error {
	s, err := state.Load(o.statePath)
	if errors.Is(err, state.ErrNotExist) {
		return fmt.Errorf("no state file at %s — nothing installed to update (run arrsenal first)", o.statePath)
	}
	if err != nil {
		return err
	}

	docker := dockerx.New()
	if err := docker.Available(); err != nil {
		return err
	}

	fmt.Println("pulling image updates…")
	out, err := docker.Pull(o.artifactsDir)
	if err != nil {
		return err
	}
	// Compose's own progress is noisy; keep the informative tail lines.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Pulled") || strings.Contains(line, "Downloaded") {
			fmt.Println(" ", line)
		}
	}
	// When TRaSH is enabled recyclarr is a compose service (issue #106) and
	// the pull above covers it — but only in compose files generated since
	// then. Artifacts from an older arrsenal predate the service, so the
	// explicit pull stays for them and for the wiring one-shot (audit
	// finding).
	if s.TRaSH.Enabled {
		if err := docker.PullImage(quality.Image); err != nil {
			return err
		}
	}

	fmt.Println("reconciling containers…")
	if err := docker.Up(o.artifactsDir); err != nil {
		return err
	}
	ready := docker.WaitReady(s.Apps, 3*time.Minute, 2*time.Second)
	printReadiness(ready)
	for _, r := range ready {
		if !r.Ready {
			return fmt.Errorf("update finished but some containers are not ready — see above")
		}
	}
	fmt.Println("done — images current, stack reconciled.")
	return nil
}
