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

package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/webauthnrp"
)

// invalidWebAuthnOverrides are overrides the global relying party would be
// refused at boot for, each with the settings field the refusal must name.
var invalidWebAuthnOverrides = []struct {
	name     string
	override TenantWebAuthnConfig
	field    string
}{
	{
		name:     "RP ID is a URL",
		override: TenantWebAuthnConfig{RPID: "https://tenant.example.com"},
		field:    tenantWebAuthnFieldRPID,
	},
	{
		name:     "RP ID carries a port",
		override: TenantWebAuthnConfig{RPID: "tenant.example.com:8443"},
		field:    tenantWebAuthnFieldRPID,
	},
	{
		name:     "origin has no host",
		override: TenantWebAuthnConfig{RPOrigins: []string{"https://tenant.example.com", "https://"}},
		field:    tenantWebAuthnFieldRPOrigins,
	},
	{
		name:     "origin is empty",
		override: TenantWebAuthnConfig{RPOrigins: []string{""}},
		field:    tenantWebAuthnFieldRPOrigins,
	},
}

// requireInvalidWebAuthnOverride asserts err is the coded InvalidInput
// refusal of a tenant WebAuthn override naming field.
func requireInvalidWebAuthnOverride(t *testing.T, err error, field string) {
	t.Helper()
	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr), "want a coded *QBError, got %T: %v", err, err)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	var overrideErr *TenantWebAuthnOverrideError
	require.True(t, errors.As(err, &overrideErr), "want *TenantWebAuthnOverrideError, got %v", err)
	assert.Equal(t, field, overrideErr.Field)
}

func goodWebAuthnOverride() *TenantWebAuthnConfig {
	return &TenantWebAuthnConfig{
		RPID:          "tenant.example.com",
		RPDisplayName: "Tenant",
		RPOrigins:     []string{"https://tenant.example.com"},
	}
}

func TestTenantWebAuthnConfig_Validate_AcceptsWellFormedAndInheritingOverrides(t *testing.T) {
	var none *TenantWebAuthnConfig
	assert.NoError(t, none.Validate(), "a nil override defers to the global relying party")
	assert.NoError(t, (&TenantWebAuthnConfig{}).Validate(), "empty fields inherit the global values")
	assert.NoError(t, (&TenantWebAuthnConfig{RPDisplayName: "Only a name"}).Validate())
	assert.NoError(t, goodWebAuthnOverride().Validate())
}

func TestTenantWebAuthnConfig_Validate_RefusesWhatTheGlobalConfigRefuses(t *testing.T) {
	for _, tt := range invalidWebAuthnOverrides {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.override.Validate()
			var overrideErr *TenantWebAuthnOverrideError
			require.True(t, errors.As(err, &overrideErr), "got %v", err)
			assert.Equal(t, tt.field, overrideErr.Field)
			assert.Contains(t, err.Error(), tt.field)

			var rpIDErr *webauthnrp.RPIDError
			var originErr *webauthnrp.OriginError
			assert.True(t, errors.As(err, &rpIDErr) || errors.As(err, &originErr),
				"the shared webauthnrp rule must stay reachable: %v", err)
		})
	}
}

func TestCreateTenant_AcceptsAValidWebAuthnOverride(t *testing.T) {
	ctx := context.Background()
	mgr := NewDefaultTenantManager(NewMemoryStorage())

	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
		ID:       "acme",
		Name:     "Acme",
		Settings: &TenantSettings{WebAuthn: goodWebAuthnOverride()},
	})
	require.NoError(t, err)

	stored, err := mgr.GetTenant(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, goodWebAuthnOverride(), stored.Settings.WebAuthn)
}

func TestCreateTenant_RefusesAnInvalidWebAuthnOverride(t *testing.T) {
	for _, tt := range invalidWebAuthnOverrides {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mgr := NewDefaultTenantManager(NewMemoryStorage())
			override := tt.override

			_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
				ID:       "acme",
				Name:     "Acme",
				Settings: &TenantSettings{WebAuthn: &override},
			})
			requireInvalidWebAuthnOverride(t, err, tt.field)

			_, err = mgr.GetTenant(ctx, "acme")
			assert.True(t, dberrors.IsNotFound(err), "a refused create must store nothing, got %v", err)
		})
	}
}

func TestUpdateTenant_RefusesAnInvalidWebAuthnOverride(t *testing.T) {
	for _, tt := range invalidWebAuthnOverrides {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mgr := NewDefaultTenantManager(NewMemoryStorage())
			_, err := mgr.CreateTenant(ctx, CreateTenantOptions{
				ID:       "acme",
				Name:     "Acme",
				Settings: &TenantSettings{WebAuthn: goodWebAuthnOverride()},
			})
			require.NoError(t, err)
			observer := &countingSettingsObserver{}
			mgr.AddSettingsObserver(observer)
			override := tt.override

			_, err = mgr.UpdateTenant(ctx, "acme", UpdateTenantOptions{
				Settings: &TenantSettings{WebAuthn: &override},
			})
			requireInvalidWebAuthnOverride(t, err, tt.field)

			stored, err := mgr.GetTenant(ctx, "acme")
			require.NoError(t, err)
			assert.Equal(t, goodWebAuthnOverride(), stored.Settings.WebAuthn,
				"a refused update must leave the stored override alone")
			assert.Zero(t, observer.calls, "a refused update must not evict cached relying parties")
		})
	}
}

// TestUpdateTenant_StoredOverrideIsTheOneTheFactoryUses walks a valid
// override from the tenant write to the relying-party config the WebAuthn
// factory resolves for that tenant.
func TestUpdateTenant_StoredOverrideIsTheOneTheFactoryUses(t *testing.T) {
	ctx := context.Background()
	mgr := NewDefaultTenantManager(NewMemoryStorage())
	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: "acme", Name: "Acme"})
	require.NoError(t, err)

	_, err = mgr.UpdateTenant(ctx, "acme", UpdateTenantOptions{
		Settings: &TenantSettings{WebAuthn: goodWebAuthnOverride()},
	})
	require.NoError(t, err)

	f := newTestWebAuthnFactory(t, NewTenantManagerWebAuthnSettings(mgr))
	cfg, err := f.resolveConfig(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, "tenant.example.com", cfg.RPID)
	assert.Equal(t, "Tenant", cfg.RPDisplayName)
	assert.Equal(t, []string{"https://tenant.example.com"}, cfg.RPOrigins)
}

func TestWebAuthnFactory_BuildFailsClosedOnAStoredInvalidOverride(t *testing.T) {
	for _, tt := range invalidWebAuthnOverrides {
		t.Run(tt.name, func(t *testing.T) {
			// An override stored before writes were validated reaches the
			// factory unchecked; only the build stands between it and a
			// relying party that can never verify a ceremony.
			override := tt.override
			f := newTestWebAuthnFactory(t, stubWebAuthnSettings{override: &override})

			svc, err := f.ServiceFor(context.Background(), "acme")
			assert.Nil(t, svc)
			var rpErr *WebAuthnTenantRPError
			require.True(t, errors.As(err, &rpErr), "want *WebAuthnTenantRPError, got %T: %v", err, err)
			assert.Equal(t, "acme", rpErr.TenantID)
			var overrideErr *TenantWebAuthnOverrideError
			require.True(t, errors.As(err, &overrideErr))
			assert.Equal(t, tt.field, overrideErr.Field)
			assert.Contains(t, err.Error(), "acme")

			_, cached := f.services.Load("acme")
			assert.False(t, cached, "a refused build must cache nothing")
		})
	}
}

type countingSettingsObserver struct {
	calls int
}

func (o *countingSettingsObserver) OnTenantSettingsChanged(string) { o.calls++ }
