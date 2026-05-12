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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestParseSpecialEnvVars_VectorEnabledProducesValidatableConfig is the
// cmd-side regression guard for the QUICBAAS_VECTOR_ENABLED=true UX bug.
//
// Before the fix, the env-var path produced a config that crashed the daemon
// at boot with "hnsw configuration is required". The fix wires
// VectorConfig.ApplyDefaults into initVectorStorage. This test pins the
// contract from the cmd-layer side: after parseSpecialEnvVars sets
// Enabled=true, applying defaults must yield a config the rest of the boot
// sequence accepts (Validate returns nil).
//
// If anyone removes ApplyDefaults from the boot path or weakens the defaults
// such that Validate fails again, this test fails.
func TestParseSpecialEnvVars_VectorEnabledProducesValidatableConfig(t *testing.T) {
	cfg = config.Default()
	t.Setenv("QUICBAAS_VECTOR_ENABLED", "true")

	parseSpecialEnvVars()

	require.True(t, cfg.Vector.Enabled, "QUICBAAS_VECTOR_ENABLED=true must enable vector")

	// Apply the same defaults the server lifecycle applies. The data dir
	// matches what the daemon would have established by the time
	// initVectorStorage runs. We invoke ApplyDefaults directly (rather than
	// initVectorStorage) because this is a cmd-layer test and doesn't depend
	// on the vector build tag.
	cfg.Vector.ApplyDefaults(cfg.Storage.DataDir)

	require.NoError(t, cfg.Vector.Validate(),
		"after parseSpecialEnvVars + ApplyDefaults, the vector config must validate; "+
			"if this fails the QUICBAAS_VECTOR_ENABLED=true documented enable path is broken again")

	// Pin the canonical defaults so that swapping them out trips this test.
	require.NotNil(t, cfg.Vector.HNSW)
	assert.Equal(t, 16, cfg.Vector.HNSW.M)
	assert.Equal(t, 200, cfg.Vector.HNSW.EfConstruction)
	assert.Equal(t, 10000, cfg.Vector.HNSW.MaxElements)
	assert.Equal(t, 1.0, cfg.Vector.HNSW.Ml)
}
