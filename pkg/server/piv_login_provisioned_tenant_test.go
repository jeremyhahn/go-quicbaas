//go:build !integration

package server

import (
	"net/http"
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
func TestPIVLogin_CertProvisionedTenantAdminLogsIn(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")

	status, body := postPIVLoginOverTLS(t, srv, xkmsPIVLoginRouter(t, srv), root, adminCert)

	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "access_token")
}
