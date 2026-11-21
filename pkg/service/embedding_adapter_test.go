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

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

func TestNewEmbeddingManagerAdapter_KeepsManager(t *testing.T) {
	tests := []struct {
		name    string
		manager *embedding.Manager
	}{
		// A nil manager must not panic; the adapter keeps it as nil.
		{name: "NilManager", manager: nil},
		{name: "WithManager", manager: &embedding.Manager{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewEmbeddingManagerAdapter(tt.manager)
			require.NotNil(t, adapter)
			assert.Same(t, tt.manager, adapter.manager)
		})
	}
}
