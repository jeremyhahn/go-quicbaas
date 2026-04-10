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

// Package proto provides cluster management client extensions.
package proto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

// TestNewQuicBaaSClientExt verifies the client extension wrapper.
func TestNewQuicBaaSClientExt(t *testing.T) {
	// Create a mock connection (nil is acceptable for this test)
	var conn grpc.ClientConnInterface

	// Test that the function returns a QuicBaaSClientExt
	client := NewQuicBaaSClientExt(conn)

	// Verify it's not nil and implements the interface
	assert.NotNil(t, client)

	// Verify the returned type implements QuicBaaSClient interface
	var _ QuicBaaSClient = client
}
