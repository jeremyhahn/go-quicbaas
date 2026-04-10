//go:build rag

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

package query

// This file contains streaming-related functionality.
// The main streaming implementation is in handler.go StreamQuery method.

// StreamConfig configures streaming behavior.
type StreamConfig struct {
	// BufferSize is the channel buffer size (default: 10).
	BufferSize int

	// FlushInterval is how often to flush partial results.
	FlushInterval int
}

// DefaultStreamConfig returns default streaming configuration.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		BufferSize:    10,
		FlushInterval: 1,
	}
}
