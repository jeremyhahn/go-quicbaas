//go:build quic && !rag

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

package quic

import (
	"context"
)

// handleRAGQuery is a stub when RAG is not enabled.
func (h *Handler) handleRAGQuery(ctx context.Context, req *Request) *Response {
	return &Response{
		Status:  StatusNotFound,
		Payload: []byte("RAG not enabled - build with -tags rag"),
	}
}

// handleRAGBuildContext is a stub when RAG is not enabled.
func (h *Handler) handleRAGBuildContext(ctx context.Context, req *Request) *Response {
	return &Response{
		Status:  StatusNotFound,
		Payload: []byte("RAG not enabled - build with -tags rag"),
	}
}
