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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// kmsServicePrefix is the service name prefix for KMS operations.
const kmsServicePrefix = "quicbaas.KMS"

// isKMSService reports whether svc names the QuicBaaS KMS service.
func isKMSService(svc string) bool {
	return strings.HasPrefix(svc, kmsServicePrefix) || svc == "KMS"
}

// kmsResponseConverter decodes raw wire JSON into a typed SDK value.
type kmsResponseConverter func(data []byte) (interface{}, error)

// kmsResponseConverters maps KMS method names to their response converters for
// O(1) dispatch. The QUIC server marshals the canonical service.*Response
// structs with encoding/json (see pkg/api/quic/handler_kms.go), and the KMS SDK
// client accepts those canonical types directly (case *service.X), so decoding
// the same bytes back into the canonical type round-trips exactly — matching
// what the embedded/gRPC transports return.
//
// Only methods that the server reaches over QUIC (i.e. have a wire opcode) and
// that the KMS client converts from service.* are included.
var kmsResponseConverters = map[string]kmsResponseConverter{
	"GenerateKey":       convertResponse[service.GenerateKeyResponse],
	"GetKey":            convertResponse[service.GetKeyResponse],
	"ListKeys":          convertResponse[service.ListKeysResponse],
	"RotateKey":         convertResponse[service.RotateKeyResponse],
	"DeleteKey":         convertResponse[service.DeleteKeyResponse],
	"ImportKey":         convertResponse[service.ImportKeyResponse],
	"ExportKey":         convertResponse[service.ExportKeyResponse],
	"UnwrapKey":         convertResponse[service.UnwrapKeyResponse],
	"GetKeyMetadata":    convertResponse[service.GetKeyMetadataResponse],
	"UpdateKeyMetadata": convertResponse[service.UpdateKeyMetadataResponse],
	"SetKeyPolicy":      convertResponse[service.SetKeyPolicyResponse],
	"GetKeyPolicy":      convertResponse[service.GetKeyPolicyResponse],
	"BackendStatus":     convertResponse[service.BackendStatusResponse],
	"ListKeyVersions":   convertResponse[service.ListKeyVersionsResponse],
}

// decodeKMSResponse decodes raw wire bytes into the canonical SDK KMS type for
// the given method. Unknown methods fall back to a generic map decode.
func decodeKMSResponse(method string, data []byte) (interface{}, error) {
	return decodeServiceResponse("quic.decodeKMSResponse", kmsResponseConverters, method, data)
}

// convertResponse decodes JSON into a *T and returns it. It is the generic
// equivalent of the per-type converters used by the backup handler, eliminating
// the repetitive single-method boilerplate for services whose client accepts
// the canonical service.* response types directly.
func convertResponse[T any](data []byte) (interface{}, error) {
	var resp T
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// decodeServiceResponse is the shared decode loop for service handlers whose
// converters map a method name to a canonical service.* response. Empty payload
// returns nil; an unknown method falls back to a generic map decode so callers
// that do not yet have a typed converter still receive structured data instead
// of an error.
func decodeServiceResponse(op string, converters map[string]kmsResponseConverter, method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}

	converter, ok := converters[method]
	if !ok {
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode response")
		}
		return decoded, nil
	}

	return converter(data)
}
