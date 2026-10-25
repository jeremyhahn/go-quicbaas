//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// unreachableCeremonyClient satisfies transport.CeremonyClient for a real
// CeremonyService whose request validation must refuse before any client
// call; reaching a method panics on the nil embedded interface.
type unreachableCeremonyClient struct {
	transport.CeremonyClient
}

// secretCopyingCeremonyService copies the init secret during the call: the
// tool wipes the request's secret once the service returns.
type secretCopyingCeremonyService struct {
	*fakeCeremonyService
	gotSecret []byte
}

func (s *secretCopyingCeremonyService) CeremonyInitialize(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
	if err := req.Secret.WithBytes(ctx, func(b []byte) error {
		s.gotSecret = append([]byte(nil), b...)
		return nil
	}); err != nil {
		return nil, err
	}
	return s.fakeCeremonyService.CeremonyInitialize(ctx, req)
}

// TestCeremonyInitializeTool_ForwardsAdminSelector proves every admin
// credential selector argument reaches the service request unchanged.
func TestCeremonyInitializeTool_ForwardsAdminSelector(t *testing.T) {
	svc := &secretCopyingCeremonyService{
		fakeCeremonyService: &fakeCeremonyService{initInfo: &service.CeremonyInitInfo{State: "READY"}},
	}
	r := newCeremonyTestRegistry(t, svc)

	_, err := r.ExecuteTool(context.Background(), "qrdb_ceremony_initialize", map[string]interface{}{
		"so_pin":              "so",
		"admin_csr_pem":       "csr",
		"admin_cert_pem":      "cert",
		"admin_cert_pool_pem": "pool",
		"admin_generate":      true,
		"secret":              "secret",
	})
	require.NoError(t, err)

	got := svc.gotInitReq
	require.NotNil(t, got)
	assert.Equal(t, "csr", got.AdminCSRPEM)
	assert.Equal(t, "cert", got.AdminCertPEM)
	assert.Equal(t, "pool", got.AdminCertPoolPEM)
	assert.True(t, got.AdminGenerate)
	assert.Equal(t, []byte("secret"), svc.gotSecret)
}

// TestCeremonyInitializeTool_NoAdminSelector proves a call without an admin
// credential selector comes back as the MCP invalid-input tool error from the
// real service's validation, before any go-qrdb client call.
func TestCeremonyInitializeTool_NoAdminSelector(t *testing.T) {
	r := newCeremonyTestRegistry(t, service.NewCeremonyService(&unreachableCeremonyClient{}))

	_, err := r.ExecuteTool(context.Background(), "qrdb_ceremony_initialize", map[string]interface{}{
		"so_pin": "so",
	})
	require.Error(t, err)
	var missing *service.CeremonyAdminSelectorMissingError
	assert.ErrorAs(t, err, &missing)
	assert.Equal(t, toolErrorCodeOf(qberrors.ErrInvalidInput), toolErrorCode(err))
}
