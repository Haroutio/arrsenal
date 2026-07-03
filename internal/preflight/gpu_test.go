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
	}
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

func TestNoGPUExplainsTheDriverLine(t *testing.T) {
	got := DetectGPU(gpuProbes(false, false))
	for _, want := range []string{"CPU", "never installs kernel drivers"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("detail should mention %q: %s", want, got.Detail)
		}
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
