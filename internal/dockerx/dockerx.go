package dockerx

import (
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes a docker CLI invocation and returns stdout. Injectable so
// everything above it is testable without a daemon.
type Runner func(args ...string) (string, error)

// Docker is a thin veneer over the docker CLI. It shells out on purpose:
// the CLI is the stable, universally-present interface (the compose plugin
// has no usable Go API), and preflight/bring-up need nothing more.
type Docker struct {
	run Runner
}

// New returns a Docker that shells out to the real CLI.
func New() *Docker {
	return &Docker{run: func(args ...string) (string, error) {
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out), nil
	}}
}

// NewWithRunner returns a Docker backed by a fake, for tests.
func NewWithRunner(r Runner) *Docker {
	return &Docker{run: r}
}

// Available reports whether the docker CLI and compose plugin respond.
func (d *Docker) Available() error {
	if _, err := d.run("version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("docker is not available or not running: %w", err)
	}
	if _, err := d.run("compose", "version"); err != nil {
		return fmt.Errorf("the docker compose plugin is missing: %w", err)
	}
	return nil
}

// Containers returns every container on the host (running or not), mapped
// name → compose project label (empty when not compose-managed). The label
// is how preflight tells "someone else's sonarr" from "ours, from the
// previous run" (DESIGN.md §4).
func (d *Docker) Containers() (map[string]string, error) {
	out, err := d.run("ps", "-a", "--format", `{{.Names}}\t{{.Label "com.docker.compose.project"}}`)
	if err != nil {
		return nil, err
	}
	containers := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, project, _ := strings.Cut(line, "\t")
		containers[name] = project
	}
	return containers, nil
}
