//go:build !vector

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

package embedding

import (
	"context"
)

// EmbeddingManagerInterface defines the interface that Manager implements.
// This interface is defined locally to avoid circular imports with pkg/interfaces.
// The pkg/interfaces.EmbeddingManager interface mirrors this definition.
type EmbeddingManagerInterface interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() (int, error)
	GetProvider(name string) (Provider, error)
	GetStats() Stats
}

// Compile-time check that Manager implements EmbeddingManagerInterface.
var _ EmbeddingManagerInterface = (*Manager)(nil)
