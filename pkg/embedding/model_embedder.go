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

import "context"

// ModelEmbedder is a provider that can embed with a model named per call. It
// returns the vectors and the model that produced them.
type ModelEmbedder interface {
	// EmbedBatchWithModel embeds texts with model, or with the provider's
	// configured model when model is empty.
	EmbedBatchWithModel(ctx context.Context, texts []string, model string) ([][]float32, string, error)
}
