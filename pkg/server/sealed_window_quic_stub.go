//go:build !quic

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// startSealedQUICServer is a no-op when the quic build tag is not set: the
// transport is not compiled, so there is no sealed-phase listener to serve.
func (s *Server) startSealedQUICServer() error { return nil }
