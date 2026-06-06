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

// dekServicePrefix is the service name prefix for DEK operations.
const dekServicePrefix = "quicbaas.DEK"

// isDEKService reports whether svc names the QuicBaaS DEK service.
func isDEKService(svc string) bool {
	return strings.HasPrefix(svc, dekServicePrefix) || svc == "DEK"
}

// dekResponseConverters maps DEK method names to their response converters for
// O(1) dispatch. The QUIC server marshals the canonical service.*Response
// structs (see pkg/api/quic/handler_kms.go DEK handlers), and the DEK SDK client
// accepts those canonical types directly (case *service.X).
var dekResponseConverters = map[string]kmsResponseConverter{
	"CreateDEK":               convertResponse[service.CreateDEKResponse],
	"GetDEK":                  convertResponse[service.GetDEKResponse],
	"ListDEKs":                convertResponse[service.ListDEKsResponse],
	"RotateDEK":               convertResponse[service.RotateDEKResponse],
	"DeleteDEK":               convertResponse[service.DeleteDEKResponse],
	"GetDEKStatus":            convertResponse[service.GetDEKStatusResponse],
	"SetDefaultDEK":           convertResponse[service.SetDefaultDEKResponse],
	"SetDEKSafety":            convertResponse[service.SetDEKSafetyResponse],
	"ClearDEKSafety":          convertResponse[service.ClearDEKSafetyResponse],
	"StartReencryption":       convertResponse[service.StartReencryptionResponse],
	"GetReencryptionProgress": convertResponse[service.GetReencryptionProgressResponse],
}

// decodeDEKResponse decodes raw wire bytes into the canonical SDK DEK type for
// the given method. Unknown methods fall back to a generic map decode.
func decodeDEKResponse(method string, data []byte) (interface{}, error) {
	return decodeServiceResponse("quic.decodeDEKResponse", dekResponseConverters, method, data)
}
