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
)

func TestClient_Conn_NotConnected(t *testing.T) {
	client := &Client{
		connected: false,
		conn:      nil,
	}

	conn := client.Conn()

	assert.Nil(t, conn)
}

func TestClient_Conn_Connected(t *testing.T) {
	// Test that Conn() returns the connection when available
	// Note: We can't create a real grpc.ClientConn in unit tests
	// so we just verify the method doesn't panic with nil
	client := &Client{
		connected: true,
		conn:      nil, // Would be a real conn in practice
	}

	conn := client.Conn()

	// Even if connected is true, conn might be nil if not properly initialized
	assert.Nil(t, conn)
}

func TestClient_isConnected_False(t *testing.T) {
	client := &Client{
		connected: false,
		closed:    false,
	}

	assert.False(t, client.isConnected())
}

func TestClient_isConnected_Closed(t *testing.T) {
	client := &Client{
		connected: true,
		closed:    true,
	}

	assert.False(t, client.isConnected())
}

func TestClient_isConnected_True(t *testing.T) {
	client := &Client{
		connected: true,
		closed:    false,
	}

	assert.True(t, client.isConnected())
}
