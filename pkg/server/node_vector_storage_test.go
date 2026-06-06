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

//go:build vector

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewNodeVectorStorage_NilClient verifies that newNodeVectorStorage
// accepts a nil client without panicking and returns a non-nil storage.
// The storage itself will return errors on operations, but construction
// must not panic.
func TestNewNodeVectorStorage_NilClient(t *testing.T) {
	var storage = newNodeVectorStorage(nil)
	require.NotNil(t, storage)
}

// TestNodeVectorStorage_Close verifies that Close is a no-op.
func TestNodeVectorStorage_Close(t *testing.T) {
	storage := newNodeVectorStorage(nil)
	assert.NoError(t, storage.Close())
}

// TestNodeVectorDataToInterface_NilSlice verifies that a nil slice is
// handled gracefully and returns an empty (non-nil) slice.
func TestNodeVectorDataToInterface_NilSlice(t *testing.T) {
	result := nodeVectorDataToInterface(nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

// TestNodeNamespaceInfoToInterface_Nil verifies that a nil NamespaceInfo
// returns a zero-valued VectorNamespaceInfo.
func TestNodeNamespaceInfoToInterface_Nil(t *testing.T) {
	result := nodeNamespaceInfoToInterface(nil)
	assert.Empty(t, result.Name)
}
