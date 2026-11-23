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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// recordingDEKKMS records the service request each DEK handler hands to the
// KMS service, so a test can prove the dek client's call reached it.
type recordingDEKKMS struct {
	mockKMSService
	setDefault  *service.SetDefaultDEKRequest
	setSafety   *service.SetDEKSafetyRequest
	clearSafety *service.ClearDEKSafetyRequest
}

func (r *recordingDEKKMS) SetDefaultDEK(_ context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	r.setDefault = req
	return &service.SetDefaultDEKResponse{Path: req.Path, PreviousDefault: "dek/previous"}, nil
}

func (r *recordingDEKKMS) SetDEKSafety(_ context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	r.setSafety = req
	return &service.SetDEKSafetyResponse{Path: req.Path}, nil
}

func (r *recordingDEKKMS) ClearDEKSafety(_ context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	r.clearSafety = req
	return &service.ClearDEKSafetyResponse{Path: req.Path, Cleared: true}, nil
}

// newDEKClientOverEmbedded builds the real dek client over the embedded
// transport, backed by a recording KMS double.
func newDEKClientOverEmbedded(t *testing.T) (dekclient.Client, *Transport, *recordingDEKKMS) {
	t.Helper()
	kms := &recordingDEKKMS{}
	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	return dekclient.NewClient(tr), tr, kms
}

func TestDEKClientOverEmbedded_SetDefaultDEK(t *testing.T) {
	client, _, kms := newDEKClientOverEmbedded(t)

	resp, err := client.SetDefaultDEK(context.Background(), "dek/current")
	require.NoError(t, err)
	require.NotNil(t, kms.setDefault, "the call must reach the KMS service")
	assert.Equal(t, "dek/current", kms.setDefault.Path)
	assert.Equal(t, "dek/current", resp.Path)
	assert.Equal(t, "dek/previous", resp.PreviousDefault)
}

func TestDEKClientOverEmbedded_SetDEKSafety(t *testing.T) {
	client, _, kms := newDEKClientOverEmbedded(t)

	err := client.SetDEKSafety(context.Background(), &dekclient.SetDEKSafetyRequest{
		Path:           "dek/current",
		MaxOperations:  1000,
		MaxBytes:       4096,
		WarningPercent: 80,
	})
	require.NoError(t, err)
	require.NotNil(t, kms.setSafety, "the call must reach the KMS service")
	assert.Equal(t, &service.SetDEKSafetyRequest{
		Path:           "dek/current",
		MaxOperations:  1000,
		MaxBytes:       4096,
		WarningPercent: 80,
	}, kms.setSafety)
}

func TestDEKClientOverEmbedded_ClearDEKSafety(t *testing.T) {
	client, _, kms := newDEKClientOverEmbedded(t)

	err := client.ClearDEKSafety(context.Background(), "dek/current")
	require.NoError(t, err)
	require.NotNil(t, kms.clearSafety, "the call must reach the KMS service")
	assert.Equal(t, "dek/current", kms.clearSafety.Path)
}

// TestDEKHandlers_WrongRequestType checks each handler still refuses a request
// it cannot map, with the typed InvalidRequestTypeError.
func TestDEKHandlers_WrongRequestType(t *testing.T) {
	_, tr, kms := newDEKClientOverEmbedded(t)

	for _, method := range []string{"SetDefaultDEK", "SetDEKSafety", "ClearDEKSafety"} {
		t.Run(method, func(t *testing.T) {
			resp, err := tr.Request(context.Background(), "quicbaas.DEK", method, &struct{ NoPath int }{})
			require.Error(t, err)
			assert.Nil(t, resp)
			var typeErr *InvalidRequestTypeError
			require.True(t, stderrors.As(err, &typeErr), "want *InvalidRequestTypeError, got %T", err)
			assert.Equal(t, method, typeErr.Method)
		})
	}
	assert.Nil(t, kms.setDefault)
	assert.Nil(t, kms.setSafety)
	assert.Nil(t, kms.clearSafety)
}
