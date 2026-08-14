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

// dynamicsecrets_handler.go routes the quicbaas.DynamicSecrets service
// (Security().DynamicSecrets()) over the QUIC wire.
//
// The eight verbs ride the server's dedicated top-level opcodes 0x64-0x6B
// (pkg/api/quic/protocol.go OpDynamicSecrets*), not an OpOrchestrator sub-op,
// so no request transform is needed: the leaf client/dynamicsecrets request
// structs carry the same snake_case JSON tags as the pkg/service request types
// the server unmarshals. Only the response needs a typed decode so callers
// receive the same leaf types every other transport returns.

package quic

import (
	"strings"

	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

// dynamicSecretsServicePrefix is the service name prefix for dynamic secrets
// operations.
const dynamicSecretsServicePrefix = "quicbaas.DynamicSecrets"

// isDynamicSecretsService reports whether svc names the QuicBaaS dynamic
// secrets service.
func isDynamicSecretsService(svc string) bool {
	return strings.HasPrefix(svc, dynamicSecretsServicePrefix) || svc == "DynamicSecrets"
}

// dynamicSecretsResponseConverters maps method names to their response
// converters for O(1) dispatch.
//
// The QUIC server marshals the canonical pkg/service response structs (see
// pkg/api/quic/handler_dynamic_secrets.go), whose JSON tags are field-for-field
// identical to the leaf SDK response types — credentials.Role matches
// dynamicsecrets.Role, lease.Lease matches dynamicsecrets.Lease, and durations
// are nanosecond integers on both sides. Decoding straight into the SDK type is
// therefore lossless and needs no intermediate wire struct.
var dynamicSecretsResponseConverters = map[string]kmsResponseConverter{
	"CreateRole":         convertResponse[dsclient.CreateRoleResponse],
	"GetRole":            convertResponse[dsclient.GetRoleResponse],
	"ListRoles":          convertResponse[dsclient.ListRolesResponse],
	"DeleteRole":         convertResponse[dsclient.DeleteRoleResponse],
	"GenerateCredential": convertResponse[dsclient.GenerateCredentialResponse],
	"RenewLease":         convertResponse[dsclient.RenewLeaseResponse],
	"RevokeLease":        convertResponse[dsclient.RevokeLeaseResponse],
	"ListLeases":         convertResponse[dsclient.ListLeasesResponse],
}

// decodeDynamicSecretsResponse decodes raw wire bytes into the leaf SDK
// dynamic secrets type for the given method. Unknown methods fall back to a
// generic map decode.
func decodeDynamicSecretsResponse(method string, data []byte) (interface{}, error) {
	return decodeServiceResponse("quic.decodeDynamicSecretsResponse",
		dynamicSecretsResponseConverters, method, data)
}
