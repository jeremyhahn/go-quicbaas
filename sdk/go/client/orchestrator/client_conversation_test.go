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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// GetSessionConversation
// =============================================================================

// TestGetSessionConversation_HappyPath asserts the client returns the page the
// transport produced, typed.
func TestGetSessionConversation_HappyPath(t *testing.T) {
	want := &GetSessionConversationResponse{
		SessionID: "s1",
		Turns: []*SessionTurn{
			{Sequence: 1, Role: SessionTurnRoleUser, Text: "hi"},
			{Sequence: 2, Role: SessionTurnRoleAssistant, Text: "hello",
				Usage: &SessionTokenUsage{OutputTokens: 8}},
		},
		NextCursor: 3,
	}
	c := NewClient(okTransport(t, "GetSessionConversation", want))

	resp, err := c.GetSessionConversation(context.Background(),
		&GetSessionConversationRequest{SessionID: "s1", Cursor: 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, resp.Turns, 2)
	assert.Equal(t, SessionTurnRoleAssistant, resp.Turns[1].Role)
	require.NotNil(t, resp.Turns[1].Usage)
	assert.Equal(t, int64(8), resp.Turns[1].Usage.OutputTokens)
	assert.Equal(t, uint64(3), resp.NextCursor)
}

// TestGetSessionConversation_Validation asserts every unusable request is
// refused before it reaches a transport.
func TestGetSessionConversation_Validation(t *testing.T) {
	c := NewClient(failTransport())

	var nilCtx context.Context
	_, err := c.GetSessionConversation(nilCtx, &GetSessionConversationRequest{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context is required")

	_, err = c.GetSessionConversation(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session_id is required")

	_, err = c.GetSessionConversation(context.Background(), &GetSessionConversationRequest{})
	require.Error(t, err)
}

// TestGetSessionConversation_TransportError asserts a transport failure is
// returned rather than swallowed into an empty conversation.
func TestGetSessionConversation_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetSessionConversation(context.Background(),
		&GetSessionConversationRequest{SessionID: "s1"})
	require.Error(t, err)
}

// TestGetSessionConversation_TypeMismatch asserts a transport that returns the
// wrong shape is an error, not a nil-dereference at the call site.
func TestGetSessionConversation_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetSessionConversation(context.Background(),
		&GetSessionConversationRequest{SessionID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid response type")
}
