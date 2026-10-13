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
	"encoding/json"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// JSON-RPC 2.0 error codes as defined in the MCP specification.
const (
	// Standard JSON-RPC 2.0 error codes.
	JSONRPCParseError     = -32700
	JSONRPCInvalidRequest = -32600
	JSONRPCMethodNotFound = -32601
	JSONRPCInvalidParams  = -32602
	JSONRPCInternalError  = -32603

	// Custom QuicBaaS MCP error codes for authorization.
	MCPAuthenticationRequired = -32001
	MCPInvalidToken           = -32002
	MCPPermissionDenied       = -32003

	// Custom QuicBaaS MCP error codes for service errors.
	// These map to QuicBaaS service error codes.
	MCPResourceNotFound    = -32004
	MCPResourceConflict    = -32005
	MCPServiceUnavailable  = -32006
	MCPRequestTimeout      = -32007
	MCPResourceExhausted   = -32008
	MCPPreconditionFailed  = -32009
	MCPOperationNotAllowed = -32010
	MCPOperationCanceled   = -32011
	MCPInvalidInput        = -32012
	MCPAlreadyExists       = -32013
	MCPUnimplemented       = -32014
)

// JSONRPCError represents a JSON-RPC 2.0 error returned by the server.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *JSONRPCError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("JSON-RPC error %d: %s: %v", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// ToSDKError converts a JSON-RPC error to an SDK QBError.
func (e *JSONRPCError) ToSDKError(op string) *errors.QBError {
	code := e.mapToErrorCode()
	msg := e.Message
	if e.Data != nil {
		if dataStr, ok := e.Data.(string); ok && dataStr != "" {
			msg = fmt.Sprintf("%s: %s", e.Message, dataStr)
		}
	}
	return errors.New(code, op, msg)
}

// mapToErrorCode maps JSON-RPC error codes to SDK error codes.
func (e *JSONRPCError) mapToErrorCode() errors.ErrorCode {
	switch e.Code {
	// Standard JSON-RPC 2.0 error codes
	case JSONRPCParseError:
		return errors.CodeInvalidInput
	case JSONRPCInvalidRequest:
		return errors.CodeInvalidInput
	case JSONRPCMethodNotFound:
		return errors.CodeNotFound
	case JSONRPCInvalidParams:
		return errors.CodeInvalidInput
	case JSONRPCInternalError:
		return errors.CodeInternal

	// Custom authorization error codes
	case MCPAuthenticationRequired:
		return errors.CodeUnauthorized
	case MCPInvalidToken:
		return errors.CodeUnauthorized
	case MCPPermissionDenied:
		return errors.CodeForbidden

	// Custom service error codes
	case MCPResourceNotFound:
		return errors.CodeNotFound
	case MCPResourceConflict:
		return errors.CodeConflict
	case MCPServiceUnavailable:
		return errors.CodeUnavailable
	case MCPRequestTimeout:
		return errors.CodeTimeout
	case MCPResourceExhausted:
		return errors.CodeResourceExhausted
	case MCPPreconditionFailed:
		return errors.CodePreconditionFailed
	case MCPOperationNotAllowed:
		return errors.CodeForbidden
	case MCPOperationCanceled:
		return errors.CodeCanceled
	case MCPInvalidInput:
		return errors.CodeInvalidInput
	case MCPAlreadyExists:
		return errors.CodeAlreadyExists
	case MCPUnimplemented:
		return errors.CodeUnimplemented

	default:
		// Check for server-defined error range
		if e.Code >= -32099 && e.Code <= -32000 {
			return errors.CodeInternal
		}
		return errors.CodeUnknown
	}
}

// ToolError is the structured content of a tool result whose isError flag is
// set. Code is one of the error codes above and classifies the failure, so a
// tool refusal reaches the SDK caller with its typed code instead of as an
// internal error. The result's text content carries the message.
type ToolError struct {
	Code int `json:"code"`
}

// toolErrorCode reads the error code from a failed tool result's structured
// content. A result that carries none is an internal error.
func toolErrorCode(structured interface{}) int {
	if structured == nil {
		return JSONRPCInternalError
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		return JSONRPCInternalError
	}
	var toolErr ToolError
	if err := json.Unmarshal(raw, &toolErr); err != nil || toolErr.Code == 0 {
		return JSONRPCInternalError
	}
	return toolErr.Code
}

// ParseError represents an error parsing MCP responses.
type ParseError struct {
	Op      string
	Message string
	Err     error
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error.
func (e *ParseError) Unwrap() error {
	return e.Err
}

// ToSDKError converts a ParseError to an SDK QBError.
func (e *ParseError) ToSDKError() *errors.QBError {
	return errors.Wrap(e.Err, errors.CodeInvalidInput, e.Op, e.Message)
}

// ProtocolError represents an MCP protocol-level error.
type ProtocolError struct {
	Op      string
	Message string
}

// Error implements the error interface.
func (e *ProtocolError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// ToSDKError converts a ProtocolError to an SDK QBError.
func (e *ProtocolError) ToSDKError() *errors.QBError {
	return errors.New(errors.CodeTransport, e.Op, e.Message)
}

// InitializationError represents an error during MCP session initialization.
type InitializationError struct {
	Op      string
	Message string
	Err     error
}

// Error implements the error interface.
func (e *InitializationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error.
func (e *InitializationError) Unwrap() error {
	return e.Err
}

// ToSDKError converts an InitializationError to an SDK QBError.
func (e *InitializationError) ToSDKError() *errors.QBError {
	if e.Err != nil {
		return errors.Wrap(e.Err, errors.CodeConnection, e.Op, e.Message)
	}
	return errors.New(errors.CodeConnection, e.Op, e.Message)
}

// ToolNotFoundError represents an error when a tool is not found.
type ToolNotFoundError struct {
	ToolName string
	Service  string
	Method   string
}

// Error implements the error interface.
func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool not found: %s (service=%s, method=%s)", e.ToolName, e.Service, e.Method)
}

// ToSDKError converts a ToolNotFoundError to an SDK QBError.
func (e *ToolNotFoundError) ToSDKError() *errors.QBError {
	return errors.New(errors.CodeNotFound, "mcp.CallTool", e.Error())
}

// ContentParseError represents an error parsing MCP content responses.
type ContentParseError struct {
	Op      string
	Message string
	Content interface{}
}

// Error implements the error interface.
func (e *ContentParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// ToSDKError converts a ContentParseError to an SDK QBError.
func (e *ContentParseError) ToSDKError() *errors.QBError {
	return errors.New(errors.CodeInternal, e.Op, e.Message)
}
