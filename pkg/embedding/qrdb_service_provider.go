//go:build vector

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
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
)

// QRDBServiceProvider is implemented by a provider that wraps a native
// go-qrdb embedding provider. QRDBServiceProvider returns that native
// provider as go-qrdb's service-layer EmbeddingProvider, so go-qrdb's
// re-exported embedding service serves it unfiltered: the model a request
// names, the models the upstream lists, and the upstream's token usage.
// The returned provider's Close is a no-op; the embedding manager owns the
// wrapped provider's lifetime.
type QRDBServiceProvider interface {
	QRDBServiceProvider() qrdbservice.EmbeddingProvider
}
