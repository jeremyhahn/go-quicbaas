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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestClientConnect_AlreadyConnected(t *testing.T) {
	// Create a client that simulates already being connected
	client := &Client{
		address:   "localhost:50051",
		opts:      defaultClientOptions(),
		connected: true,
	}

	err := client.Connect(context.Background())
	assert.NoError(t, err)
}

func TestClientConnect_Closed(t *testing.T) {
	client := &Client{
		address:   "localhost:50051",
		opts:      defaultClientOptions(),
		closed:    true,
		connected: false,
	}

	err := client.Connect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestClientDisconnect_WithPool(t *testing.T) {
	// Create a client with a pool
	client := &Client{
		address: "localhost:50051",
		opts:    defaultClientOptions(),
		pool:    &ConnectionPool{}, // Mock pool
	}

	err := client.Disconnect()
	assert.NoError(t, err) // Should be a no-op
}

func TestClientDisconnect_NotConnected(t *testing.T) {
	client := &Client{
		address:   "localhost:50051",
		opts:      defaultClientOptions(),
		connected: false,
	}

	err := client.Disconnect()
	assert.NoError(t, err) // Should be a no-op
}

func TestClientClose_AlreadyClosed(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	client := &Client{
		address: "localhost:50051",
		opts:    defaultClientOptions(),
		logger:  log,
		closed:  true,
	}

	err = client.Close()
	assert.NoError(t, err)
}

func TestClientClose_NotConnected(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	client := &Client{
		address:   "localhost:50051",
		opts:      defaultClientOptions(),
		logger:    log,
		connected: false,
		closed:    false,
	}

	err = client.Close()
	assert.NoError(t, err)
	assert.True(t, client.closed)
}

func TestClientIsConnected(t *testing.T) {
	t.Run("connected", func(t *testing.T) {
		client := &Client{
			connected: true,
		}
		assert.True(t, client.isConnected())
	})

	t.Run("not connected", func(t *testing.T) {
		client := &Client{
			connected: false,
		}
		assert.False(t, client.isConnected())
	})
}

func TestClientAddMetadata(t *testing.T) {
	t.Run("with metadata", func(t *testing.T) {
		opts := defaultClientOptions()
		opts.metadata = map[string]string{
			"key1": "value1",
			"key2": "value2",
		}

		client := &Client{
			opts: opts,
		}

		ctx := client.addMetadata(context.Background())
		assert.NotNil(t, ctx)
	})

	t.Run("without metadata", func(t *testing.T) {
		client := &Client{
			opts: defaultClientOptions(),
		}

		ctx := client.addMetadata(context.Background())
		assert.NotNil(t, ctx)
	})
}
