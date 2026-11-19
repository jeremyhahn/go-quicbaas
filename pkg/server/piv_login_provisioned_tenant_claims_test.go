//go:build !integration

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

package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// accessTokenClaims decodes the claim set of the access token a login
// response carries. The signature is not checked: the test asks only what
// the issuer wrote into the token.
func accessTokenClaims(t *testing.T, body string) map[string]any {
	t.Helper()
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp), body)
	parts := strings.Split(resp.AccessToken, ".")
	require.Len(t, parts, 3, "the access token is a JWS")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}

// TestPIVLogin_CertProvisionedTenantAdminTokenCarriesTenant checks the bearer
// the integration helper ProvisionTenant gets back: the tenant admin's PIV
// login through the composed xkms module must mint a token whose tenant_id
// is the provisioned tenant. A token without it reaches tenant routes with
// no tenant bound ("tenantctx: ... no tenant is bound to the request").
func TestPIVLogin_CertProvisionedTenantAdminTokenCarriesTenant(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")

	status, body := postPIVLoginOverTLS(t, srv, xkmsPIVLoginRouter(t, srv), root, adminCert)

	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "acme", accessTokenClaims(t, body)["tenant_id"],
		"the tenant admin's access token names its tenant")
}
