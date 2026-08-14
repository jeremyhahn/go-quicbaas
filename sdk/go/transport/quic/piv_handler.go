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
// Security().Auth().PIV() over the QUIC transport. The request and response
// payloads are the go-xkms transport types verbatim
// (go-xkms/sdk/go/transport), so encoding is a plain JSON round-trip: the
// SDK request struct encodes to exactly the wire shape the server's
// handler_piv.go decodes, and the server's response bytes unmarshal straight
// into the xkms response type the SDK caller asserts on.

package quic

import (
	"encoding/json"
	"strings"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// pivServicePrefix is the service name the SDK PIV surface routes through.
// It matches the const used by the REST / MCP / embedded transports.
const pivServicePrefix = "quicbaas.PIV"

// isPIVService reports whether svc names the QuicBaaS PIV service.
func isPIVService(svc string) bool {
	return strings.HasPrefix(svc, pivServicePrefix) || svc == "PIV"
}

// pivResponseDecoder decodes raw wire JSON for one method into the xkms
// transport type its caller expects.
type pivResponseDecoder func(data []byte) (interface{}, error)

// pivResponseDecoders maps method names to their decoders for O(1) dispatch.
// StorePIVCertificate, DeletePIVCertificate and ImportPIVCertificate are
// void verbs: the server returns an empty payload, which the transport loop
// resolves to (nil, nil) before reaching a decoder.
var pivResponseDecoders = map[string]pivResponseDecoder{
	"ListPIVSlots":         decodePIVListSlotsResponse,
	"GetPIVCertificate":    decodePIVGetCertificateResponse,
	"ExportPIVCertificate": decodePIVGetCertificateResponse,
	"GeneratePIVKey":       decodePIVGenerateKeyResponse,
	"GeneratePIVCSR":       decodePIVGenerateCSRResponse,
	"StorePIVCertificate":  decodePIVVoidResponse,
	"DeletePIVCertificate": decodePIVVoidResponse,
	"ImportPIVCertificate": decodePIVVoidResponse,
}

// decodePIVResponse decodes raw wire bytes into the xkms transport type for
// the given method. An empty payload returns nil. Unknown methods are
// rejected (every wired PIV method has a decoder) to prevent silent
// mis-routing.
func decodePIVResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := pivResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodePIVResponse",
			"unknown piv method: "+method)
	}
	return decoder(data)
}

func decodePIVListSlotsResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.ListPIVSlotsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapPIVDecode("ListPIVSlots", err)
	}
	return &resp, nil
}

func decodePIVGetCertificateResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.GetPIVCertificateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapPIVDecode("GetPIVCertificate", err)
	}
	return &resp, nil
}

func decodePIVGenerateKeyResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.GeneratePIVKeyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapPIVDecode("GeneratePIVKey", err)
	}
	return &resp, nil
}

func decodePIVGenerateCSRResponse(data []byte) (interface{}, error) {
	var resp xkmstransport.GeneratePIVCSRResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapPIVDecode("GeneratePIVCSR", err)
	}
	return &resp, nil
}

// decodePIVVoidResponse handles a void verb whose server nonetheless
// returned a body (e.g. an empty JSON object). The verb has no response
// type, so the payload is discarded after validation.
func decodePIVVoidResponse(data []byte) (interface{}, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, wrapPIVDecode("void", err)
	}
	return nil, nil
}

func wrapPIVDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodePIVResponse", "failed to unmarshal "+method+" response")
}
