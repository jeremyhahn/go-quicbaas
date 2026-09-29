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

package fleet

import "testing"

// =============================================================================
// stubDetector — test double for gpuDetector
// =============================================================================

// stubDetector lets each test control exactly which probe signals are active
// without touching the filesystem or requiring any GPU hardware.
type stubDetector struct {
	device    bool
	nvidiaSMI bool
	cudaEnv   bool
	appleSili bool
}

func (s *stubDetector) DeviceExists() bool       { return s.device }
func (s *stubDetector) NvidiaSMIAvailable() bool { return s.nvidiaSMI }
func (s *stubDetector) CUDAEnvSet() bool         { return s.cudaEnv }
func (s *stubDetector) IsAppleSilicon() bool     { return s.appleSili }

// withProbe swaps in a stub detector for the duration of the test and
// restores the original when the test ends.
func withProbe(t *testing.T, d gpuDetector) {
	t.Helper()
	orig := gpuProbe
	gpuProbe = d
	t.Cleanup(func() { gpuProbe = orig })
}

// =============================================================================
// TestDetectGPU — covers each detection signal independently
// =============================================================================
//
// These tests share the package-level gpuProbe global via withProbe(), so they
// must NOT run in parallel — t.Parallel() would race the swap and let one
// test observe another test's stub. Run sequentially instead.

// TestDetectGPU_NoCudaReturnsFalse verifies that a pure-CPU agent with no
// GPU indicators returns false so it is never flagged as GPU-capable.
func TestDetectGPU_NoCudaReturnsFalse(t *testing.T) {
	withProbe(t, &stubDetector{}) // all false
	if DetectGPU() {
		t.Error("expected DetectGPU to return false when no GPU signals are present")
	}
}

// TestDetectGPU_NvidiaDeviceReturnsTrue verifies detection when /dev/nvidia0
// is present (NVIDIA kernel driver loaded).
func TestDetectGPU_NvidiaDeviceReturnsTrue(t *testing.T) {
	withProbe(t, &stubDetector{device: true})
	if !DetectGPU() {
		t.Error("expected DetectGPU to return true when /dev/nvidia0 exists")
	}
}

// TestDetectGPU_NvidiaSMIReturnsTrue verifies detection when nvidia-smi is
// found on PATH.
func TestDetectGPU_NvidiaSMIReturnsTrue(t *testing.T) {
	withProbe(t, &stubDetector{nvidiaSMI: true})
	if !DetectGPU() {
		t.Error("expected DetectGPU to return true when nvidia-smi is on PATH")
	}
}

// TestDetectGPU_CUDAEnvReturnsTrue verifies detection when CUDA_VISIBLE_DEVICES
// is set (CUDA runtime configured).
func TestDetectGPU_CUDAEnvReturnsTrue(t *testing.T) {
	withProbe(t, &stubDetector{cudaEnv: true})
	if !DetectGPU() {
		t.Error("expected DetectGPU to return true when CUDA_VISIBLE_DEVICES is set")
	}
}

// TestDetectGPU_AppleSiliconReturnsTrue verifies detection on darwin/arm64
// (Apple Metal integrated GPU).
func TestDetectGPU_AppleSiliconReturnsTrue(t *testing.T) {
	withProbe(t, &stubDetector{appleSili: true})
	if !DetectGPU() {
		t.Error("expected DetectGPU to return true on Apple Silicon")
	}
}
