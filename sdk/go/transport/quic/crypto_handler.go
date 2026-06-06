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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// cryptoServicePrefix is the service name prefix for Crypto (transit) operations.
const cryptoServicePrefix = "quicbaas.Crypto"

// isCryptoService reports whether svc names the QuicBaaS Crypto service.
func isCryptoService(svc string) bool {
	return strings.HasPrefix(svc, cryptoServicePrefix) || svc == "Crypto"
}

// cryptoResponseConverters maps Crypto method names to their response converters
// for O(1) dispatch. The QUIC server marshals the canonical service.*Response
// structs (see pkg/api/quic/handler_crypto.go), and the Crypto SDK client
// accepts those canonical types directly (case *service.X).
//
// Only methods reachable over QUIC (with a wire opcode in pkg/api/quic) are
// included. EncryptBatch, DecryptBatch and GenerateWrappedDataKey have no QUIC
// opcode and are therefore intentionally absent.
var cryptoResponseConverters = map[string]kmsResponseConverter{
	"Encrypt":         convertResponse[service.EncryptResponse],
	"Decrypt":         convertResponse[service.DecryptResponse],
	"Sign":            convertResponse[service.SignResponse],
	"Verify":          convertResponse[service.VerifyResponse],
	"Rewrap":          convertResponse[service.RewrapResponse],
	"GenerateDataKey": convertResponse[service.GenerateDataKeyResponse],
	"GenerateHMAC":    convertResponse[service.HMACResponse],
	"VerifyHMAC":      convertResponse[service.VerifyHMACResponse],
	"GenerateRandom":  convertResponse[service.GenerateRandomResponse],
	"Hash":            convertResponse[service.HashResponse],
}

// decodeCryptoResponse decodes raw wire bytes into the canonical SDK Crypto type
// for the given method. Unknown methods fall back to a generic map decode.
func decodeCryptoResponse(method string, data []byte) (interface{}, error) {
	return decodeServiceResponse("quic.decodeCryptoResponse", cryptoResponseConverters, method, data)
}
