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

package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMockWatchStream verifies mock stream creation.
func TestNewMockWatchStream(t *testing.T) {
	stream := newMockWatchStream()
	require.NotNil(t, stream)
	assert.NotNil(t, stream.ctx)
	assert.NotNil(t, stream.responses)
	assert.Len(t, stream.responses, 0)
}

// TestMockWatchStream_Send verifies sending responses to mock stream.
func TestMockWatchStream_Send(t *testing.T) {
	stream := newMockWatchStream()

	resp1 := &WatchResponse{
		WatchID:  1,
		Revision: 10,
		Events:   []*WatchEvent{},
	}

	err := stream.Send(resp1)
	assert.NoError(t, err)

	responses := stream.getResponses()
	require.Len(t, responses, 1)
	assert.Equal(t, resp1, responses[0])
}

// TestMockWatchStream_SendMultiple verifies multiple responses.
func TestMockWatchStream_SendMultiple(t *testing.T) {
	stream := newMockWatchStream()

	// Send multiple responses
	for i := 0; i < 5; i++ {
		resp := &WatchResponse{
			WatchID:  int64(i),
			Revision: int64(i * 10),
		}
		err := stream.Send(resp)
		assert.NoError(t, err)
	}

	responses := stream.getResponses()
	require.Len(t, responses, 5)

	// Verify order preserved
	for i := 0; i < 5; i++ {
		assert.Equal(t, int64(i), responses[i].WatchID)
		assert.Equal(t, int64(i*10), responses[i].Revision)
	}
}

// TestMockWatchStream_Context verifies context retrieval.
func TestMockWatchStream_Context(t *testing.T) {
	stream := newMockWatchStream()

	ctx := stream.Context()
	require.NotNil(t, ctx)
	assert.Equal(t, context.Background(), ctx)
}

// TestMockWatchStream_GetResponsesCopy verifies getResponses returns a copy.
func TestMockWatchStream_GetResponsesCopy(t *testing.T) {
	stream := newMockWatchStream()

	resp1 := &WatchResponse{WatchID: 1}
	stream.Send(resp1)

	// Get responses
	responses1 := stream.getResponses()
	require.Len(t, responses1, 1)

	// Modify returned slice
	responses1[0] = &WatchResponse{WatchID: 999}

	// Get responses again - should still be original
	responses2 := stream.getResponses()
	require.Len(t, responses2, 1)
	assert.Equal(t, int64(1), responses2[0].WatchID)
}

// TestMockWatchStream_ConcurrentSend verifies thread safety.
func TestMockWatchStream_ConcurrentSend(t *testing.T) {
	stream := newMockWatchStream()

	// Send responses concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			resp := &WatchResponse{WatchID: int64(n)}
			stream.Send(resp)
			done <- true
		}(i)
	}

	// Wait for all sends
	for i := 0; i < 10; i++ {
		<-done
	}

	responses := stream.getResponses()
	assert.Equal(t, 10, len(responses))
}
