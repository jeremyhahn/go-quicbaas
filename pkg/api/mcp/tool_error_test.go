//go:build mcp

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	stderrors "errors"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestToolErrorResult_CarriesCode pins the wire shape of a failed tool call:
// the message as text content and the MCP code as structured content.
func TestToolErrorResult_CarriesCode(t *testing.T) {
	err := qberrors.New(qberrors.ErrAlreadyExists, "iam.ProvisionTenant", "tenant acme already exists")

	result := ToolErrorResult(nil, "quicbaas_test_tool", err)

	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPAlreadyExists}, result.StructuredContent)
	require.Len(t, result.Content, 1)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Equal(t, err.Error(), text.Text)
}

// TestToolErrorResult_UntypedIsInternal proves an error without a service
// code answers as an internal error rather than borrowing a client class.
func TestToolErrorResult_UntypedIsInternal(t *testing.T) {
	result := ToolErrorResult(nil, "quicbaas_test_tool", context.Canceled)

	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.JSONRPCInternalError}, result.StructuredContent)
}

// TestToolErrorResult_SanitizesServerClass pins that a server-class error's
// internal text (a path, a wrapped store error) never reaches the caller: the
// text is the sentence REST sends and only the code survives.
func TestToolErrorResult_SanitizesServerClass(t *testing.T) {
	err := qberrors.Wrap(stderrors.New("open /etc/quicbaas/secrets/tenant-ca.key: permission denied"),
		qberrors.ErrStorage, "secrets.store.Get")

	result := ToolErrorResult(nil, "quicbaas_secrets_get", err)

	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPServiceUnavailable}, result.StructuredContent)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Equal(t, qberrors.ServerFailureMessage, text.Text)
	require.NotContains(t, text.Text, "tenant-ca.key")
}

// TestToolErrorResult_KeepsRequestClassMessage pins that an error describing
// the request keeps its message, wrapped cause included.
func TestToolErrorResult_KeepsRequestClassMessage(t *testing.T) {
	err := qberrors.New(qberrors.ErrInvalidInput, "kv.Put", "key must not be empty")

	result := ToolErrorResult(nil, "quicbaas_kv_put", err)

	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPInvalidInput}, result.StructuredContent)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Equal(t, err.Error(), text.Text)
	require.Contains(t, text.Text, "key must not be empty")
}

// TestToolErrorResult_SealedWindowIsUnavailable pins that the sealed-window
// refusal answers as service-unavailable, as REST and gRPC do, and keeps its
// fixed text.
func TestToolErrorResult_SealedWindowIsUnavailable(t *testing.T) {
	err := &SealedWindowError{Tool: "quicbaas_kv_get"}

	result := ToolErrorResult(nil, "quicbaas_kv_get", err)

	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPServiceUnavailable}, result.StructuredContent)
	text, ok := mcpgo.AsTextContent(result.Content[0])
	require.True(t, ok)
	require.Equal(t, err.Error(), text.Text)
}

// internalClassError is a ClassifiedError whose class no MCP code names.
type internalClassError struct{}

func (internalClassError) Error() string                 { return "internal refusal" }
func (internalClassError) ErrorCode() qberrors.ErrorCode { return qberrors.ErrInternal }

func TestToolErrorCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", qberrors.NotFound("secrets.Get", "secret"), mcptransport.MCPResourceNotFound},
		{"forbidden", qberrors.New(qberrors.ErrForbidden, "op", "denied"), mcptransport.MCPPermissionDenied},
		{"cross tenant", qberrors.New(qberrors.ErrCrossTenantAccess, "op", "denied"), mcptransport.MCPPermissionDenied},
		{"validation", qberrors.New(qberrors.ErrValidation, "op", "bad"), mcptransport.MCPInvalidInput},
		{"conflict", qberrors.New(qberrors.ErrConflict, "op", "stale"), mcptransport.MCPResourceConflict},
		{"unavailable", qberrors.New(qberrors.ErrLeaderNotAvailable, "op", "no leader"), mcptransport.MCPServiceUnavailable},
		{"outermost code wins", &qberrors.QBError{
			Code: qberrors.ErrPreconditionFailed,
			Op:   "mcp.tenant_barrier",
			Err:  qberrors.NotFound("barrier.Status", "tenant barrier"),
		}, mcptransport.MCPPreconditionFailed},
		{"server class", qberrors.New(qberrors.ErrInternal, "op", "broken"), mcptransport.JSONRPCInternalError},
		{"untyped", context.DeadlineExceeded, mcptransport.JSONRPCInternalError},
		{"sealed window", &SealedWindowError{Tool: "quicbaas_kv_get"}, mcptransport.MCPServiceUnavailable},
		{"classified unmapped class", internalClassError{}, mcptransport.JSONRPCInternalError},
		// gRPC pins the same refusals to codes.ResourceExhausted in
		// TestConvertOrchestratorError_AFleetCapacityRefusalIsResourceExhausted.
		{"fleet capacity refused", &orchestration.FleetCapacityRefusedError{Backend: "claude"}, mcptransport.MCPResourceExhausted},
		{"agent pool exhausted", orchestration.ErrAgentPoolExhausted, mcptransport.MCPResourceExhausted},
		// The project and SDLC services return their own typed errors; gRPC
		// pins the same classes in convertProjectError and convertSDLCError.
		{"project not found", &service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"}, mcptransport.MCPResourceNotFound},
		{"project already exists", &service.ProjectAlreadyExistsError{TenantID: "t", Name: "p"}, mcptransport.MCPAlreadyExists},
		{"project access denied", &service.ProjectAccessDeniedError{}, mcptransport.MCPPermissionDenied},
		{"binding not found", sdlc.ErrNotFound, mcptransport.MCPResourceNotFound},
		{"binding invalid", &sdlc.InvalidInputError{Field: "binding", Message: "is required"}, mcptransport.MCPInvalidInput},
		{"adoption refused", sdlc.ErrAdoptionRefused, mcptransport.MCPPreconditionFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, toolErrorCode(tc.err))
		})
	}
}

func TestDomainErrorClass_ClassesProjectAndSDLCErrors(t *testing.T) {
	code, ok := domainErrorClass(&service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"})
	require.True(t, ok)
	require.Equal(t, qberrors.ErrNotFound, code)

	code, ok = domainErrorClass(sdlc.ErrVersionConflict)
	require.True(t, ok)
	require.Equal(t, qberrors.ErrAlreadyExists, code)
}

func TestDomainErrorClass_LeavesOtherErrorsUnclassed(t *testing.T) {
	_, ok := domainErrorClass(context.Canceled)
	require.False(t, ok)
	_, ok = domainErrorClass(qberrors.New(qberrors.ErrNotFound, "op", "already coded"))
	require.False(t, ok, "a coded error is classed by its own code, not here")
}
