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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// invalidWebAuthnOverrideError is the refusal the tenant manager itself
// returns for a malformed per-tenant WebAuthn override (go-quicbaas#368).
func invalidWebAuthnOverrideError(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	tenants := iam.NewDefaultTenantManager(iam.NewMemoryStorage())
	_, err := tenants.CreateTenant(ctx, iam.CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	_, err = tenants.UpdateTenant(ctx, "acme", iam.UpdateTenantOptions{
		Settings: &iam.TenantSettings{WebAuthn: &iam.TenantWebAuthnConfig{RPOrigins: []string{"https://"}}},
	})
	require.Error(t, err)
	return err
}

// The gRPC TenantSettings message carries no WebAuthn override, but the
// tenant manager's refusal must still reach a gRPC caller as a client error,
// not as Internal, whichever write produced it.
func TestIAMServer_InvalidWebAuthnOverrideIsInvalidArgument(t *testing.T) {
	refusal := invalidWebAuthnOverrideError(t)
	ctx := context.Background()

	t.Run("CreateTenant", func(t *testing.T) {
		srv, svc := newTestIAMServer(t)
		svc.On("CreateTenant", ctx, mock.AnythingOfType("iam.CreateTenantOptions")).Return(nil, refusal)

		_, err := srv.CreateTenant(ctx, &iampb.CreateTenantRequest{Id: "acme", Name: "Acme"})

		assert.Equal(t, codes.InvalidArgument, status.Code(err), "got %v", err)
		assert.Contains(t, status.Convert(err).Message(), "settings.webauthn.rp_origins")
	})

	t.Run("UpdateTenant", func(t *testing.T) {
		srv, svc := newTestIAMServer(t)
		svc.On("GetTenant", ctx, mock.Anything).Return(&iam.Tenant{ID: "t1"}, nil)
		svc.On("UpdateTenant", ctx, mock.AnythingOfType("*iam.Tenant")).Return(nil, refusal)

		_, err := srv.UpdateTenant(ctx, &iampb.UpdateTenantRequest{Id: "acme", Name: "Acme"})

		assert.Equal(t, codes.InvalidArgument, status.Code(err), "got %v", err)
		assert.Contains(t, status.Convert(err).Message(), "settings.webauthn.rp_origins")
	})
}
