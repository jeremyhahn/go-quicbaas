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

	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// trustServicePrefix is the service name prefix for Trust operations.
const trustServicePrefix = "quicbaas.Trust"

// isTrustService reports whether svc names the QuicBaaS Trust service.
func isTrustService(svc string) bool {
	return strings.HasPrefix(svc, trustServicePrefix) || svc == "Trust"
}

// =============================================================================
// Response decoding (server service.*Response wire shape -> SDK trust types)
// =============================================================================
//
// The Trust SDK surface (trustServiceImpl in sdk/go) asserts on the leaf
// client/trust types. The QUIC server's handler_trust.go encodes the
// canonical pkg/service.*Response structs, whose JSON tags (snake_case)
// match the leaf trust types field-for-field, so unmarshalling the server
// bytes directly into the leaf type round-trips, matching the REST/MCP/
// embedded transports.

// trustResponseDecoder decodes raw wire JSON for one method into the leaf
// trust type its caller expects.
type trustResponseDecoder func(data []byte) (interface{}, error)

// trustResponseDecoders maps method names to their decoders for O(1)
// dispatch.
var trustResponseDecoders = map[string]trustResponseDecoder{
	"CrossSignedRootPool": decodeCrossSignedRootPoolResponse,
	"AutoImportAIA":       decodeAutoImportAIAResponse,
}

// decodeTrustResponse decodes raw wire bytes into the leaf trust type for
// the given method. An empty payload returns nil. Unknown methods are
// rejected (every wired trust method has a decoder) to prevent silent
// mis-routing.
func decodeTrustResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := trustResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeTrustResponse",
			"unknown trust method: "+method)
	}
	return decoder(data)
}

func decodeCrossSignedRootPoolResponse(data []byte) (interface{}, error) {
	var resp trustclient.CrossSignedRootPoolResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapTrustDecode("CrossSignedRootPool", err)
	}
	return &resp, nil
}

func decodeAutoImportAIAResponse(data []byte) (interface{}, error) {
	var resp trustclient.AutoImportAIAResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapTrustDecode("AutoImportAIA", err)
	}
	return &resp, nil
}

func wrapTrustDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeTrustResponse", "failed to unmarshal "+method+" response")
}
