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

// secretsServicePrefix is the service name prefix for Secrets operations.
const secretsServicePrefix = "quicbaas.Secrets"

// isSecretsService reports whether svc names the QuicBaaS Secrets service.
func isSecretsService(svc string) bool {
	return strings.HasPrefix(svc, secretsServicePrefix) || svc == "Secrets"
}

// secretsResponseConverters maps Secrets method names to their response
// converters for O(1) dispatch. The QUIC server marshals the canonical
// service.*Response structs (see pkg/api/quic/handler_secrets.go).
//
// Only the versioning / rotation methods whose SDK client accepts the canonical
// service.* response directly (case *service.X) are converted here. Store, Get,
// List, Delete and Ensure return bespoke SDK client structs (Secret,
// StoreSecretResponse, ...) whose wire shape differs from the service type; they
// are deliberately left to the generic decode path until a verified conversion
// is wired (see report).
var secretsResponseConverters = map[string]kmsResponseConverter{
	"GetVersion":        convertResponse[service.GetSecretVersionResponse],
	"ListVersions":      convertResponse[service.ListSecretVersionsResponse],
	"Rotate":            convertResponse[service.RotateSecretResponse],
	"Rollback":          convertResponse[service.RollbackSecretResponse],
	"UpdateStatus":      convertResponse[service.UpdateSecretStatusResponse],
	"SetRotationPolicy": convertResponse[service.SetRotationPolicyResponse],
	"GetRotationPolicy": convertResponse[service.GetRotationPolicyResponse],
}

// decodeSecretsResponse decodes raw wire bytes into the canonical SDK Secrets
// type for the given method. Unknown methods fall back to a generic map decode.
func decodeSecretsResponse(method string, data []byte) (interface{}, error) {
	return decodeServiceResponse("quic.decodeSecretsResponse", secretsResponseConverters, method, data)
}
