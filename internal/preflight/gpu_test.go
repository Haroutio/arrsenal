package preflight

import (
	"errors"
	"strings"
	"testing"

	"github.com/Haroutio/arrsenal/internal/state"
)

func gpuProbes(nvidiaOK, toolkitOK bool, vendors ...string) GPUProbes {
	return GPUProbes{
		NvidiaSmi: func() error {
			if nvidiaOK {
				return nil
			}
			return errors.New("nvidia-smi: not found")
		},
		NvidiaToolkitPresent: func() bool { return toolkitOK },
		DRIVendors:           func() []string { return vendors },
		PCIDisplayVendors:    func() []string { return nil },
	}
}

// withPCI overlays the PCI-bus view (what EXISTS, driver or not).
func withPCI(p GPUProbes, vendors ...string) GPUProbes {
	p.PCIDisplayVendors = func() []string { return vendors }
	return p
}

func TestDetectGPULanes(t *testing.T) {
	cases := map[string]struct {
		probes GPUProbes
		want   state.GPUMode
	}{
		"nvidia full":              {gpuProbes(true, true), state.GPUNvidia},
		"nvidia sans toolkit":      {gpuProbes(true, false), state.GPUNvidia},
		"nvidia wins over igpu":    {gpuProbes(true, true, "0x8086"), state.GPUNvidia},
		"intel":                    {gpuProbes(false, false, "0x8086"), state.GPUIntel},
		"amd":                      {gpuProbes(false, false, "0x1002"), state.GPUAMD},
		"intel preferred over amd": {gpuProbes(false, false, "0x1002", "0x8086"), state.GPUIntel},
		"unknown vendor only":      {gpuProbes(false, false, "0x10de"), state.GPUNone},
		"nothing":                  {gpuProbes(false, false), state.GPUNone},
	}
	for name, tc := range cases {
		if got := DetectGPU(tc.probes); got.Proposal != tc.want {
			t.Errorf("%s: proposal = %s, want %s", name, got.Proposal, tc.want)
		}
	}
}

func TestNvidiaWithoutToolkitProposesInstall(t *testing.T) {
	got := DetectGPU(gpuProbes(true, false))
	if !got.NvidiaDriverOK || got.NvidiaToolkitOK {
		t.Fatalf("flags wrong: %+v", got)
	}
	if got.ToolkitInstallHint == "" || !strings.Contains(got.ToolkitInstallHint, "no kernel drivers") {
		t.Fatalf("hint must offer the toolkit and reaffirm the no-kernel-drivers line: %q", got.ToolkitInstallHint)
	}
}

func TestNoGPUAnywhereSaysSo(t *testing.T) {
	got := DetectGPU(gpuProbes(false, false))
	for _, want := range []string{"CPU", "No GPU detected"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail should mention %q: %s", want, got.Detail)
		}
	}
}

func TestDeadNvidiaDriverIsDiagnosedFromThePCIBus(t *testing.T) {
	// The mediasrv case: RTX passed through (0x10de on the bus, QEMU's
	// 0x1234 display too), nvidia-smi dead, no render nodes.
	probes := withPCI(gpuProbes(false, false), "0x1234", "0x10de")
	got := DetectGPU(probes)
	if got.Proposal != state.GPUNone {
		t.Fatalf("dead driver must still propose none, got %s", got.Proposal)
	}
	for _, want := range []string{"NVIDIA GPU is visible on the PCI bus", "driver is not working", "kernel update", "never installs kernel drivers"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail should mention %q: %s", want, got.Detail)
		}
	}
}

func TestInertIntelOnBusIsDiagnosed(t *testing.T) {
	probes := withPCI(gpuProbes(false, false), "0x8086")
	got := DetectGPU(probes)
	if got.Proposal != state.GPUNone || !strings.Contains(got.Detail, "no /dev/dri render device") {
		t.Fatalf("driverless Intel GPU should be named: %+v", got)
	}
}

func TestWorkingDriversStillWinOverPCIDiagnosis(t *testing.T) {
	// A live NVIDIA driver with the card obviously also on the bus.
	probes := withPCI(gpuProbes(true, true), "0x10de")
	if got := DetectGPU(probes); got.Proposal != state.GPUNvidia {
		t.Fatalf("working nvidia must be chosen, got %+v", got)
	}
	// QEMU's virtual display (0x1234) alone must not trigger GPU messaging.
	probes = withPCI(gpuProbes(false, false), "0x1234")
	if got := DetectGPU(probes); !strings.Contains(got.Detail, "No GPU detected") {
		t.Fatalf("a virtual display adapter is not a GPU: %s", got.Detail)
	}
}

func TestToolkitPlanIsUserRunnable(t *testing.T) {
	plan := NvidiaToolkitInstallPlan()
	if len(plan) == 0 {
		t.Fatal("empty plan")
	}
	joined := strings.Join(plan, "\n")
	for _, forbidden := range []string{"nvidia-driver", "nouveau", "dkms"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("the plan must never touch kernel drivers, found %q", forbidden)
		}
	}
	if !strings.Contains(FormatToolkitPlan(), "1. ") {
		t.Fatal("formatted plan should be a numbered list")
	}
}
