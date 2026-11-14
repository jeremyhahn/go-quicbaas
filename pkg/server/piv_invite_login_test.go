//go:build !integration

package server

// The PIV invite activation path end to end in process: a pending user whose
// policy permits only piv activates through the invite, then logs in with the
// activated certificate through go-xkms's PIV login route as the server mounts
// it. The login checks the policy for mtls, so this proves the activation
// records mtls beside piv (go-quicbaas#1739, CA Decision 103).

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestPIVInvite_ActivatedCertLogsInThroughXKMS(t *testing.T) {
	ctx := context.Background()
	root, leafPEM := newPIVRootAndLeaf(t)
	caPath := filepath.Join(t.TempDir(), "piv-ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}), 0o600))

	srv, components, _ := newIntegrationLikeNode(t, func(s *Server) {
		// The PIV CA is in the one client trust-anchor set,
		// security.tls_ca_file (#1321).
		s.config.Security.TLSCAFile = caPath
		require.NoError(t, s.loadOperatorClientTrustAnchors())
	})
	activator := components.GetPIVActivator()
	require.NotNil(t, activator, "PIV activation must be enabled by security.tls_ca_file")

	user, err := components.GetIdentityStore().CreateUser(ctx, qrdbsdk.SystemTenantID, iam.CreateUserOptions{
		Email:       "piv-invitee@example.com",
		Username:    "piv-invitee",
		AuthMethods: []string{iam.AuthMethodPIV},
	})
	require.NoError(t, err)
	token, _, err := components.GetInviteStore().Create(ctx, qrdbsdk.SystemTenantID, user.ID, time.Hour)
	require.NoError(t, err)

	require.NoError(t, activator.Activate(ctx, qrdbsdk.SystemTenantID, token, leafPEM))

	activated, err := components.GetIdentityStore().GetUser(ctx, qrdbsdk.SystemTenantID, user.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{iam.AuthMethodPIV, iam.AuthMethodMTLS}, activated.AuthMethods)

	block, _ := pem.Decode(leafPEM)
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	rec := postXKMSPIVLogin(xkmsPIVLoginRouter(t, srv), leaf)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "access_token")
}
