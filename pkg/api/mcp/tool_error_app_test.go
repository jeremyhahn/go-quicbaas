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
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// appStoreFailureDetail is internal text a failing store reports; no tool
// result may carry it.
const appStoreFailureDetail = "write /var/lib/quicbaas/apps.db: input/output error"

// appStoreFailureError is the error a failing application store returns.
type appStoreFailureError struct{}

func (appStoreFailureError) Error() string { return appStoreFailureDetail }

// toolResultText returns the text a tool error result carries.
func toolResultText(t *testing.T, result *mcpgo.CallToolResult) string {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(mcpgo.TextContent)
	require.True(t, ok, "content is %T", result.Content[0])
	return text.Text
}

// TestToolErrorResult_AppMetadataErrorIsInvalidInput proves an app register or
// update refused for its metadata answers invalid input with the refusal.
func TestToolErrorResult_AppMetadataErrorIsInvalidInput(t *testing.T) {
	t.Parallel()
	err := &iam.AppMetadataError{
		Cause: &iam.ResourceURIError{Detail: "default audience must be an absolute URI"},
	}

	result := ToolErrorResult(nil, "iam_app_update", err)

	require.True(t, result.IsError)
	assert.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPInvalidInput}, result.StructuredContent)
	assert.Contains(t, toolResultText(t, result), "default audience must be an absolute URI")
}

// TestToolErrorResult_AppStorageFailureIsSanitized proves a store failure
// answers internal error without the store's text.
func TestToolErrorResult_AppStorageFailureIsSanitized(t *testing.T) {
	t.Parallel()
	err := &iam.IAMContextError{Message: "failed to store application", Cause: appStoreFailureError{}}

	result := ToolErrorResult(nil, "iam_app_register", err)

	require.True(t, result.IsError)
	assert.Equal(t, mcptransport.ToolError{Code: mcptransport.JSONRPCInternalError}, result.StructuredContent)
	assert.Equal(t, qberrors.ServerFailureMessage, toolResultText(t, result))
}
