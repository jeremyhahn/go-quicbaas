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
)

// A tenant's grace job disables only that tenant's own key versions,
// whatever its payload names: the key is bound to the job's tenant at
// execution.
func TestKMSGracePeriodHandler_TenantJobBoundToItsTenant(t *testing.T) {
	updater := newMockKMSKeyStatusUpdater()
	updater.updateFunc = func(_ context.Context, keyID string, version uint64) (*GracePeriodResult, error) {
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

	for _, payload := range []string{
		`{"key_id":"tenant/other/key","version":1}`,
		`{"key_id":"system-key","version":1}`,
		`{"key_id":"tenant/acme-evil/key","version":1}`,
	} {
		_, err := h.Handle(context.Background(), tenantJob(payload))
		var invalid *scheduler.InvalidJobError
		require.ErrorAs(t, err, &invalid, "payload %s", payload)
		require.Equal(t, "payload.key_id", invalid.Field)
	}
	require.Empty(t, updater.calls, "no key outside the tenant is touched")

	_, err := h.Handle(context.Background(), tenantJob(`{"key_id":"tenant/acme/key","version":1}`))
	require.NoError(t, err)
	require.Equal(t, []updateCall{{KeyID: "tenant/acme/key", Version: 1}}, updater.calls)

	// A delegated job aimed at a tenant is bound the same way.
	delegated := &scheduler.Job{
		OwnerType:      scheduler.OwnerTypeDelegated,
		TargetTenantID: "acme",
		Payload:        []byte(`{"key_id":"tenant/other/key","version":1}`),
	}
	_, err = h.Handle(context.Background(), delegated)
	require.Error(t, err)

	// A platform job keeps platform authority.
	platform := &scheduler.Job{
		OwnerType: scheduler.OwnerTypeDelegated,
		Payload:   []byte(`{"key_id":"tenant/other/key","version":2}`),
	}
	_, err = h.Handle(context.Background(), platform)
	require.NoError(t, err)
}
