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
)

func TestEmbeddedServer_ShutdownNotStarted_Coverage(t *testing.T) {
	es := &embeddedServer{}

	// shutdown when started is false should return nil
	err := es.shutdown()
	assert.NoError(t, err)
}

// TestShutdownEmbeddedServer_Coverage tests global shutdown function.
func TestShutdownEmbeddedServer_Coverage(t *testing.T) {
	// Ensure no server exists
	embeddedServerHolder.Store(nil)

	// Calling shutdown when no server exists should be fine
	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

// TestGetFreePortForEmbedded_Coverage tests port allocation.
func TestGetFreePortForEmbedded_Coverage(t *testing.T) {
	port, err := getFreePortForEmbedded()
	assert.NoError(t, err)
	assert.Greater(t, port, 1024) // Should be an ephemeral port
	assert.Less(t, port, 65536)
}
