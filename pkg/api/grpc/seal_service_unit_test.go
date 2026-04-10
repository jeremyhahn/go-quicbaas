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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestSealServiceClose(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize the manager
	err = InitializeDefaultSealManager(log)
	require.NoError(t, err)
	defer func() { globalSealManager = nil }()

	svc := NewSealService(globalSealManager, log)
	assert.NotNil(t, svc)

	err = svc.Close()
	assert.NoError(t, err)

	// Cleanup
	globalSealManager = nil
}
