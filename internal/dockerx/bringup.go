package dockerx

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RunnerIn executes a docker CLI invocation in a working directory.
// Compose resolves docker-compose.yml, .env and any override file from the
// working directory, so bring-up must run "in" the artifacts dir.
type RunnerIn func(dir string, args ...string) (string, error)

// defaultRunnerIn shells out for real.
func defaultRunnerIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// Up runs the reconciliation: `docker compose up -d --remove-orphans` in the
// artifacts directory. Compose itself is the declarative engine — deselected
// apps' containers are removed, changed services recreated (DESIGN.md §1);
// Arrsenal adds nothing on top.
func (d *Docker) Up(artifactsDir string) error {
	_, err := d.runIn(artifactsDir, "compose", "up", "-d", "--remove-orphans")
	if err != nil {
		return fmt.Errorf("bring-up failed: %w", err)
	}
	return nil
}

// ValidateCompose asks compose itself whether the generated artifacts parse —
// the authoritative answer, from the same binary that will run them.
func (d *Docker) ValidateCompose(artifactsDir string) error {
	if _, err := d.runIn(artifactsDir, "compose", "config", "--quiet"); err != nil {
		return fmt.Errorf("generated compose file failed validation: %w", err)
	}
	return nil
}

// ReadyResult is one container's readiness verdict.
type ReadyResult struct {
	App    string
	Ready  bool
	Detail string // on failure: what state it is in and the exact logs command
}

// WaitReady polls each container until it is running (and healthy, when the
// image defines a healthcheck) or the timeout passes. Results come back in
// input order; a failure names the container's actual state and the one
// command to investigate it (DESIGN.md §5 error style).
func (d *Docker) WaitReady(apps []string, timeout, poll time.Duration) []ReadyResult {
	deadline := time.Now().Add(timeout)
	results := make([]ReadyResult, len(apps))
	pending := map[int]string{}
	for i, app := range apps {
		pending[i] = app
	}

	for len(pending) > 0 {
		for i, app := range pending {
			status, health, err := d.containerState(app)
			switch {
			case err == nil && status == "running" && (health == "" || health == "healthy"):
				results[i] = ReadyResult{App: app, Ready: true}
				delete(pending, i)
			case err == nil && (status == "exited" || status == "dead"):
				// A crashed container will not un-crash by waiting.
				results[i] = ReadyResult{App: app, Ready: false, Detail: failDetail(app, status, health)}
				delete(pending, i)
			}
		}
		if len(pending) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(poll)
	}

	for i, app := range pending {
		status, health, err := d.containerState(app)
		detail := failDetail(app, status, health)
		if err != nil {
			detail = fmt.Sprintf("%s: no such container after bring-up — check: docker compose ps", app)
		}
		results[i] = ReadyResult{App: app, Ready: false, Detail: detail}
	}
	return results
}

func failDetail(app, status, health string) string {
	s := status
	if health != "" {
		s += "/" + health
	}
	return fmt.Sprintf("%s is %s — check: docker logs %s", app, s, app)
}

// containerState returns docker's view of one container.
func (d *Docker) containerState(name string) (status, health string, err error) {
	out, err := d.run("inspect", "--format",
		`{{.State.Status}}\t{{if .State.Health}}{{.State.Health.Status}}{{end}}`, name)
	if err != nil {
		return "", "", err
	}
	status, health, _ = strings.Cut(strings.TrimSpace(out), "\t")
	return status, health, nil
}
