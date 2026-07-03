package generate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFilesLandsArtifactsAndLeavesOverrideAlone(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "docker-compose.override.yml")
	if err := os.WriteFile(override, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := render(t, goldenCases()["minimal"])
	if err := WriteFiles(dir, got); err != nil {
		t.Fatal(err)
	}

	compose, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil || len(compose) == 0 {
		t.Fatalf("compose not written: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || len(env) == 0 {
		t.Fatalf(".env not written: %v", err)
	}

	// The escape hatch survives regeneration, byte for byte.
	b, err := os.ReadFile(override)
	if err != nil || string(b) != "user content" {
		t.Fatalf("override file disturbed: %q %v", b, err)
	}

	// Idempotent rewrite, no temp debris.
	if err := WriteFiles(dir, render(t, goldenCases()["minimal"])); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("want exactly compose + .env + override, got %v", names)
	}
}
