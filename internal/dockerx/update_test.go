package dockerx

import (
	"strings"
	"testing"
)

func TestPullRunsComposePullInArtifactsDir(t *testing.T) {
	var gotDir string
	var gotArgs []string
	d := NewWithRunner(nil, func(dir string, args ...string) (string, error) {
		gotDir, gotArgs = dir, args
		return "sonarr Pulled\n", nil
	})
	out, err := d.Pull("/opt/arrsenal")
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != "/opt/arrsenal" || strings.Join(gotArgs, " ") != "compose pull" {
		t.Fatalf("ran %q in %q", strings.Join(gotArgs, " "), gotDir)
	}
	if !strings.Contains(out, "Pulled") {
		t.Fatalf("output passthrough: %q", out)
	}
}

func TestUpScopedSkipsRemoveOrphans(t *testing.T) {
	var gotArgs []string
	d := NewWithRunner(nil, func(_ string, args ...string) (string, error) {
		gotArgs = args
		return "", nil
	})
	if err := d.Up("/x", "sonarr", "prowlarr"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if joined != "compose up -d sonarr prowlarr" {
		t.Fatalf("scoped up args: %q", joined)
	}
	// Full up keeps reconciliation.
	if err := d.Up("/x"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotArgs, " ") != "compose up -d --remove-orphans" {
		t.Fatalf("full up args: %q", strings.Join(gotArgs, " "))
	}
}
