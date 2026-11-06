//go:build !integration

package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPIVLogin_CertProvisionedTenantAdminLogsIn drives what the integration
// helper ProvisionTenant does (test/integration/helpers/tenant_provision.go):
// provision a tenant in cert mode from a CSR whose subject is CommonName
// "admin" with no SAN, then present the issued certificate to the PIV login
// the composed node serves. No test middleware binds the tenant: the server
// has to place the login in the tenant from the certificate itself (#1698).
// The token it mints must name that tenant, since every data-plane call made
// with the bearer is bound to the tenant the token carries (#2212).
func TestPIVLogin_CertProvisionedTenantAdminLogsIn(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")

	status, body := postPIVLoginOverTLS(t, srv, xkmsPIVLoginRouter(t, srv), root, adminCert)

	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "access_token")

	var resp map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &resp), body)
	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok, "access_token is not a string: %v", resp["access_token"])
	require.NotEmpty(t, accessToken)

	segments := strings.Split(accessToken, ".")
	require.Len(t, segments, 3, "access_token is not a JWT")
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))

	names := make([]string, 0, len(claims))
	for name := range claims {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "cnf" || name == "jwk" || strings.Contains(strings.ToLower(name), "key") {
			t.Logf("claim %s: <key material omitted>", name)
			continue
		}
		t.Logf("claim %s: %v", name, claims[name])
	}

	var tenant any
	found := false
	for _, key := range []string{"tenant_id", "tid", "tenant"} {
		if value, present := claims[key]; present {
			tenant, found = value, true
			break
		}
	}
	require.True(t, found, "token carries no tenant claim (tenant_id, tid, tenant); claims found: %v", names)
	assert.Equal(t, "acme", tenant, "token tenant claim; claims found: %v", names)
}
