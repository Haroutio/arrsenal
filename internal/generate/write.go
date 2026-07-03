package generate

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFiles lands the artifacts in dir (creating it 0755 if needed) as
// docker-compose.yml and .env, both 0644 — they contain no secrets by
// construction (pinned by test). docker-compose.override.yml in the same
// directory is the user's and is never touched here or anywhere.
func WriteFiles(dir string, a Artifacts) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	for name, content := range map[string][]byte{
		"docker-compose.yml": a.Compose,
		".env":               a.Env,
	} {
		path := filepath.Join(dir, name)
		tmp, err := os.CreateTemp(dir, "."+name+"-*")
		if err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		tmpName := tmp.Name()
		cleanup := func() { _ = os.Remove(tmpName) }
		if _, err := tmp.Write(content); err != nil {
			_ = tmp.Close()
			cleanup()
			return fmt.Errorf("writing %s: %w", path, err)
		}
		if err := tmp.Chmod(0o644); err != nil {
			_ = tmp.Close()
			cleanup()
			return fmt.Errorf("setting mode on %s: %w", path, err)
		}
		if err := tmp.Close(); err != nil {
			cleanup()
			return fmt.Errorf("closing %s: %w", path, err)
		}
		if err := os.Rename(tmpName, path); err != nil {
			cleanup()
			return fmt.Errorf("replacing %s: %w", path, err)
		}
	}
	return nil
}
