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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// keyStatusKMS is a KMS double that records the status each key was set to.
type keyStatusKMS struct {
	mockKMSService
	status   map[string]service.KeyStatus
	requests []*service.UpdateKeyStatusRequest
}

func (k *keyStatusKMS) UpdateKeyStatus(_ context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	k.requests = append(k.requests, req)
	old := k.status[req.KeyID]
	k.status[req.KeyID] = req.Status
	return &service.UpdateKeyStatusResponse{
		KeyID:     req.KeyID,
		Version:   req.Version,
		OldStatus: old,
		NewStatus: req.Status,
	}, nil
}

func newKeyStatusClient(t *testing.T) (kmsclient.Client, *keyStatusKMS) {
	t.Helper()
	kms := &keyStatusKMS{status: map[string]service.KeyStatus{"key-1": service.KeyStatusEnabled}}
	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	return kmsclient.NewClient(tr), kms
}

// TestEmbeddedKMS_UpdateKeyStatus_DisableThenEnable drives the real SDK KMS
// client through the embedded transport, as an embedded consumer does.
func TestEmbeddedKMS_UpdateKeyStatus_DisableThenEnable(t *testing.T) {
	client, kms := newKeyStatusClient(t)
	ctx := context.Background()

	disabled, err := client.UpdateKeyStatus(ctx, &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Status: kmsclient.KeyStatusDisabled})
	require.NoError(t, err)
	assert.Equal(t, &kmsclient.UpdateKeyStatusResponse{KeyID: "key-1", Status: kmsclient.KeyStatusDisabled}, disabled)
	assert.Equal(t, service.KeyStatusDisabled, kms.status["key-1"])

	enabled, err := client.UpdateKeyStatus(ctx, &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Version: 2, Status: kmsclient.KeyStatusEnabled})
	require.NoError(t, err)
	assert.Equal(t, &kmsclient.UpdateKeyStatusResponse{KeyID: "key-1", Version: 2, Status: kmsclient.KeyStatusEnabled}, enabled)
	assert.Equal(t, service.KeyStatusEnabled, kms.status["key-1"])
	require.Len(t, kms.requests, 2)
	assert.Equal(t, uint64(2), kms.requests[1].Version)
}

// TestEmbeddedKMS_UpdateKeyStatus_EveryStatus checks that each status the SDK
// client can carry reaches the service as the status of the same name.
func TestEmbeddedKMS_UpdateKeyStatus_EveryStatus(t *testing.T) {
	want := map[kmsclient.KeyStatus]service.KeyStatus{
		kmsclient.KeyStatusEnabled:         service.KeyStatusEnabled,
		kmsclient.KeyStatusDisabled:        service.KeyStatusDisabled,
		kmsclient.KeyStatusPendingDeletion: service.KeyStatusPendingDeletion,
		kmsclient.KeyStatusDeleted:         service.KeyStatusDeleted,
		kmsclient.KeyStatusRotating:        service.KeyStatusRotating,
		kmsclient.KeyStatusCompromised:     service.KeyStatusCompromised,
	}
	client, kms := newKeyStatusClient(t)
	for sdkStatus, svcStatus := range want {
		t.Run(sdkStatus.String(), func(t *testing.T) {
			_, err := client.UpdateKeyStatus(context.Background(), &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Status: sdkStatus})
			require.NoError(t, err)
			assert.Equal(t, svcStatus, kms.status["key-1"])
		})
	}
}

func TestEmbeddedKMS_UpdateKeyStatus_RefusesBadRequests(t *testing.T) {
	client, kms := newKeyStatusClient(t)
	ctx := context.Background()

	_, err := client.UpdateKeyStatus(ctx, &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Status: kmsclient.KeyStatus(99)})
	assert.True(t, errors.IsInvalidInput(err), "unknown status: err = %v, want InvalidInput", err)

	_, err = client.UpdateKeyStatus(ctx, &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Version: -1, Status: kmsclient.KeyStatusDisabled})
	assert.True(t, errors.IsInvalidInput(err), "negative version: err = %v, want InvalidInput", err)

	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	_, err = tr.Request(ctx, "quicbaas.KMS", "UpdateKeyStatus", &kmsclient.UpdateKeyMetadataRequest{KeyID: "key-1"})
	var invalid *InvalidRequestTypeError
	assert.ErrorAs(t, err, &invalid)

	assert.Empty(t, kms.requests, "a refused request must not reach the service")
}

func TestEmbeddedKMS_UpdateKeyStatus_ServiceNotConfigured(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)
	_, err = kmsclient.NewClient(tr).UpdateKeyStatus(context.Background(), &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Status: kmsclient.KeyStatusDisabled})
	var notConfigured *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &notConfigured)
}
