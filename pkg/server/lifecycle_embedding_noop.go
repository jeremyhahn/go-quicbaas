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

//go:build !vector

package server

import (
	"errors"
)

// ErrEmbeddingNotCompiled is returned when embedding operations are attempted
// but the binary was not compiled with the vector build tag.
var ErrEmbeddingNotCompiled = errors.New("embedding support not compiled - build with -tags vector")

// startEmbedding is a no-op when vector support is not compiled.
func (s *Server) startEmbedding() error {
	s.logger.Debug("Embedding not compiled, skipping initialization")
	return nil
}

// stopEmbedding is a no-op when vector support is not compiled.
func (s *Server) stopEmbedding() error {
	return nil
}
