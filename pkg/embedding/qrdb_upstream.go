//go:build vector && qrdb_vector

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

import qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

// QRDBUpstream is a provider that wraps a go-qrdb embedding provider. It
// exposes that provider so a caller that speaks go-qrdb's shapes can reach
// what the single-text wrapper drops: the token usage of a request and the
// models the provider lists.
type QRDBUpstream interface {
	// QRDBProvider returns the wrapped go-qrdb embedding provider.
	QRDBProvider() qrdbsdk.EmbeddingProvider
}

var (
	_ QRDBUpstream = (*OpenAIProvider)(nil)
	_ QRDBUpstream = (*OllamaProvider)(nil)
	_ QRDBUpstream = (*LMStudioProvider)(nil)
	_ QRDBUpstream = (*VoyageProvider)(nil)
)
