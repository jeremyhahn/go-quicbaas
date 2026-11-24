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

package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A tenant's grace job disables only that tenant's own key versions,
// whatever its payload names: the payload carries the bare key name and the
// update runs as the job's tenant.
func TestKMSGracePeriodHandler_TenantJobBoundToItsTenant(t *testing.T) {
	type boundCall struct {
		KeyID   string
		Version uint64
		Tenant  string
		System  bool
	}
	var bound []boundCall
	updater := newMockKMSKeyStatusUpdater()
	updater.updateFunc = func(ctx context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
		tenant, _ := tenantctx.Lookup(ctx)
		bound = append(bound, boundCall{KeyID: keyID, Version: version, Tenant: tenant, System: tenantctx.IsSystem(tenant)})
		return &GracePeriodResult{KeyID: keyID, Version: version, OldStatus: "enabled", NewStatus: "disabled"}, nil
	}
	h := NewKMSGracePeriodHandler(updater)

	tenantJob := func(payload string) *scheduler.Job {
		return &scheduler.Job{
			ID:             7,
			OwnerType:      scheduler.OwnerTypeTenant,
			OwnerID:        "acme",
			TargetTenantID: "acme",
			Handler:        HandlerKMSGracePeriod,
			Payload:        []byte(payload),
		}
	}

	// The tenant's job updates its own key by the bare name, as the tenant.
	_, err := h.Handle(context.Background(), tenantJob(`{"key_id":"key","version":1}`))
	require.NoError(t, err)
	require.Equal(t, []boundCall{{KeyID: "key", Version: 1, Tenant: "acme"}}, bound)

	// A payload naming another tenant's key name still runs as acme, so the
	// update reaches only acme's key of that name.
	bound = nil
	_, err = h.Handle(context.Background(), tenantJob(`{"key_id":"other-key","version":1}`))
	require.NoError(t, err)
	require.Equal(t, []boundCall{{KeyID: "other-key", Version: 1, Tenant: "acme"}}, bound)

	// A delegated job aimed at a tenant is bound the same way.
	bound = nil
	delegated := &scheduler.Job{
		OwnerType:      scheduler.OwnerTypeDelegated,
		TargetTenantID: "acme",
		Payload:        []byte(`{"key_id":"other-key","version":1}`),
	}
	_, err = h.Handle(context.Background(), delegated)
	require.NoError(t, err)
	require.Equal(t, []boundCall{{KeyID: "other-key", Version: 1, Tenant: "acme"}}, bound)

	// A platform job keeps platform authority: it runs as the system tenant.
	bound = nil
	platform := &scheduler.Job{
		OwnerType: scheduler.OwnerTypeDelegated,
		Payload:   []byte(`{"key_id":"other-key","version":2}`),
	}
	_, err = h.Handle(context.Background(), platform)
	require.NoError(t, err)
	require.Equal(t, []boundCall{{KeyID: "other-key", Version: 2, Tenant: tenantctx.SystemTenantID, System: true}}, bound)
}
