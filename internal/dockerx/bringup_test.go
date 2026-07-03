package dockerx

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestUpRunsComposeInTheArtifactsDir(t *testing.T) {
	var gotDir string
	var gotArgs []string
	d := NewWithRunner(nil, func(dir string, args ...string) (string, error) {
		gotDir, gotArgs = dir, args
		return "", nil
	})
	if err := d.Up("/opt/arrsenal"); err != nil {
		t.Fatal(err)
	}
	if gotDir != "/opt/arrsenal" {
		t.Fatalf("ran in %q, want /opt/arrsenal (compose resolves .env and the override file from the working dir)", gotDir)
	}
	want := "compose up -d --remove-orphans"
	if strings.Join(gotArgs, " ") != want {
		t.Fatalf("args = %q, want %q — reconciliation is delegated to compose verbatim", strings.Join(gotArgs, " "), want)
	}
}

// stateSequence fakes docker inspect: each call for an app pops its next state.
type stateSequence struct {
	states map[string][]string // app → sequence of "status\thealth" lines
}

func (f *stateSequence) run(args ...string) (string, error) {
	name := args[len(args)-1]
	seq := f.states[name]
	if len(seq) == 0 {
		return "", fmt.Errorf("no such container %s", name)
	}
	out := seq[0]
	if len(seq) > 1 { // hold the final state forever
		f.states[name] = seq[1:]
	}
	return out + "\n", nil
}

func TestWaitReadyPollsUntilRunning(t *testing.T) {
	fake := &stateSequence{states: map[string][]string{
		"sonarr":  {"created\t", "running\t"},
		"radarr":  {"running\thealthy"},
		"sabnzbd": {"running\tstarting", "running\thealthy"},
	}}
	d := NewWithRunner(fake.run)
	got := d.WaitReady([]string{"sonarr", "radarr", "sabnzbd"}, time.Second, time.Millisecond)
	for _, r := range got {
		if !r.Ready {
			t.Errorf("%s not ready: %s", r.App, r.Detail)
		}
	}
}

func TestWaitReadyReportsCrashesImmediately(t *testing.T) {
	fake := &stateSequence{states: map[string][]string{
		"sonarr": {"exited\t"},
	}}
	d := NewWithRunner(fake.run)
	start := time.Now()
	got := d.WaitReady([]string{"sonarr"}, 10*time.Second, time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a crashed container must not consume the timeout (took %s)", elapsed)
	}
	if got[0].Ready {
		t.Fatal("exited container reported ready")
	}
	for _, want := range []string{"sonarr is exited", "docker logs sonarr"} {
		if !strings.Contains(got[0].Detail, want) {
			t.Errorf("detail should contain %q: %s", want, got[0].Detail)
		}
	}
}

func TestWaitReadyTimesOutWithHonestState(t *testing.T) {
	fake := &stateSequence{states: map[string][]string{
		"sonarr": {"running\tstarting"}, // healthcheck never turns healthy
	}}
	d := NewWithRunner(fake.run)
	got := d.WaitReady([]string{"sonarr"}, 20*time.Millisecond, 5*time.Millisecond)
	if got[0].Ready {
		t.Fatal("never-healthy container reported ready")
	}
	if !strings.Contains(got[0].Detail, "running/starting") {
		t.Errorf("detail should name the observed state: %s", got[0].Detail)
	}
}

func TestWaitReadyHandlesMissingContainer(t *testing.T) {
	fake := &stateSequence{states: map[string][]string{}}
	d := NewWithRunner(fake.run)
	got := d.WaitReady([]string{"ghost"}, 20*time.Millisecond, 5*time.Millisecond)
	if got[0].Ready || !strings.Contains(got[0].Detail, "no such container") {
		t.Fatalf("missing container must be reported plainly: %+v", got[0])
	}
}
