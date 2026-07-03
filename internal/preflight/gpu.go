package preflight

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Haroutio/arrsenal/internal/state"
)

// GPUDetection is what the probes concluded. Detection proposes; the TUI
// shows it and lets the user override to none (DESIGN.md §8).
type GPUDetection struct {
	Proposal state.GPUMode
	Detail   string // human explanation of what was found and what happens next

	// NVIDIA specifics: the driver is the user's job (never installed by
	// Arrsenal — permanent line); the container toolkit is ours to offer.
	NvidiaDriverOK  bool
	NvidiaToolkitOK bool
	// ToolkitInstallHint is set when the driver works but the container
	// toolkit is missing — the cmd layer turns it into an inform-then-prompt.
	ToolkitInstallHint string
}

// GPUProbes injects the hardware questions for testability.
type GPUProbes struct {
	// NvidiaSmi returns nil when the NVIDIA driver is installed and working.
	NvidiaSmi func() error
	// NvidiaToolkitPresent reports whether docker knows the nvidia runtime.
	NvidiaToolkitPresent func() bool
	// DRIVendors returns the PCI vendor IDs of /dev/dri render devices
	// (e.g. "0x8086" Intel, "0x1002" AMD). Empty when /dev/dri is absent.
	DRIVendors func() []string
}

// DefaultGPUProbes wires the real probes.
func DefaultGPUProbes() GPUProbes {
	return GPUProbes{
		NvidiaSmi: func() error {
			return exec.Command("nvidia-smi", "-L").Run()
		},
		NvidiaToolkitPresent: func() bool {
			out, err := exec.Command("docker", "info", "--format", "{{json .Runtimes}}").Output()
			return err == nil && strings.Contains(string(out), "nvidia")
		},
		DRIVendors: driVendors,
	}
}

// DetectGPU runs the three lanes in preference order: NVIDIA (discrete,
// NVENC), then Intel (QSV), then AMD (VAAPI). A working NVIDIA driver wins
// even when an iGPU is also present — NVENC is why people pass the card in.
func DetectGPU(p GPUProbes) GPUDetection {
	if p.NvidiaSmi() == nil {
		d := GPUDetection{
			Proposal:        state.GPUNvidia,
			NvidiaDriverOK:  true,
			NvidiaToolkitOK: p.NvidiaToolkitPresent(),
		}
		if d.NvidiaToolkitOK {
			d.Detail = "NVIDIA GPU with working driver and container toolkit — hardware transcoding (NVENC) will be configured end-to-end."
		} else {
			d.Detail = "NVIDIA GPU with working driver, but the nvidia-container-toolkit is missing — containers cannot reach the GPU until it is installed."
			d.ToolkitInstallHint = "Arrsenal can install nvidia-container-toolkit (a repo + package + docker runtime config — no kernel drivers)."
		}
		return d
	}

	vendors := p.DRIVendors()
	for _, v := range vendors {
		if v == "0x8086" {
			return GPUDetection{Proposal: state.GPUIntel,
				Detail: "Intel GPU at /dev/dri — QuickSync hardware transcoding will be configured end-to-end."}
		}
	}
	for _, v := range vendors {
		if v == "0x1002" {
			return GPUDetection{Proposal: state.GPUAMD,
				Detail: "AMD GPU at /dev/dri — VAAPI hardware transcoding will be configured end-to-end."}
		}
	}

	return GPUDetection{Proposal: state.GPUNone,
		Detail: "No usable GPU detected — transcoding will use the CPU. If this machine has an NVIDIA card, " +
			"install its driver first (Arrsenal never installs kernel drivers) and re-run; " +
			"Intel/AMD GPUs are detected via /dev/dri automatically."}
}

// driVendors reads the render devices' PCI vendor IDs from sysfs.
func driVendors() []string {
	cards, _ := filepath.Glob("/sys/class/drm/card*/device/vendor")
	var out []string
	for _, c := range cards {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		out = append(out, strings.TrimSpace(string(b)))
	}
	return out
}

// NvidiaToolkitInstallPlan is the Tier-1 (Debian/Ubuntu) install sequence the
// cmd layer offers when the driver works but the toolkit is missing. It is a
// plan, not an action: every step is shown before anything runs
// (inform-then-prompt, DESIGN.md §10).
func NvidiaToolkitInstallPlan() []string {
	return []string{
		"curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg",
		`curl -sL https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' > /etc/apt/sources.list.d/nvidia-container-toolkit.list`,
		"apt-get update",
		"apt-get install -y nvidia-container-toolkit",
		"nvidia-ctk runtime configure --runtime=docker",
		"systemctl restart docker",
	}
}

// FormatToolkitPlan renders the plan as a numbered list for display.
func FormatToolkitPlan() string {
	var b strings.Builder
	for i, step := range NvidiaToolkitInstallPlan() {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
	}
	return b.String()
}
