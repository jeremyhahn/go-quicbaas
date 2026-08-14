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

// piv_handler.go carries the backend-scoped PIV verbs behind
// Security().Auth().PIV() over the MCP transport, mapping each method onto
// the server's xkms.piv.* tools. The SDK request and response types are the
// go-xkms transport structs verbatim; because their []byte fields marshal to
// base64 JSON strings — exactly the encoding the tool schema uses — both
// directions are plain JSON round-trips.

package mcp

import (
	"encoding/json"
	"strings"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// pivServiceName is the SDK service identifier for the PIV surface.
const pivServiceName = "quicbaas.PIV"

// isMCPPIVService reports whether the service name targets the PIV surface.
func isMCPPIVService(service string) bool {
	return strings.HasPrefix(service, pivServiceName) || service == "PIV"
}

// pivMethodTools maps PIV methods (the xkms PIVService vocabulary) to the
// server's xkms.piv.* tool names.
var pivMethodTools = map[string]string{
	"ListPIVSlots":         "xkms.piv.listSlots",
	"GetPIVCertificate":    "xkms.piv.getCert",
	"StorePIVCertificate":  "xkms.piv.storeCert",
	"DeletePIVCertificate": "xkms.piv.deleteCert",
	"GeneratePIVKey":       "xkms.piv.generateKey",
	"ImportPIVCertificate": "xkms.piv.importCert",
	"ExportPIVCertificate": "xkms.piv.exportCert",
	"GeneratePIVCSR":       "xkms.piv.generateCSR",
}

// pivToolName returns the MCP tool name for a PIV method and whether the
// method is wired over MCP.
func pivToolName(method string) (string, bool) {
	name, ok := pivMethodTools[method]
	return name, ok
}

// pivMCPHandler adapts the xkms transport PIV types to the MCP tool argument
// and result shapes. It carries no state.
type pivMCPHandler struct{}

// newPIVMCPHandler creates a PIV MCP handler.
func newPIVMCPHandler() *pivMCPHandler { return &pivMCPHandler{} }

// transformRequest converts an xkms transport PIV request into MCP tool
// arguments via JSON round-trip: the struct's snake_case JSON tags are the
// tool argument names, and []byte fields become the base64 strings the tool
// schema expects.
func (h *pivMCPHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.piv."+method, "request is required")
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"mcp.piv."+method, "failed to encode request")
	}
	var args map[string]interface{}
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"mcp.piv."+method, "request is not a JSON object")
	}
	return args, nil
}

// pivResultTransformers maps method names to their result decoders for O(1)
// dispatch. Void verbs decode to nil.
var pivResultTransformers = map[string]func(result interface{}) (interface{}, error){
	"ListPIVSlots":         decodePIVListSlotsResult,
	"GetPIVCertificate":    decodePIVCertificateResult,
	"ExportPIVCertificate": decodePIVCertificateResult,
	"GeneratePIVKey":       decodePIVGenerateKeyResult,
	"GeneratePIVCSR":       decodePIVGenerateCSRResult,
	"StorePIVCertificate":  decodePIVVoidResult,
	"DeletePIVCertificate": decodePIVVoidResult,
	"ImportPIVCertificate": decodePIVVoidResult,
}

// transformResponse converts an MCP tool result into the xkms transport
// response type for the method.
func (h *pivMCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	transformer, ok := pivResultTransformers[method]
	if !ok {
		return nil, errors.NewUnimplemented("mcp.piv."+method,
			"method not exposed by the PIV MCP tool set")
	}
	return transformer(result)
}

// decodePIVResult round-trips a tool result map into out.
func decodePIVResult(method string, result, out interface{}) error {
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal,
			"mcp.piv."+method, "failed to re-encode tool result")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.Wrap(err, errors.CodeInternal,
			"mcp.piv."+method, "failed to decode tool result")
	}
	return nil
}

func decodePIVListSlotsResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.ListPIVSlotsResponse
	if err := decodePIVResult("ListPIVSlots", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func decodePIVCertificateResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.GetPIVCertificateResponse
	if err := decodePIVResult("GetPIVCertificate", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func decodePIVGenerateKeyResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.GeneratePIVKeyResponse
	if err := decodePIVResult("GeneratePIVKey", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func decodePIVGenerateCSRResult(result interface{}) (interface{}, error) {
	var resp xkmstransport.GeneratePIVCSRResponse
	if err := decodePIVResult("GeneratePIVCSR", result, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// decodePIVVoidResult discards the {"success": true} acknowledgment the void
// verbs return.
func decodePIVVoidResult(interface{}) (interface{}, error) {
	return nil, nil
}
