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

package quic

import (
	"encoding/json"
	"strings"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// attestationServicePrefix is the service name prefix for Attestation
// operations.
const attestationServicePrefix = attestationclient.ServiceName

// isAttestationService reports whether svc names the QuicBaaS Attestation
// service.
func isAttestationService(svc string) bool {
	return strings.HasPrefix(svc, attestationServicePrefix) || svc == "Attestation"
}

// =============================================================================
// Response decoding (server service.*Response wire shape -> SDK types)
// =============================================================================
//
// The Attestation SDK surface asserts on the leaf client/attestation types.
// The QUIC server's handler_attestation.go encodes the canonical
// pkg/service.*Response structs, whose snake_case JSON tags match the leaf
// types field-for-field, so unmarshalling the server bytes directly into the
// leaf type round-trips — the same arrangement Trust and the certificate
// extensions use.
//
// AttestedAt is the one field that is not a literal match: the service struct
// carries a time.Time and the leaf type a string. Those are wire-compatible
// because time.Time marshals to an RFC 3339 string, which unmarshals straight
// into the leaf's string field.

// attestationResponseDecoder decodes raw wire JSON for one method into the
// leaf attestation type its caller expects.
type attestationResponseDecoder func(data []byte) (interface{}, error)

// attestationResponseDecoders maps method names to their decoders for O(1)
// dispatch.
var attestationResponseDecoders = map[string]attestationResponseDecoder{
	"Verify":          decodeAttestationVerifyResponse,
	"VerifyChain":     decodeAttestationVerifyChainResponse,
	"AttestContainer": decodeAttestContainerResponse,
}

// decodeAttestationResponse decodes raw wire bytes into the leaf attestation
// type for the given method. An empty payload returns nil. Unknown methods are
// rejected (every wired attestation method has a decoder) to prevent silent
// mis-routing.
func decodeAttestationResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := attestationResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeAttestationResponse",
			"unknown attestation method: "+method)
	}
	return decoder(data)
}

func decodeAttestationVerifyResponse(data []byte) (interface{}, error) {
	var resp attestationclient.VerifyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapAttestationDecode("Verify", err)
	}
	return &resp, nil
}

func decodeAttestationVerifyChainResponse(data []byte) (interface{}, error) {
	var resp attestationclient.VerifyChainResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapAttestationDecode("VerifyChain", err)
	}
	return &resp, nil
}

func decodeAttestContainerResponse(data []byte) (interface{}, error) {
	var resp attestationclient.AttestContainerResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapAttestationDecode("AttestContainer", err)
	}
	return &resp, nil
}

func wrapAttestationDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeAttestationResponse", "failed to unmarshal "+method+" response")
}
