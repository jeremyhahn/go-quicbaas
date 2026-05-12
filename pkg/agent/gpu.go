// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package agent

import (
	"os"
	"os/exec"
	"runtime"
)

// gpuProbe is the probe function used by DetectGPU. It is a package-level
// variable so tests can inject a mock without touching the real filesystem.
var gpuProbe gpuDetector = &osGPUDetector{}

// gpuDetector abstracts OS-level GPU presence checks so the detection logic
// can be unit-tested without a real GPU or CUDA installation.
type gpuDetector interface {
	// DeviceExists reports whether /dev/nvidia0 is present.
	DeviceExists() bool
	// NvidiaSMIAvailable reports whether nvidia-smi is on PATH.
	NvidiaSMIAvailable() bool
	// CUDAEnvSet reports whether the CUDA_VISIBLE_DEVICES env var is set
	// and non-empty (a reliable signal that a CUDA runtime is configured).
	CUDAEnvSet() bool
	// IsAppleSilicon reports true on darwin/arm64 (Apple Metal GPU).
	IsAppleSilicon() bool
}

// osGPUDetector is the production implementation of gpuDetector.
type osGPUDetector struct{}

func (o *osGPUDetector) DeviceExists() bool {
	_, err := os.Stat("/dev/nvidia0")
	return err == nil
}

func (o *osGPUDetector) NvidiaSMIAvailable() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

func (o *osGPUDetector) CUDAEnvSet() bool {
	return os.Getenv("CUDA_VISIBLE_DEVICES") != ""
}

func (o *osGPUDetector) IsAppleSilicon() bool {
	return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
}

// DetectGPU performs a cheap startup probe to determine whether this host
// has a usable GPU. It returns true when any of the following conditions hold:
//
//   - /dev/nvidia0 exists (NVIDIA kernel driver loaded)
//   - nvidia-smi is on PATH (NVIDIA management library present)
//   - CUDA_VISIBLE_DEVICES env var is set (CUDA runtime configured)
//   - Running on darwin/arm64 (Apple Silicon with integrated Metal GPU)
//
// Pure-CPU agents return false. The result should be computed once at daemon
// startup and cached; each probe is cheap but involves a filesystem stat and
// PATH lookup.
func DetectGPU() bool {
	d := gpuProbe
	return d.DeviceExists() || d.NvidiaSMIAvailable() || d.CUDAEnvSet() || d.IsAppleSilicon()
}
