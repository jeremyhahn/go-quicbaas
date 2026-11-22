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

//go:build !(vector && qrdb_vector)

package server

import qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

// qrdbRAGServerOptions returns nil when vector support or go-qrdb's RAG
// pipeline is not compiled, so the node serves no go-qrdb RAG routes.
func (s *Server) qrdbRAGServerOptions() []qrdbsdk.ServerOption {
	return nil
}
