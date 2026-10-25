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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestLoginFinish_TenantRefusedIsPreconditionFailed checks that a non-system
// tenant login refused at token issuance leaves the login-finish tool as the
// coded MCPPreconditionFailed sentinel with the refusal's guidance intact, not
// the sanitized server-failure text. go-quicbaas #1816 removes the refusal.
func TestLoginFinish_TenantRefusedIsPreconditionFailed(t *testing.T) {
	login := &fakeLoginServicer{
		finishFunc: func(ctx context.Context, req *qrdbtransport.FinishLoginRequest) (*service.FinishLoginResult, error) {
			return nil, service.RefuseTenantLogin("acme")
		},
	}
	_, err := executeLoginFinish(context.Background(), login, map[string]interface{}{"session_id": "s", "type": "piv"})
	require.Error(t, err)

	result := ToolErrorResult(nil, "quicbaas_login_finish", err)

	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPPreconditionFailed}, result.StructuredContent)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Contains(t, text.Text, (&sdkerrors.TenantLoginRefusedError{}).Error())
}
