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
	"sync"
)

// mockWatchStream is a test implementation of WatchStream
// Used by both integration tests and benchmarks.
type mockWatchStream struct {
	ctx       context.Context
	responses []*WatchResponse
	mu        sync.Mutex
}

func newMockWatchStream() *mockWatchStream {
	return &mockWatchStream{
		ctx:       context.Background(),
		responses: make([]*WatchResponse, 0),
	}
}

func newMockWatchStreamWithContext(ctx context.Context) *mockWatchStream {
	return &mockWatchStream{
		ctx:       ctx,
		responses: make([]*WatchResponse, 0),
	}
}

func (m *mockWatchStream) Send(resp *WatchResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, resp)
	return nil
}

func (m *mockWatchStream) Context() context.Context {
	return m.ctx
}

func (m *mockWatchStream) getResponses() []*WatchResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*WatchResponse, len(m.responses))
	copy(result, m.responses)
	return result
}
