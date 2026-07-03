package preflight

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/Haroutio/arrsenal/internal/registry"
	"github.com/Haroutio/arrsenal/internal/state"
)

// ComposeProject is the compose project name Arrsenal generates; containers
// carrying this label are ours (kept in sync with generate's compose `name`).
const ComposeProject = "arrsenal"

// ConflictKind classifies scan findings.
type ConflictKind string

// Conflict kinds. Ports and container names must be resolved before `up`;
// existing appdata is a notice — the new container adopts it (DESIGN.md §4).
const (
	KindPort          ConflictKind = "port"
	KindContainerName ConflictKind = "container-name"
	KindAppdata       ConflictKind = "appdata-exists"
)

// Conflict is one finding, aimed at the TUI: every field it needs to offer
// a remap or a deselection, and human text for everything else.
type Conflict struct {
	App      string
	Kind     ConflictKind
	Port     int    // set for KindPort: the busy HOST port
	Protocol string // set for KindPort
	// ContainerPort is the registry-side key for a remap (state.PortRemaps
	// is keyed by container port — see state.WebPorts for why).
	ContainerPort int
	Detail        string
}

// Blocking reports whether the finding must be resolved before bring-up.
func (c Conflict) Blocking() bool { return c.Kind != KindAppdata }

// ScanDeps injects the environment probes so the scan logic is testable
// without a docker daemon or bound sockets.
type ScanDeps struct {
	// Containers: name → compose project label (dockerx.Docker.Containers).
	Containers func() (map[string]string, error)
	// PortFree reports whether a host port is bindable right now.
	PortFree func(port int, protocol string) bool
	// AppdataNonEmpty reports whether an app's config dir has content.
	AppdataNonEmpty func(dir string) bool
}

// DefaultDeps wires the real probes. containers comes from a dockerx.Docker
// so the caller controls CLI construction.
func DefaultDeps(containers func() (map[string]string, error)) ScanDeps {
	return ScanDeps{
		Containers:      containers,
		PortFree:        portFree,
		AppdataNonEmpty: appdataNonEmpty,
	}
}

// ScanConflicts checks every SELECTED app — and nothing else, that's the
// coexistence promise — for host port collisions, container-name collisions,
// and existing appdata. Containers that belong to Arrsenal's own compose
// project are ours from a previous run: not a name conflict, and their bound
// ports are their own, so port checks skip those apps.
func ScanConflicts(s *state.State, deps ScanDeps) ([]Conflict, error) {
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to scan for invalid state: %w", err)
	}
	containers, err := deps.Containers()
	if err != nil {
		return nil, fmt.Errorf("listing containers (is docker running?): %w", err)
	}

	var out []Conflict
	for _, id := range s.Apps {
		app, _ := registry.ByID(id)

		ours := false
		if project, exists := containers[id]; exists {
			if project == ComposeProject {
				ours = true // previous arrsenal run; compose reconciles it
			} else {
				detail := fmt.Sprintf("a container named %q already exists", id)
				if project != "" {
					detail += fmt.Sprintf(" (compose project %q)", project)
				}
				out = append(out, Conflict{App: id, Kind: KindContainerName,
					Detail: detail + " — deselect the app here, or remove/rename that container"})
			}
		}

		// Our own running container legitimately binds its ports.
		if !ours && !s.HostNetworked(id) {
			webHost, _ := s.WebPorts(app)
			ports := []registry.PortMap{{Container: app.Web.Container, Host: webHost, Protocol: app.Web.Protocol, Purpose: app.Web.Purpose}}
			for _, p := range app.ExtraPorts {
				ports = append(ports, registry.PortMap{Container: p.Container, Host: s.HostPort(app, p), Protocol: p.Protocol, Purpose: p.Purpose})
			}
			for _, p := range ports {
				if !deps.PortFree(p.Host, p.Protocol) {
					out = append(out, Conflict{App: id, Kind: KindPort, Port: p.Host, Protocol: p.Protocol,
						ContainerPort: p.Container,
						Detail: fmt.Sprintf("host port %d/%s (%s) is already in use — pick another port for %s",
							p.Host, p.Protocol, p.Purpose, app.Name)})
				}
			}
		}

		if deps.AppdataNonEmpty(filepath.Join(s.AppdataRoot, id)) {
			out = append(out, Conflict{App: id, Kind: KindAppdata,
				Detail: fmt.Sprintf("existing config found in %s — the new container will adopt it (settings and history carry over)",
					filepath.Join(s.AppdataRoot, id))})
		}
	}
	return out, nil
}

// portFree probes by binding: the only honest answer to "can compose publish
// this port" is to try. Binds on the wildcard address and releases instantly.
func portFree(port int, protocol string) bool {
	addr := fmt.Sprintf(":%d", port)
	switch protocol {
	case "udp":
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			return false
		}
		_ = pc.Close()
	default:
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return false
		}
		_ = l.Close()
	}
	return true
}

func appdataNonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
