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
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// FileBackend is a stub type. QuicBaaS operates as a BaaS client
// and does not host a local database engine.
type FileBackend struct{}

// NewFileBackend returns nil. QuicBaaS no longer hosts a local database engine.
func NewFileBackend(store *FileKVStore, nodeID uint64, log *logger.Logger) *FileBackend {
	return nil
}

// Close is a no-op stub.
func (f *FileBackend) Close() error {
	return nil
}
