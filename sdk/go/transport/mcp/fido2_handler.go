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

// fido2_handler.go carries the tenant-scoped WebAuthn ceremony verbs behind
// Security().Auth().FIDO2() over the MCP transport, mapping each method onto
// the server's xkms.fido2.* tools. The SDK request and response types are
// the go-xkms transport structs verbatim; the only quicbaas addition is the
// tenant_id argument injected from the request context
// (transport.TenantFromContext), because QuicBaaS runs one relying party per
// tenant while xkms runs exactly one.
//
// Note the MCP posture difference: on REST and QUIC the authentication
// ceremony is pre-auth, but the MCP server keeps all four verbs behind
// authorization (see pkg/api/mcp/authz_qrdb.go) — widening the
// unauthenticated MCP tool surface is an owner decision.

package mcp

import (
	"encoding/json"
	"strings"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fido2ServiceName is the SDK service identifier for the FIDO2 surface.
const fido2ServiceName = "quicbaas.FIDO2"

// isMCPFIDO2Service reports whether the service name targets the FIDO2
// surface.
func isMCPFIDO2Service(service string) bool {
	return strings.HasPrefix(service, fido2ServiceName) || service == "FIDO2"
}

// fido2MethodTools maps FIDO2 methods (the xkms FIDO2Service vocabulary) to
// the server's xkms.fido2.* tool names.
var fido2MethodTools = map[string]string{
	"BeginRegistration":    "xkms.fido2.beginRegistration",
	"FinishRegistration":   "xkms.fido2.finishRegistration",
	"BeginAuthentication":  "xkms.fido2.beginAuthentication",
	"FinishAuthentication": "xkms.fido2.finishAuthentication",
}

// fido2ToolName returns the MCP tool name for a FIDO2 method and whether the
// method is wired over MCP.
func fido2ToolName(method string) (string, bool) {
	name, ok := fido2MethodTools[method]
	return name, ok
}

// fido2MCPHandler adapts the xkms transport FIDO2 types to the MCP tool
// argument and result shapes. It carries no state.
type fido2MCPHandler struct{}

// newFIDO2MCPHandler creates a FIDO2 MCP handler.
func newFIDO2MCPHandler() *fido2MCPHandler { return &fido2MCPHandler{} }

// transformRequest converts an xkms transport FIDO2 request into MCP tool
// arguments via JSON round-trip: the struct's snake_case JSON tags are the
// tool argument names. The caller's tenant scope is injected by the
// transport loop (see doRequest), not here.
func (h *fido2MCPHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.fido2."+method, "request is required")
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"mcp.fido2."+method, "failed to encode request")
	}
	var args map[string]interface{}
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"mcp.fido2."+method, "request is not a JSON object")
	}
	return args, nil
}

// fido2ResultTransformers maps method names to their result decoders for
// O(1) dispatch.
var fido2ResultTransformers = map[string]func(result interface{}) (interface{}, error){
	"BeginRegistration":    decodeFIDO2BeginRegistrationResult,
	"FinishRegistration":   decodeFIDO2FinishRegistrationResult,
	"BeginAuthentication":  decodeFIDO2BeginAuthenticationResult,
	"FinishAuthentication": decodeFIDO2FinishAuthenticationResult,
}

// transformResponse converts an MCP tool result into the xkms transport
// response type for the method.
func (h *fido2MCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	transformer, ok := fido2ResultTransformers[method]
	if !ok {
		return nil, errors.NewUnimplemented("mcp.fido2."+method,
			"method not exposed by the FIDO2 MCP tool set")
	}
	return transformer(result)
}

// decodeFIDO2Result round-trips a tool result map into out.
func decodeFIDO2Result(method string, result, out interface{}) error {
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal,
			"mcp.fido2."+method, "failed to re-encode tool result")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.Wrap(err, errors.CodeInternal,
			"mcp.fido2."+method, "failed to decode tool result")
	}
	return nil
}

func decodeFIDO2BeginRegistrationResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.BeginRegistrationResponse
	if err := decodeFIDO2Result("BeginRegistration", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func decodeFIDO2FinishRegistrationResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.FinishRegistrationResponse
	if err := decodeFIDO2Result("FinishRegistration", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func decodeFIDO2BeginAuthenticationResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.BeginAuthenticationResponse
	if err := decodeFIDO2Result("BeginAuthentication", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func decodeFIDO2FinishAuthenticationResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.FinishAuthenticationResponse
	if err := decodeFIDO2Result("FinishAuthentication", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
