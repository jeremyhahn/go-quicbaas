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
	stderrors "errors"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// toolErrorCodes classifies a service error code as the MCP error code the
// SDK MCP transport turns back into its typed error. The classes follow the
// REST mapping (ErrorCode.HTTPStatus); a code absent from the map describes
// the server, not the request, and answers as an internal error.
var toolErrorCodes = map[qberrors.ErrorCode]int{
	qberrors.ErrInvalidInput:            mcptransport.MCPInvalidInput,
	qberrors.ErrValidation:              mcptransport.MCPInvalidInput,
	qberrors.ErrOutOfRange:              mcptransport.MCPInvalidInput,
	qberrors.ErrMissingTenantContext:    mcptransport.MCPInvalidInput,
	qberrors.ErrMissingJustification:    mcptransport.MCPInvalidInput,
	qberrors.ErrMissingUserContext:      mcptransport.MCPInvalidInput,
	qberrors.ErrNotFound:                mcptransport.MCPResourceNotFound,
	qberrors.ErrAlreadyExists:           mcptransport.MCPAlreadyExists,
	qberrors.ErrConflict:                mcptransport.MCPResourceConflict,
	qberrors.ErrUnauthorized:            mcptransport.MCPAuthenticationRequired,
	qberrors.ErrForbidden:               mcptransport.MCPPermissionDenied,
	qberrors.ErrSystemKeyspaceViolation: mcptransport.MCPPermissionDenied,
	qberrors.ErrCrossTenantAccess:       mcptransport.MCPPermissionDenied,
	qberrors.ErrNotAllowed:              mcptransport.MCPOperationNotAllowed,
	qberrors.ErrPreconditionFailed:      mcptransport.MCPPreconditionFailed,
	qberrors.ErrResourceExhausted:       mcptransport.MCPResourceExhausted,
	qberrors.ErrRateLimited:             mcptransport.MCPResourceExhausted,
	qberrors.ErrTimeout:                 mcptransport.MCPRequestTimeout,
	qberrors.ErrDeadlineExceeded:        mcptransport.MCPRequestTimeout,
	qberrors.ErrCanceled:                mcptransport.MCPOperationCanceled,
	qberrors.ErrUnimplemented:           mcptransport.MCPUnimplemented,
	qberrors.ErrNotImplemented:          mcptransport.MCPUnimplemented,
	qberrors.ErrUnavailable:             mcptransport.MCPServiceUnavailable,
	qberrors.ErrLeaderNotAvailable:      mcptransport.MCPServiceUnavailable,
	qberrors.ErrClusterNotReady:         mcptransport.MCPServiceUnavailable,
	qberrors.ErrQuorumLost:              mcptransport.MCPServiceUnavailable,
	qberrors.ErrStorage:                 mcptransport.MCPServiceUnavailable,
	qberrors.ErrRaft:                    mcptransport.MCPServiceUnavailable,
	qberrors.ErrCircuitOpen:             mcptransport.MCPServiceUnavailable,
}

// ClassifiedError is an error that is not a QBError but belongs to an error
// class: a refusal the MCP surface itself raises, such as SealedWindowError.
// Its text is fixed and names only the refusal, so it is returned as is; its
// class is resolved through toolErrorCodes like any other.
type ClassifiedError interface {
	error
	ErrorCode() qberrors.ErrorCode
}

// ToolErrorResult answers a failed call of tool. The structured content
// carries the error's MCP code, so the SDK caller gets the same typed error
// the other transports return. The text content keeps the message of an
// error that describes the request; an error that describes the server is
// answered with the sentence REST sends and its cause is logged, so internal
// detail never reaches the caller.
func ToolErrorResult(log *logger.Logger, tool string, err error) *mcpgo.CallToolResult {
	result := mcpgo.NewToolResultError(toolErrorMessage(log, tool, err))
	result.StructuredContent = mcptransport.ToolError{Code: toolErrorCode(err)}
	return result
}

// toolErrorMessage returns the text a failed tool call carries, by the
// classification REST applies (qberrors.DescribesRequest).
func toolErrorMessage(log *logger.Logger, tool string, err error) string {
	var qbErr *qberrors.QBError
	if !qberrors.As(err, &qbErr) || qberrors.DescribesRequest(qbErr.Code) {
		return err.Error()
	}
	if log != nil {
		log.WithFields(map[string]interface{}{
			"component": "mcp-tool-error",
			"tool":      tool,
			"error":     err.Error(),
		}).Error("tool error returned to caller as a sanitized response")
	}
	return qberrors.ServerFailureMessage
}

// toolErrorCode returns the MCP error code for err: the class of the coded
// error it wraps, or the JSON-RPC internal-error code when it has none.
func toolErrorCode(err error) int {
	var qbErr *qberrors.QBError
	if qberrors.As(err, &qbErr) {
		return toolErrorCodeOf(qbErr.Code)
	}
	var classified ClassifiedError
	if stderrors.As(err, &classified) {
		return toolErrorCodeOf(classified.ErrorCode())
	}
	if fault, ok := orchestration.ClassifyAgentCatalogError(err); ok {
		return toolErrorCodeOf(agentCatalogClasses[fault])
	}
	if fault, ok := service.ClassifyPromptError(err); ok {
		return toolErrorCodeOf(promptClasses[fault])
	}
	if code, ok := domainErrorClass(err); ok {
		return toolErrorCodeOf(code)
	}
	return mcptransport.JSONRPCInternalError
}

// agentCatalogClasses carries orchestration's catalog faults to the class
// gRPC's agentCatalogCodes gives them, so a refusal such as an exhausted pool
// or a full fleet reads the same on every transport.
var agentCatalogClasses = map[orchestration.AgentCatalogFault]qberrors.ErrorCode{
	orchestration.AgentCatalogFaultInvalid:     qberrors.ErrInvalidInput,
	orchestration.AgentCatalogFaultNotFound:    qberrors.ErrNotFound,
	orchestration.AgentCatalogFaultConflict:    qberrors.ErrAlreadyExists,
	orchestration.AgentCatalogFaultInUse:       qberrors.ErrPreconditionFailed,
	orchestration.AgentCatalogFaultForbidden:   qberrors.ErrForbidden,
	orchestration.AgentCatalogFaultUnavailable: qberrors.ErrUnavailable,
	orchestration.AgentCatalogFaultExhausted:   qberrors.ErrResourceExhausted,
}

// promptClasses carries the prompt service's faults to the class gRPC's
// promptFaultCode gives them, so a prompt refusal reads the same on every
// transport.
var promptClasses = map[service.PromptFault]qberrors.ErrorCode{
	service.PromptFaultInvalid:     qberrors.ErrInvalidInput,
	service.PromptFaultNotFound:    qberrors.ErrNotFound,
	service.PromptFaultConflict:    qberrors.ErrConflict,
	service.PromptFaultUnavailable: qberrors.ErrUnavailable,
}

// domainSentinelClasses classes the project and SDLC sentinels a tool returns
// the way the gRPC and REST transports do, so an MCP caller reads an unknown
// project or binding as not-found rather than as an internal fault.
var domainSentinelClasses = []struct {
	sentinel error
	code     qberrors.ErrorCode
}{
	{sdlc.ErrNotFound, qberrors.ErrNotFound},
	{orchestration.ErrProjectNotFound, qberrors.ErrNotFound},
	{orchestration.ErrTaskNotFound, qberrors.ErrNotFound},
	{orchestration.ErrWorkflowNotFound, qberrors.ErrNotFound},
	{orchestration.ErrFleetNodeNotFound, qberrors.ErrNotFound},
	{orchestration.ErrBackendNotFound, qberrors.ErrNotFound},
	{orchestration.ErrWorkflowAlreadyExists, qberrors.ErrAlreadyExists},
	{sdlc.ErrAlreadyExists, qberrors.ErrAlreadyExists},
	{sdlc.ErrVersionConflict, qberrors.ErrAlreadyExists},
	{sdlc.ErrWebhookRouteConflict, qberrors.ErrAlreadyExists},
	{orchestration.ErrTaskSourceAlreadyAdopted, qberrors.ErrAlreadyExists},
	{sdlc.ErrAdoptionRefused, qberrors.ErrPreconditionFailed},
	{orchestration.ErrTaskInvalidState, qberrors.ErrPreconditionFailed},
	{sdlc.ErrAdoptionIncomplete, qberrors.ErrUnavailable},
	{sdlc.ErrUnavailable, qberrors.ErrUnavailable},
	{sdlc.ErrInvalidInput, qberrors.ErrInvalidInput},
	{sdlc.ErrReview, qberrors.ErrInvalidInput},
	{orchestration.ErrWorkflowValidation, qberrors.ErrInvalidInput},
	{orchestration.ErrWorkflowRetiredField, qberrors.ErrInvalidInput},
	{orchestration.ErrInvalidInput, qberrors.ErrInvalidInput},
}

// domainErrorClass classes a project or SDLC service error, reporting false
// for any other error.
func domainErrorClass(err error) (qberrors.ErrorCode, bool) {
	var (
		accessDenied  *service.ProjectAccessDeniedError
		notFound      *service.ProjectNotFoundError
		unresolved    *service.ProjectUnresolvedError
		alreadyExists *service.ProjectAlreadyExistsError
		invalid       *service.ProjectInvalidError
		roleInvalid   *service.ProjectRoleInvalidError
		forge         *sdlc.ForgeNotRegisteredError
		strategy      *sdlc.StrategyNotRegisteredError
	)
	switch {
	case stderrors.As(err, &accessDenied):
		return qberrors.ErrForbidden, true
	case stderrors.As(err, &notFound), stderrors.As(err, &unresolved):
		return qberrors.ErrNotFound, true
	case stderrors.As(err, &alreadyExists):
		return qberrors.ErrAlreadyExists, true
	case stderrors.As(err, &invalid), stderrors.As(err, &roleInvalid),
		stderrors.As(err, &forge), stderrors.As(err, &strategy):
		return qberrors.ErrInvalidInput, true
	}
	for _, class := range domainSentinelClasses {
		if stderrors.Is(err, class.sentinel) {
			return class.code, true
		}
	}
	return 0, false
}

// toolErrorCodeOf returns the MCP error code for an error class.
func toolErrorCodeOf(class qberrors.ErrorCode) int {
	if code, ok := toolErrorCodes[class]; ok {
		return code
	}
	return mcptransport.JSONRPCInternalError
}
