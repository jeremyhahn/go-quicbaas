//go:build vector && !gpu

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

package server

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/vector/gpu"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestNewGPUIndexFactory(t *testing.T) {
	factory := NewGPUIndexFactory()
	if factory == nil {
		t.Fatal("NewGPUIndexFactory returned nil")
	}
}

func TestGPUIndexFactory_CreateIndex_ReturnsStubError(t *testing.T) {
	factory := NewGPUIndexFactory()

	config := &interfaces.VectorIndexConfig{
		IndexType:      interfaces.IndexTypeGPUCAGRA,
		Dimensions:     128,
		DistanceMetric: "l2",
	}

	idx, err := factory.CreateIndex(config)
	if idx != nil {
		t.Error("expected nil VectorIndex from stub factory")
	}
	if !errors.Is(err, gpu.ErrGPUNotCompiled) {
		t.Errorf("expected ErrGPUNotCompiled, got: %v", err)
	}
}
