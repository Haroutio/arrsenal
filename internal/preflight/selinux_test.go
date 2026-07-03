package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSELinuxDetection(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		p := filepath.Join(dir, "enforce")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if got := checkSELinuxAt(write("1")); !got.Enforcing {
		t.Fatal("enforce=1 must report enforcing")
	} else {
		for _, want := range []string{"permission denied", "override", "Tier 2"} {
			if !strings.Contains(got.Warning, want) {
				t.Errorf("warning should mention %q: %s", want, got.Warning)
			}
		}
	}
	if got := checkSELinuxAt(write("0")); got.Enforcing {
		t.Fatal("permissive (0) must not warn")
	}
	if got := checkSELinuxAt(filepath.Join(dir, "missing")); got.Enforcing {
		t.Fatal("no selinuxfs must not warn")
	}
}
