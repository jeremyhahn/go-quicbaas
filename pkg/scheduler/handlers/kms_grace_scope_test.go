package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// A tenant-owned grace job's payload is editable by its tenant, so the key it
// names must lie inside the owner's key namespace. System and delegated jobs
// keep acting on any key.
func TestKMSGracePeriodHandler_Handle_TenantJobKeyScope(t *testing.T) {
	tests := []struct {
		name      string
		ownerType scheduler.OwnerType
		ownerID   string
		keyID     string
		wantErr   bool
	}{
		{"tenant job, another tenant's key", scheduler.OwnerTypeTenant, "acme", "tenant/victim/k", true},
		{"tenant job, system key", scheduler.OwnerTypeTenant, "acme", "plain-system-key", true},
		{"tenant job, tenant name prefix", scheduler.OwnerTypeTenant, "acme", "tenant/acme-other/k", true},
		{"tenant job, own key", scheduler.OwnerTypeTenant, "acme", "tenant/acme/k", false},
		{"delegated job, any tenant key", scheduler.OwnerTypeDelegated, "", "tenant/victim/k", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockKMSKeyStatusUpdater()
			h := NewKMSGracePeriodHandler(mock)

			payload, err := json.Marshal(KMSGracePeriodPayload{KeyID: tt.keyID, Version: 1})
			require.NoError(t, err)
			job := &scheduler.Job{
				ID:        1,
				OwnerType: tt.ownerType,
				OwnerID:   tt.ownerID,
				Handler:   HandlerKMSGracePeriod,
				Payload:   payload,
			}

			result, err := h.Handle(context.Background(), job)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				var invalid *scheduler.InvalidJobError
				assert.ErrorAs(t, err, &invalid)
				assert.Empty(t, mock.calls, "the key is not touched")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, mock.calls, 1)
			assert.Equal(t, tt.keyID, mock.calls[0].KeyID)
		})
	}
}
