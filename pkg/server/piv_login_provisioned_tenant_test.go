//go:build !integration

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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

	hosted := hostedXKMSPIVLoginModule(t, srv)

	status, body := postPIVLoginOverTLS(t, srv, moduleRouter(hosted), root, adminCert)

	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "access_token")

	// The bearer is what every later call presents: the tenant-scoped REST
	// routes resolve their tenant with tenantctx.Target from the Subject the
	// bearer validates to, so the login has to bind it to the admin's tenant.
	ctx := tenantBoundBearerContext(t, hosted, body)
	tenant, err := tenantctx.Target(ctx, "rest.IAM", "acme")
	require.NoError(t, err, "the tenant admin's bearer must be bound to its tenant")
	assert.Equal(t, "acme", tenant)
}

// TestPIVLogin_CertProvisionedTenantAdminBearerRefusedForOtherTenant is the
// isolation beside it: the bearer bound to "acme" names "other" in the URL
// and is still refused 403, with no fallback to the system tenant.
func TestPIVLogin_CertProvisionedTenantAdminBearerRefusedForOtherTenant(t *testing.T) {
	srv, components, root := newOperatorTLSIntegrationNode(t)
	adminCert := provisionCertModeTenant(t, components, "acme")
	hosted := hostedXKMSPIVLoginModule(t, srv)

	status, body := postPIVLoginOverTLS(t, srv, moduleRouter(hosted), root, adminCert)
	require.Equal(t, http.StatusOK, status, body)

	ctx := tenantBoundBearerContext(t, hosted, body)
	_, err := tenantctx.Target(ctx, "rest.IAM", "other")
	require.Error(t, err)
	var crossTenant *tenantctx.CrossTenantError
	assert.True(t, errors.As(err, &crossTenant), "want a cross-tenant refusal, got %v", err)
	var coded *qberrors.QBError
	if assert.True(t, errors.As(err, &coded), "the refusal is a coded error") {
		assert.Equal(t, qberrors.ErrForbidden, coded.Code, "the REST layer answers it 403")
	}
}

// hostedXKMSPIVLoginModule hosts the xkms PIV login module the way the node
// does, so its login route and its bearer validator are the production ones.
func hostedXKMSPIVLoginModule(t *testing.T, srv *Server) qrdbsdk.ServiceModule {
	t.Helper()
	hosted, err := srv.hostXKMSModule(newXKMSPIVLoginModule(t, srv))
	require.NoError(t, err)
	return hosted
}

// tenantBoundBearerContext validates the login response's access_token
// through the module's own bearer fallback, the validator qrdb's middleware
// consults, and binds the resulting Subject's tenant into a context exactly
// as the REST layer's injectPrincipal does.
func tenantBoundBearerContext(t *testing.T, hosted qrdbsdk.ServiceModule, loginBody string) context.Context {
	t.Helper()
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(loginBody), &tokens))
	require.NotEmpty(t, tokens.AccessToken)

	fallback, ok := hosted.(qrdbsdk.ModuleAuthFallback)
	require.True(t, ok, "the hosted xkms module validates its own bearers")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/acme/users", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	identity, err := fallback.AuthenticateHTTP(req)
	require.NoError(t, err)

	subject := auth.SubjectFromIdentity(identity)
	ctx := context.Background()
	if subject.TenantID != "" {
		ctx = tenantctx.WithTenant(ctx, subject.TenantID)
	}
	return ctx
}
