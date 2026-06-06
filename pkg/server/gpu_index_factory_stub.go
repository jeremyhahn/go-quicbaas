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
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// gpuIndexFactory is the stub factory when GPU support is not compiled.
type gpuIndexFactory struct{}

// NewGPUIndexFactory returns a factory that returns errors for all GPU index creation.
func NewGPUIndexFactory() *gpuIndexFactory {
	return &gpuIndexFactory{}
}

// CreateIndex always returns ErrGPUNotCompiled when GPU is not compiled in.
func (f *gpuIndexFactory) CreateIndex(_ *interfaces.VectorIndexConfig) (interfaces.VectorIndex, error) {
	return nil, qrdbsdk.ErrGPUNotCompiled
}
