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
// Security().Auth().FIDO2() over the QUIC transport. The request and
// response payloads are the go-xkms transport types verbatim
// (go-xkms/sdk/go/transport); the only quicbaas addition on the wire is the
// tenant_id field injected into the JSON body from the request context
// (transport.TenantFromContext), the same enrichment the orchestrator verbs
// use, because QuicBaaS runs one relying party per tenant while xkms runs
// exactly one.

package quic

import (
	"encoding/json"
	"strings"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fido2ServicePrefix is the service name the SDK FIDO2 surface routes
// through. It matches the const used by the REST / MCP / embedded
// transports.
const fido2ServicePrefix = "quicbaas.FIDO2"

// isFIDO2Service reports whether svc names the QuicBaaS FIDO2 service.
func isFIDO2Service(svc string) bool {
	return strings.HasPrefix(svc, fido2ServicePrefix) || svc == "FIDO2"
}

// fido2ResponseDecoder decodes raw wire JSON for one method into the xkms
// transport type its caller expects.
type fido2ResponseDecoder func(data []byte) (interface{}, error)

// fido2ResponseDecoders maps method names to their decoders for O(1)
// dispatch.
var fido2ResponseDecoders = map[string]fido2ResponseDecoder{
	"BeginRegistration":    decodeFIDO2BeginRegistrationResponse,
	"FinishRegistration":   decodeFIDO2FinishRegistrationResponse,
	"BeginAuthentication":  decodeFIDO2BeginAuthenticationResponse,
	"FinishAuthentication": decodeFIDO2FinishAuthenticationResponse,
}

// decodeFIDO2Response decodes raw wire bytes into the xkms transport type
// for the given method. An empty payload returns nil. Unknown methods are
// rejected (every wired FIDO2 method has a decoder) to prevent silent
// mis-routing.
func decodeFIDO2Response(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := fido2ResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeFIDO2Response",
			"unknown fido2 method: "+method)
	}
	return decoder(data)
}

func decodeFIDO2BeginRegistrationResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.BeginRegistrationResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapFIDO2Decode("BeginRegistration", err)
	}
	return &resp, nil
}

func decodeFIDO2FinishRegistrationResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.FinishRegistrationResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapFIDO2Decode("FinishRegistration", err)
	}
	return &resp, nil
}

func decodeFIDO2BeginAuthenticationResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.BeginAuthenticationResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapFIDO2Decode("BeginAuthentication", err)
	}
	return &resp, nil
}

func decodeFIDO2FinishAuthenticationResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.FinishAuthenticationResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapFIDO2Decode("FinishAuthentication", err)
	}
	return &resp, nil
}

func wrapFIDO2Decode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeFIDO2Response", "failed to unmarshal "+method+" response")
}
