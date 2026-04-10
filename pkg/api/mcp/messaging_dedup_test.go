//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Messaging Dedup Stats Tests
// =============================================================================

func TestHandleMsgDedupStats_MissingChannel(t *testing.T) {
	log := newTestLogger(t)
	svc := &mockService{}
	reg := &ToolRegistry{service: svc, logger: log}

	msgSvc := &mockMessagingService{}

	_, err := reg.handleMsgDedupStats(context.Background(), msgSvc, map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel")
}

func TestHandleMsgDedupStats_NotPushService(t *testing.T) {
	log := newTestLogger(t)
	svc := &mockService{}
	reg := &ToolRegistry{service: svc, logger: log}

	msgSvc := &mockMessagingService{}

	// mockMessagingService is not *messaging.PushService, so should return error
	_, err := reg.handleMsgDedupStats(context.Background(), msgSvc, map[string]interface{}{
		"channel": "test-channel",
	})
	require.Error(t, err)
}
