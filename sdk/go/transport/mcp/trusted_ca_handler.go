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

// The re-exported go-xkms CAService (Security().TrustedCA()) over the MCP
// transport: each method maps onto the server's xkms.ca.* tool, which serves
// the same go-xkms CA as the REST /api/v1/xkms/ca/* routes (#1330).

package mcp

import (
	"encoding/json"
	"strings"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// trustedCAServiceName is the transport service name of the re-exported
// go-xkms CAService.
const trustedCAServiceName = "quicbaas.TrustedCA"

// isMCPTrustedCAService reports whether the service name targets the
// re-exported go-xkms CA.
func isMCPTrustedCAService(service string) bool {
	return strings.HasPrefix(service, trustedCAServiceName) || service == "TrustedCA"
}

// trustedCAMethodTools maps go-xkms CAService methods to the server's
// xkms.ca.* tool names.
var trustedCAMethodTools = map[string]string{
	"GetCABundle":       "xkms.ca.getBundle",
	"GetCACertificate":  "xkms.ca.getCert",
	"SignCSR":           "xkms.ca.signCSR",
	"IssueCertificate":  "xkms.ca.issueCert",
	"RevokeCertificate": "xkms.ca.revokeCert",
	"GenerateCRL":       "xkms.ca.generateCRL",
	"IsRevoked":         "xkms.ca.isRevoked",
}

// trustedCAToolName returns the MCP tool name for a CAService method and
// whether the method is wired over MCP.
func trustedCAToolName(method string) (string, bool) {
	name, ok := trustedCAMethodTools[method]
	return name, ok
}

// trustedCARequestTypes checks each method's go-xkms request type.
var trustedCARequestTypes = map[string]func(interface{}) bool{
	"GetCABundle":       func(r interface{}) bool { v, ok := r.(*xkmstransport.GetCABundleRequest); return ok && v != nil },
	"GetCACertificate":  func(r interface{}) bool { v, ok := r.(*xkmstransport.GetCACertificateRequest); return ok && v != nil },
	"SignCSR":           func(r interface{}) bool { v, ok := r.(*xkmstransport.SignCSRRequest); return ok && v != nil },
	"IssueCertificate":  func(r interface{}) bool { v, ok := r.(*xkmstransport.IssueCertificateRequest); return ok && v != nil },
	"RevokeCertificate": func(r interface{}) bool { v, ok := r.(*xkmstransport.RevokeCertificateRequest); return ok && v != nil },
	"GenerateCRL":       func(r interface{}) bool { v, ok := r.(*xkmstransport.GenerateCRLRequest); return ok && v != nil },
	"IsRevoked":         func(r interface{}) bool { v, ok := r.(*xkmstransport.IsRevokedRequest); return ok && v != nil },
}

// trustedCATransformRequest converts a go-xkms CA request into the xkms.ca.*
// tool arguments: the request's snake_case JSON tags are the argument names,
// except csr_pem, which the tool takes as the PEM text, not base64.
func trustedCATransformRequest(method string, req interface{}) (map[string]interface{}, error) {
	op := "mcp.trustedca." + method
	check, ok := trustedCARequestTypes[method]
	if !ok {
		return nil, errors.NewUnimplemented(op, "method not exposed by the CA MCP tool set")
	}
	if !check(req) {
		return nil, errors.NewInvalidInput(op, "invalid request type, expected the go-xkms "+method+" request")
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, op, "failed to encode request")
	}
	var args map[string]interface{}
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, op, "request is not a JSON object")
	}
	if r, ok := req.(*xkmstransport.SignCSRRequest); ok {
		args["csr_pem"] = string(r.CSRPEM)
	}
	return args, nil
}

// trustedCAResponseTypes builds each method's go-xkms response value.
var trustedCAResponseTypes = map[string]func() interface{}{
	"GetCABundle":       func() interface{} { return &xkmstransport.GetCABundleResponse{} },
	"GetCACertificate":  func() interface{} { return &xkmstransport.GetCACertificateResponse{} },
	"SignCSR":           func() interface{} { return &xkmstransport.SignCSRResponse{} },
	"IssueCertificate":  func() interface{} { return &xkmstransport.IssueCertificateResponse{} },
	"RevokeCertificate": func() interface{} { return &xkmstransport.RevokeCertificateResponse{} },
	"GenerateCRL":       func() interface{} { return &xkmstransport.GenerateCRLResponse{} },
	"IsRevoked":         func() interface{} { return &xkmstransport.IsRevokedResponse{} },
}

// trustedCATransformResponse decodes an xkms.ca.* tool result (the server's
// go-xkms response, JSON-encoded) into the go-xkms response type.
func trustedCATransformResponse(method string, result interface{}) (interface{}, error) {
	op := "mcp.trustedca." + method
	mk, ok := trustedCAResponseTypes[method]
	if !ok {
		return nil, errors.NewUnimplemented(op, "method not exposed by the CA MCP tool set")
	}
	out := mk()
	if result == nil {
		return out, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to re-encode tool result")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode tool result")
	}
	return out, nil
}
