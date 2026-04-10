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

package interfaces

import "testing"

func TestIndexType_String(t *testing.T) {
	tests := []struct {
		name     string
		idx      IndexType
		expected string
	}{
		{"flat", IndexTypeFlat, "flat"},
		{"hnsw", IndexTypeHNSW, "hnsw"},
		{"ivf_flat", IndexTypeIVFFlat, "ivf_flat"},
		{"ivf_pq", IndexTypeIVFPQ, "ivf_pq"},
		{"gpu_cagra", IndexTypeGPUCAGRA, "gpu_cagra"},
		{"gpu_ivf_flat", IndexTypeGPUIVFFlat, "gpu_ivf_flat"},
		{"gpu_ivf_pq", IndexTypeGPUIVFPQ, "gpu_ivf_pq"},
		{"gpu_brute_force", IndexTypeGPUBruteForce, "gpu_brute_force"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.idx.String()
			if result != tt.expected {
				t.Errorf("IndexType(%d).String() = %q, want %q", tt.idx, result, tt.expected)
			}
		})
	}
}

func TestIndexType_String_Unknown(t *testing.T) {
	unknown := IndexType(255)
	result := unknown.String()
	if result != "unknown" {
		t.Errorf("expected 'unknown' for invalid IndexType, got %q", result)
	}
}

func TestParseIndexType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected IndexType
	}{
		{"flat_lower", "flat", IndexTypeFlat},
		{"flat_upper", "FLAT", IndexTypeFlat},
		{"hnsw_lower", "hnsw", IndexTypeHNSW},
		{"hnsw_upper", "HNSW", IndexTypeHNSW},
		{"ivf_flat_lower", "ivf_flat", IndexTypeIVFFlat},
		{"ivf_flat_upper", "IVF_FLAT", IndexTypeIVFFlat},
		{"ivf_pq_lower", "ivf_pq", IndexTypeIVFPQ},
		{"ivf_pq_upper", "IVF_PQ", IndexTypeIVFPQ},
		{"gpu_cagra_lower", "gpu_cagra", IndexTypeGPUCAGRA},
		{"gpu_cagra_upper", "GPU_CAGRA", IndexTypeGPUCAGRA},
		{"gpu_ivf_flat_lower", "gpu_ivf_flat", IndexTypeGPUIVFFlat},
		{"gpu_ivf_flat_upper", "GPU_IVF_FLAT", IndexTypeGPUIVFFlat},
		{"gpu_ivf_pq_lower", "gpu_ivf_pq", IndexTypeGPUIVFPQ},
		{"gpu_ivf_pq_upper", "GPU_IVF_PQ", IndexTypeGPUIVFPQ},
		{"gpu_brute_force_lower", "gpu_brute_force", IndexTypeGPUBruteForce},
		{"gpu_brute_force_upper", "GPU_BRUTE_FORCE", IndexTypeGPUBruteForce},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseIndexType(tt.input)
			if result != tt.expected {
				t.Errorf("ParseIndexType(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseIndexType_UnknownDefaultsToHNSW(t *testing.T) {
	unknowns := []string{"", "invalid", "flatx", "HNSW_FLAT", "random"}
	for _, input := range unknowns {
		t.Run(input, func(t *testing.T) {
			result := ParseIndexType(input)
			if result != IndexTypeHNSW {
				t.Errorf("ParseIndexType(%q) = %d, want IndexTypeHNSW (%d)", input, result, IndexTypeHNSW)
			}
		})
	}
}

func TestIsGPUIndexType(t *testing.T) {
	gpuTypes := []IndexType{
		IndexTypeGPUCAGRA,
		IndexTypeGPUIVFFlat,
		IndexTypeGPUIVFPQ,
		IndexTypeGPUBruteForce,
	}
	for _, idx := range gpuTypes {
		if !IsGPUIndexType(idx) {
			t.Errorf("IsGPUIndexType(%d / %s) = false, want true", idx, idx.String())
		}
	}

	cpuTypes := []IndexType{
		IndexTypeFlat,
		IndexTypeHNSW,
		IndexTypeIVFFlat,
		IndexTypeIVFPQ,
	}
	for _, idx := range cpuTypes {
		if IsGPUIndexType(idx) {
			t.Errorf("IsGPUIndexType(%d / %s) = true, want false", idx, idx.String())
		}
	}
}

func TestDefaultVectorIndexConfig(t *testing.T) {
	config := DefaultVectorIndexConfig()

	if config == nil {
		t.Fatal("DefaultVectorIndexConfig() returned nil")
	}
	if config.IndexType != IndexTypeHNSW {
		t.Errorf("expected IndexType=IndexTypeHNSW, got %d", config.IndexType)
	}
	if config.DistanceMetric != "l2" {
		t.Errorf("expected DistanceMetric=l2, got %s", config.DistanceMetric)
	}
	if config.M != 16 {
		t.Errorf("expected M=16, got %d", config.M)
	}
	if config.MMax != 32 {
		t.Errorf("expected MMax=32, got %d", config.MMax)
	}
	if config.EfConstruction != 200 {
		t.Errorf("expected EfConstruction=200, got %d", config.EfConstruction)
	}
	if config.EfSearch != 50 {
		t.Errorf("expected EfSearch=50, got %d", config.EfSearch)
	}
	if config.MaxElements != 1000000 {
		t.Errorf("expected MaxElements=1000000, got %d", config.MaxElements)
	}
}

func TestDefaultVectorIndexConfig_ReturnsDistinctInstances(t *testing.T) {
	a := DefaultVectorIndexConfig()
	b := DefaultVectorIndexConfig()

	if a == b {
		t.Error("DefaultVectorIndexConfig() should return distinct instances on each call")
	}

	a.M = 999
	if b.M == 999 {
		t.Error("mutating one config should not affect the other")
	}
}
