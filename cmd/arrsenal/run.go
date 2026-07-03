package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/dockerx"
	"github.com/Haroutio/arrsenal/internal/generate"
	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/state"
	"github.com/Haroutio/arrsenal/internal/tui"
)

func run(o options) error {
	s, fresh, err := loadOrNewState(o.statePath)
	if err != nil {
		return err
	}
	applyFlagOverrides(s, o, fresh)

	if o.yes {
		if err := headlessFill(s, o); err != nil {
			return err
		}
	} else if err := interactiveFill(s, o); err != nil { // o reserved for future interactive knobs
		return err
	}

	return pipeline(s, o)
}

func loadOrNewState(path string) (*state.State, bool, error) {
	s, err := state.Load(path)
	if errors.Is(err, state.ErrNotExist) {
		return state.New(), true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return s, false, nil
}

// applyFlagOverrides copies explicitly-useful flag values into the state.
// Zero-ish values mean "keep what the state has".
func applyFlagOverrides(s *state.State, o options, fresh bool) {
	if o.dataRoot != "" {
		s.DataRoot = o.dataRoot
	}
	if o.appdataRoot != "" {
		s.AppdataRoot = o.appdataRoot
	}
	if o.umask != "" {
		s.Umask = o.umask
	}
	if o.tz != "" && (fresh || o.yes) {
		s.TZ = o.tz
	}
	if fresh || o.yes {
		s.PUID, s.PGID = o.puid, o.pgid
	}
	if o.gpu != "" {
		s.GPU = state.GPUMode(o.gpu)
	}
}

// headlessFill completes the state from flags alone (DESIGN.md §10).
func headlessFill(s *state.State, o options) error {
	if o.apps != "" {
		s.Apps = nil
		for _, id := range strings.Split(o.apps, ",") {
			if id = strings.TrimSpace(id); id != "" {
				s.Apps = append(s.Apps, id)
			}
		}
	}
	if len(s.Apps) == 0 {
		return errors.New("headless mode needs --apps (comma-separated IDs) or an existing state file")
	}
	if o.gpu == "" {
		det := preflight.DetectGPU(preflight.DefaultGPUProbes())
		s.GPU = det.Proposal
		fmt.Println("gpu:", det.Detail)
	}
	return s.Validate()
}

// interactiveFill drives the TUI screens in order, writing into the state.
func interactiveFill(s *state.State, _ options) error {
	// 1. App selection.
	sel := tui.NewSelect(s.Apps)
	if err := runScreen(&selectAdapter{&sel}); err != nil {
		return err
	}
	if sel.Quit() {
		return errors.New("aborted")
	}
	s.Apps = sel.Selected()
	for _, id := range sel.Removals() {
		fmt.Printf("note: %s will be removed; its config in %s is preserved\n", id, filepath.Join(s.AppdataRoot, id))
	}

	// 2. Identity & environment.
	set := tui.NewSettings(s)
	if err := runScreen(&settingsAdapter{&set, s}); err != nil {
		return err
	}
	if set.Quit() {
		return errors.New("aborted")
	}
	set.Apply(s)

	// 3. Storage.
	mounts, err := preflight.ListMounts()
	if err != nil {
		fmt.Println("warning:", err)
	}
	paths := tui.NewPaths(s, mounts)
	if err := runScreen(&pathsAdapter{&paths, s}); err != nil {
		return err
	}
	if paths.Quit() {
		return errors.New("aborted")
	}
	paths.Apply(s)

	// 4. GPU: detection proposes, the user disposes.
	det := preflight.DetectGPU(preflight.DefaultGPUProbes())
	fmt.Println(det.Detail)
	if det.ToolkitInstallHint != "" {
		fmt.Println(det.ToolkitInstallHint)
		fmt.Print(preflight.FormatToolkitPlan())
	}
	if det.Proposal != state.GPUNone && confirm(fmt.Sprintf("Use the detected GPU (%s)?", det.Proposal), true) {
		s.GPU = det.Proposal
	} else {
		s.GPU = state.GPUNone
	}

	return s.Validate()
}

// pipeline is the shared back half: scan → tree → checks → render → up.
func pipeline(s *state.State, o options) error {
	docker := dockerx.New()
	if err := docker.Available(); err != nil {
		return err
	}

	// Conflict scan; interactive mode gets the remap screen, headless fails.
	conflicts, err := preflight.ScanConflicts(s, preflight.DefaultDeps(docker.Containers))
	if err != nil {
		return err
	}
	var blocking []preflight.Conflict
	for _, c := range conflicts {
		if c.Blocking() {
			blocking = append(blocking, c)
		} else {
			fmt.Println("note:", c.Detail)
		}
	}
	if len(blocking) > 0 {
		if o.yes {
			var lines []string
			for _, c := range blocking {
				lines = append(lines, "  "+c.Detail)
			}
			return fmt.Errorf("blocking conflicts:\n%s", strings.Join(lines, "\n"))
		}
		remap := tui.NewRemap(blocking, s)
		if err := runScreen(&remapAdapter{&remap, s}); err != nil {
			return err
		}
		if remap.Quit() || remap.Back() {
			return errors.New("aborted — adjust your selection and re-run")
		}
		remap.Apply(s)
	}

	// Save early: the state is the source of truth from here on.
	if err := s.Save(o.statePath); err != nil {
		return err
	}

	dirs, err := preflight.EnsureTree(s)
	if err != nil {
		return err
	}
	created := 0
	for _, d := range dirs {
		if d.Created {
			created++
		}
	}
	fmt.Printf("directories: %d verified, %d created\n", len(dirs)-created, created)

	if hl := preflight.CheckHardlink(filepath.Join(s.DataRoot, "usenet"), filepath.Join(s.DataRoot, "media")); !hl.OK {
		fmt.Println("warning:", hl.Detail)
		if !o.yes && !confirm("Continue with copy-mode imports?", false) {
			return errors.New("aborted")
		}
	} else {
		fmt.Println(hl.Detail)
	}
	if se := preflight.CheckSELinux(); se.Enforcing {
		fmt.Println("warning:", se.Warning)
	}

	artifacts, err := generate.Render(s, o.statePath)
	if err != nil {
		return err
	}
	if err := generate.WriteFiles(o.artifactsDir, artifacts); err != nil {
		return err
	}
	fmt.Printf("generated: %s/docker-compose.yml + .env\n", o.artifactsDir)

	if err := docker.ValidateCompose(o.artifactsDir); err != nil {
		return err
	}
	if o.skipUp {
		fmt.Println("skip-up set: not starting containers")
		return nil
	}

	fmt.Println("bringing the stack up…")
	if err := docker.Up(o.artifactsDir); err != nil {
		return err
	}
	results := docker.WaitReady(s.Apps, 3*time.Minute, 2*time.Second)
	failed := 0
	for _, r := range results {
		if r.Ready {
			fmt.Printf("  ✓ %s\n", r.App)
		} else {
			failed++
			fmt.Printf("  ✗ %s\n", r.Detail)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d containers did not become ready", failed, len(results))
	}
	fmt.Println("done — the stack is up. Re-run arrsenal any time to add or remove apps.")
	return nil
}

// confirm asks on the terminal; def is the answer for a bare enter.
func confirm(q string, def bool) bool {
	suffix := " [y/N] "
	if def {
		suffix = " [Y/n] "
	}
	fmt.Print(q + suffix)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return def
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return strings.HasPrefix(line, "y")
}

// --- Bubble Tea adapters -------------------------------------------------
// Each screen keeps a pure UpdateWith for tests; these adapters bind them to
// tea.Program with the state threaded through.

func runScreen(m tea.Model) error {
	_, err := tea.NewProgram(m).Run()
	return err
}

type selectAdapter struct{ m *tui.SelectModel }

func (a *selectAdapter) Init() tea.Cmd { return a.m.Init() }
func (a *selectAdapter) View() string  { return a.m.View() }
func (a *selectAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.Update(msg)
	*a.m = next.(tui.SelectModel)
	if a.m.Done() {
		return a, tea.Quit
	}
	return a, cmd
}

type settingsAdapter struct {
	m *tui.SettingsModel
	s *state.State
}

func (a *settingsAdapter) Init() tea.Cmd { return a.m.Init() }
func (a *settingsAdapter) View() string  { return a.m.View() }
func (a *settingsAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.UpdateWith(msg, a.s)
	*a.m = next
	if a.m.Done() {
		return a, tea.Quit
	}
	return a, cmd
}

type pathsAdapter struct {
	m *tui.PathsModel
	s *state.State
}

func (a *pathsAdapter) Init() tea.Cmd { return nil }
func (a *pathsAdapter) View() string  { return a.m.View() }
func (a *pathsAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.UpdateWith(msg, a.s)
	*a.m = next
	if a.m.Done() {
		return a, tea.Quit
	}
	return a, cmd
}

type remapAdapter struct {
	m *tui.RemapModel
	s *state.State
}

func (a *remapAdapter) Init() tea.Cmd { return nil }
func (a *remapAdapter) View() string  { return a.m.View() }
func (a *remapAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.m.UpdateWith(msg, a.s)
	*a.m = next
	if a.m.Done() || a.m.Back() {
		return a, tea.Quit
	}
	return a, cmd
}
