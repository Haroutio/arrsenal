package dockerx

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Image pulling with progress (issue #115): Arrsenal draws its own
// indicators instead of dumping docker's output. Docker's pipe-mode pull
// output carries no byte counts — only layer status lines — so progress is
// measured in layers: "Pulling fs layer" discovers one, "Pull complete"
// (or "Already exists") finishes one. Chunky for big layers, but honest,
// and it moves.

// ComposeImages returns every image the generated compose file references,
// overrides included — compose itself resolves the merged model, so a
// user-pinned image in docker-compose.override.yml is counted too.
func (d *Docker) ComposeImages(artifactsDir string) ([]string, error) {
	out, err := d.runIn(artifactsDir, "compose", "config", "--images")
	if err != nil {
		return nil, fmt.Errorf("listing compose images: %w", err)
	}
	var images []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" && !seen[line] {
			images = append(images, line)
			seen[line] = true
		}
	}
	return images, nil
}

// ImagePresent reports whether an image ref exists locally.
func (d *Docker) ImagePresent(ref string) bool {
	_, err := d.run("image", "inspect", "--format", "{{.Id}}", ref)
	return err == nil
}

// PullProgress runs `docker pull ref`, reporting layer completion through
// onProgress(done, total). The raw docker output never reaches the
// terminal; on failure the tail of it comes back in the error.
func (d *Docker) PullProgress(ref string, onProgress func(done, total int)) error {
	if d.pull != nil { // test seam
		return d.pull(ref, onProgress)
	}
	cmd := exec.Command("docker", "pull", ref)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // pipe errors through the same scanner
	if err := cmd.Start(); err != nil {
		return err
	}

	tail := parsePullStream(stdout, onProgress)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pulling %s: %v\n%s", ref, err, strings.Join(tail, "\n"))
	}
	return nil
}

// parsePullStream reads docker pull's pipe-mode output, reporting layer
// completion, and returns the output tail for error context.
func parsePullStream(r io.Reader, onProgress func(done, total int)) []string {
	var tail []string
	layers := map[string]bool{} // layer id → finished
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		tail = append(tail, line)
		if len(tail) > 12 {
			tail = tail[1:]
		}
		id, status, ok := strings.Cut(line, ": ")
		if !ok || strings.Contains(id, " ") {
			continue // "Digest:", "Status:", pull headers
		}
		switch status {
		case "Pulling fs layer", "Waiting", "Download complete":
			// discovery only — "Download complete" precedes extraction, so
			// it is not DONE done.
			if _, known := layers[id]; !known {
				layers[id] = false
			}
		case "Pull complete", "Already exists":
			layers[id] = true
		}
		if onProgress != nil && len(layers) > 0 {
			done := 0
			for _, fin := range layers {
				if fin {
					done++
				}
			}
			onProgress(done, len(layers))
		}
	}
	return tail
}
