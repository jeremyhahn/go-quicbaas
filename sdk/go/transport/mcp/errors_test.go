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
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestJSONRPCError(t *testing.T) {
	t.Run("Error without data", func(t *testing.T) {
		err := &JSONRPCError{
			Code:    JSONRPCInternalError,
			Message: "internal error",
		}
		expected := "JSON-RPC error -32603: internal error"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("Error with data", func(t *testing.T) {
		err := &JSONRPCError{
			Code:    JSONRPCInvalidParams,
			Message: "invalid params",
			Data:    "missing required field",
		}
		expected := "JSON-RPC error -32602: invalid params: missing required field"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("ToSDKError with string data", func(t *testing.T) {
		rpcErr := &JSONRPCError{
			Code:    JSONRPCInvalidRequest,
			Message: "invalid request",
			Data:    "extra details",
		}
		sdkErr := rpcErr.ToSDKError("test.op")
		if sdkErr.Code != errors.CodeInvalidInput {
			t.Errorf("expected CodeInvalidInput, got %v", sdkErr.Code)
		}
		if sdkErr.Op != "test.op" {
			t.Errorf("expected op test.op, got %s", sdkErr.Op)
		}
	})

	t.Run("ToSDKError with non-string data", func(t *testing.T) {
		rpcErr := &JSONRPCError{
			Code:    JSONRPCInternalError,
			Message: "internal error",
			Data:    123, // Non-string data
		}
		sdkErr := rpcErr.ToSDKError("test.op")
		if sdkErr.Code != errors.CodeInternal {
			t.Errorf("expected CodeInternal, got %v", sdkErr.Code)
		}
		// Should just use the message without data
		if sdkErr.Message != "internal error" {
			t.Errorf("expected message 'internal error', got %s", sdkErr.Message)
		}
	})

	t.Run("mapToErrorCode for all codes", func(t *testing.T) {
		tests := []struct {
			code     int
			expected errors.ErrorCode
		}{
			{JSONRPCParseError, errors.CodeInvalidInput},
			{JSONRPCInvalidRequest, errors.CodeInvalidInput},
			{JSONRPCMethodNotFound, errors.CodeNotFound},
			{JSONRPCInvalidParams, errors.CodeInvalidInput},
			{JSONRPCInternalError, errors.CodeInternal},
			{MCPAuthenticationRequired, errors.CodeUnauthorized},
			{MCPInvalidToken, errors.CodeUnauthorized},
			{MCPPermissionDenied, errors.CodeForbidden},
			{-32050, errors.CodeInternal}, // Server error range
			{-32000, errors.CodeInternal}, // Server error range
			{12345, errors.CodeUnknown},   // Unknown code
		}

		for _, tc := range tests {
			err := &JSONRPCError{Code: tc.code, Message: "test"}
			result := err.mapToErrorCode()
			if result != tc.expected {
				t.Errorf("code %d: expected %v, got %v", tc.code, tc.expected, result)
			}
		}
	})
}

func TestMCPApplicationErrorCodesPinned(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{name: "authentication required", got: MCPAuthenticationRequired, want: -32001},
		{name: "invalid token", got: MCPInvalidToken, want: -32002},
		{name: "permission denied", got: MCPPermissionDenied, want: -32003},
		{name: "resource not found", got: MCPResourceNotFound, want: -32004},
		{name: "resource conflict", got: MCPResourceConflict, want: -32005},
		{name: "service unavailable", got: MCPServiceUnavailable, want: -32006},
		{name: "request timeout", got: MCPRequestTimeout, want: -32007},
		{name: "resource exhausted", got: MCPResourceExhausted, want: -32008},
		{name: "precondition failed", got: MCPPreconditionFailed, want: -32009},
		{name: "operation not allowed", got: MCPOperationNotAllowed, want: -32010},
		{name: "operation canceled", got: MCPOperationCanceled, want: -32011},
		{name: "invalid input", got: MCPInvalidInput, want: -32012},
		{name: "already exists", got: MCPAlreadyExists, want: -32013},
		{name: "unimplemented", got: MCPUnimplemented, want: -32014},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("MCP error code = %d, want %d", tc.got, tc.want)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	t.Run("Error with underlying error", func(t *testing.T) {
		underlying := stderrors.New("underlying error")
		err := &ParseError{
			Op:      "test.parse",
			Message: "failed to parse",
			Err:     underlying,
		}
		expected := "test.parse: failed to parse: underlying error"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("Error without underlying error", func(t *testing.T) {
		err := &ParseError{
			Op:      "test.parse",
			Message: "failed to parse",
		}
		expected := "test.parse: failed to parse"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := stderrors.New("underlying error")
		err := &ParseError{
			Op:      "test.parse",
			Message: "failed to parse",
			Err:     underlying,
		}
		if err.Unwrap() != underlying {
			t.Errorf("expected underlying error, got %v", err.Unwrap())
		}
	})

	t.Run("ToSDKError", func(t *testing.T) {
		underlying := stderrors.New("underlying error")
		err := &ParseError{
			Op:      "test.parse",
			Message: "failed to parse",
			Err:     underlying,
		}
		sdkErr := err.ToSDKError()
		if sdkErr.Code != errors.CodeInvalidInput {
			t.Errorf("expected CodeInvalidInput, got %v", sdkErr.Code)
		}
	})
}

func TestProtocolError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ProtocolError{
			Op:      "test.protocol",
			Message: "protocol error",
		}
		expected := "test.protocol: protocol error"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("ToSDKError", func(t *testing.T) {
		err := &ProtocolError{
			Op:      "test.protocol",
			Message: "protocol error",
		}
		sdkErr := err.ToSDKError()
		if sdkErr.Code != errors.CodeTransport {
			t.Errorf("expected CodeTransport, got %v", sdkErr.Code)
		}
	})
}

func TestInitializationError(t *testing.T) {
	t.Run("Error with underlying error", func(t *testing.T) {
		underlying := stderrors.New("underlying error")
		err := &InitializationError{
			Op:      "test.init",
			Message: "failed to initialize",
			Err:     underlying,
		}
		expected := "test.init: failed to initialize: underlying error"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("Error without underlying error", func(t *testing.T) {
		err := &InitializationError{
			Op:      "test.init",
			Message: "failed to initialize",
		}
		expected := "test.init: failed to initialize"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := stderrors.New("underlying error")
		err := &InitializationError{
			Op:      "test.init",
			Message: "failed to initialize",
			Err:     underlying,
		}
		if err.Unwrap() != underlying {
			t.Errorf("expected underlying error, got %v", err.Unwrap())
		}
	})

	t.Run("ToSDKError with underlying error", func(t *testing.T) {
		underlying := stderrors.New("underlying error")
		err := &InitializationError{
			Op:      "test.init",
			Message: "failed to initialize",
			Err:     underlying,
		}
		sdkErr := err.ToSDKError()
		if sdkErr.Code != errors.CodeConnection {
			t.Errorf("expected CodeConnection, got %v", sdkErr.Code)
		}
	})

	t.Run("ToSDKError without underlying error", func(t *testing.T) {
		err := &InitializationError{
			Op:      "test.init",
			Message: "failed to initialize",
		}
		sdkErr := err.ToSDKError()
		if sdkErr.Code != errors.CodeConnection {
			t.Errorf("expected CodeConnection, got %v", sdkErr.Code)
		}
	})
}

func TestToolNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ToolNotFoundError{
			ToolName: "quicbaas_unknown",
			Service:  "quicbaas.Unknown",
			Method:   "Test",
		}
		expected := "tool not found: quicbaas_unknown (service=quicbaas.Unknown, method=Test)"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("ToSDKError", func(t *testing.T) {
		err := &ToolNotFoundError{
			ToolName: "quicbaas_unknown",
			Service:  "quicbaas.Unknown",
			Method:   "Test",
		}
		sdkErr := err.ToSDKError()
		if sdkErr.Code != errors.CodeNotFound {
			t.Errorf("expected CodeNotFound, got %v", sdkErr.Code)
		}
	})
}

func TestContentParseError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ContentParseError{
			Op:      "test.content",
			Message: "failed to parse content",
			Content: "invalid content",
		}
		expected := "test.content: failed to parse content"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("ToSDKError", func(t *testing.T) {
		err := &ContentParseError{
			Op:      "test.content",
			Message: "failed to parse content",
		}
		sdkErr := err.ToSDKError()
		if sdkErr.Code != errors.CodeInternal {
			t.Errorf("expected CodeInternal, got %v", sdkErr.Code)
		}
	})
}
