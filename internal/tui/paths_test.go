package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/state"
)

func fixtureMounts() []preflight.MountInfo {
	return []preflight.MountInfo{
		{Target: "/mnt/das", FSType: "xfs", FreeBytes: 54 * 1024 * 1024 * 1024 * 1024},
		{Target: "/", FSType: "ext4", FreeBytes: 38 * 1024 * 1024 * 1024},
	}
}

func pathsEnter(m PathsModel, s *state.State) PathsModel {
	next, _ := m.UpdateWith(tea.KeyMsg{Type: tea.KeyEnter}, s)
	return next
}

func TestPathsShowsMountTableWithOSDiskTag(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	view := NewPaths(s, fixtureMounts()).View()
	for _, want := range []string{"/mnt/das", "54.0 TiB free", "(OS disk)", "/data", "/opt/appdata"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestPathsOSDiskNeedsDoubleConfirm(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"} // default /data is on "/" in the fixture
	m := NewPaths(s, fixtureMounts())

	m = pathsEnter(m, s)
	if m.Done() {
		t.Fatal("first enter on an OS-disk data root must warn, not proceed")
	}
	if !strings.Contains(m.View(), "OS disk") || !strings.Contains(m.View(), "38.0 GiB") {
		t.Fatalf("warning must name the disk and its free space: %s", m.osWarn)
	}
	m = pathsEnter(m, s)
	if !m.Done() {
		t.Fatal("second enter accepts the risk")
	}
}

func TestPathsRealStorageProceedsDirectly(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	s.DataRoot = "/mnt/das/data"
	m := NewPaths(s, fixtureMounts())
	m = pathsEnter(m, s)
	if !m.Done() {
		t.Fatalf("real storage needs no double-confirm: %s / %s", m.err, m.osWarn)
	}
	m.Apply(s)
	if s.DataRoot != "/mnt/das/data" {
		t.Fatalf("apply lost the path: %s", s.DataRoot)
	}
}

func TestPathsValidationBlocksBadPaths(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	m := NewPaths(s, fixtureMounts())
	m.inputs[0].SetValue("relative/path")
	m = pathsEnter(m, s)
	if m.Done() || m.err == "" {
		t.Fatal("relative data root must be rejected inline")
	}
}
