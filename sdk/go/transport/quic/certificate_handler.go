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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// certificateServicePrefix is the service name prefix for Certificate operations.
const certificateServicePrefix = "quicbaas.Certificate"

// isCertificateService reports whether svc names the QuicBaaS Certificate service.
func isCertificateService(svc string) bool {
	return strings.HasPrefix(svc, certificateServicePrefix) || svc == "Certificate"
}

// =============================================================================
// Response decoding (server service.*Response wire shape -> SDK cert types)
// =============================================================================
//
// The Certificate SDK client (sdk/go/client/certificate) asserts on bespoke
// certificate.*Response types. The QUIC server's handler_certificate.go encodes
// the canonical pkg/service.*Response structs, whose JSON tags (snake_case) are
// identical to the SDK certificate response tags. Unmarshalling the server bytes
// directly into the SDK type therefore round-trips field-for-field, matching the
// REST/MCP transports.
//
// Only the six verbs the server wires over QUIC are decoded here
// (IssueCertificate / SignCSR / RevokeCertificate / VerifyCertificate /
// GetCACertificate / GetTLSConfig). The other client methods (GetCABundle,
// IsInitialized, GenerateCRL, IsRevoked, CAInfo, ListCertificates) have no QUIC
// opcode and never reach this decoder — they fail at MethodToOpcode resolution.

// certificateResponseDecoder decodes raw wire JSON for one method into the SDK
// certificate type its caller expects.
type certificateResponseDecoder func(data []byte) (interface{}, error)

// certificateResponseDecoders maps method names to their decoders for O(1)
// dispatch.
var certificateResponseDecoders = map[string]certificateResponseDecoder{
	"IssueCertificate":  decodeIssueCertificateResponse,
	"SignCSR":           decodeSignCSRResponse,
	"RevokeCertificate": decodeRevokeCertificateResponse,
	"VerifyCertificate": decodeVerifyCertificateResponse,
	"GetCACertificate":  decodeGetCACertificateResponse,
	"GetTLSConfig":      decodeGetTLSConfigResponse,

	// Certificate extensions — Security().CertificateExt(). These ride the
	// OpOrchestrator dispatch byte with a sub-op byte (see protocol.go
	// SubOpCertExt*); the server encodes the canonical pkg/service response
	// structs, whose snake_case JSON tags match the leaf SDK types.
	"CrossSign":           decodeCrossSignResponse,
	"Chain":               decodeCertificateChainResponse,
	"IssueDeviceIdentity": decodeDeviceIdentityResponse,
}

// decodeCertificateResponse decodes raw wire bytes into the SDK certificate type
// for the given method. An empty payload returns nil. Unknown methods are
// rejected (every wired certificate method has a decoder) to prevent silent
// mis-routing.
func decodeCertificateResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := certificateResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeCertificateResponse",
			"unknown certificate method: "+method)
	}
	return decoder(data)
}

func decodeIssueCertificateResponse(data []byte) (interface{}, error) {
	var resp certificate.IssueCertificateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("IssueCertificate", err)
	}
	return &resp, nil
}

func decodeSignCSRResponse(data []byte) (interface{}, error) {
	var resp certificate.SignCSRResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("SignCSR", err)
	}
	return &resp, nil
}

func decodeRevokeCertificateResponse(data []byte) (interface{}, error) {
	var resp certificate.RevokeCertificateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("RevokeCertificate", err)
	}
	return &resp, nil
}

func decodeVerifyCertificateResponse(data []byte) (interface{}, error) {
	var resp certificate.VerifyCertificateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("VerifyCertificate", err)
	}
	return &resp, nil
}

func decodeGetCACertificateResponse(data []byte) (interface{}, error) {
	var resp certificate.GetCACertificateResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("GetCACertificate", err)
	}
	return &resp, nil
}

// decodeGetTLSConfigResponse maps the server's GetTLSConfig acknowledgement to
// the SDK GetTLSConfigResponse. The server cannot serialize *tls.Config over the
// wire (it returns only {success, message}), so the SDK response carries a nil
// Config; the caller must obtain the live *tls.Config from the service layer.
// The body is validated to decode so a malformed payload still surfaces.
func decodeGetTLSConfigResponse(data []byte) (interface{}, error) {
	var ack struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &ack); err != nil {
		return nil, wrapCertificateDecode("GetTLSConfig", err)
	}
	return &certificate.GetTLSConfigResponse{}, nil
}

func wrapCertificateDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeCertificateResponse", "failed to unmarshal "+method+" response")
}

func decodeCrossSignResponse(data []byte) (interface{}, error) {
	var resp certificate.CrossSignResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("CrossSign", err)
	}
	return &resp, nil
}

func decodeCertificateChainResponse(data []byte) (interface{}, error) {
	var resp certificate.ChainResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("Chain", err)
	}
	return &resp, nil
}

func decodeDeviceIdentityResponse(data []byte) (interface{}, error) {
	var resp certificate.DeviceIdentityResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapCertificateDecode("IssueDeviceIdentity", err)
	}
	return &resp, nil
}
