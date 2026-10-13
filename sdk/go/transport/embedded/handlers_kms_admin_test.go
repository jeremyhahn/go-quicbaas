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

// handlers_kms_admin_test.go covers the embedded-transport wiring for the KMS
// key-administration verbs: revoke-with-reason mapping, SDK-shape acceptance
// for metadata/policy verbs, and the rotation-policy dispatch entries.

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingKMSService embeds mockKMSService and records the requests the
// handlers convert, so tests can assert SDK request shapes map onto the
// canonical service requests.
type recordingKMSService struct {
	*mockKMSService
	lastRevoke      *service.RevokeKeyRequest
	lastGetMetadata *service.GetKeyMetadataRequest
	lastUpdateMeta  *service.UpdateKeyMetadataRequest
	lastSetPolicy   *service.SetKeyPolicyRequest
	lastGetPolicy   *service.GetKeyPolicyRequest
	lastSetRotation *service.SetKeyRotationPolicyRequest
	lastGetRotation *service.GetKeyRotationPolicyRequest
	lastDelRotation *service.DeleteKeyRotationPolicyRequest
}

func (m *recordingKMSService) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	m.lastRevoke = req
	return &service.RevokeKeyResponse{KeyID: req.KeyID}, nil
}

func (m *recordingKMSService) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	m.lastGetMetadata = req
	return &service.GetKeyMetadataResponse{}, nil
}

func (m *recordingKMSService) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	m.lastUpdateMeta = req
	return &service.UpdateKeyMetadataResponse{}, nil
}

func (m *recordingKMSService) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	m.lastSetPolicy = req
	return &service.SetKeyPolicyResponse{KeyID: req.KeyID}, nil
}

func (m *recordingKMSService) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	m.lastGetPolicy = req
	return &service.GetKeyPolicyResponse{}, nil
}

func (m *recordingKMSService) SetKeyRotationPolicy(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	m.lastSetRotation = req
	return &service.SetKeyRotationPolicyResponse{KeyID: req.KeyID, Policy: req.Policy}, nil
}

func (m *recordingKMSService) GetKeyRotationPolicy(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	m.lastGetRotation = req
	return &service.GetKeyRotationPolicyResponse{KeyID: req.KeyID}, nil
}

func (m *recordingKMSService) DeleteKeyRotationPolicy(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	m.lastDelRotation = req
	return &service.DeleteKeyRotationPolicyResponse{KeyID: req.KeyID}, nil
}

func newRecordingKMSTransport(t *testing.T) (*Transport, *recordingKMSService) {
	t.Helper()
	rec := &recordingKMSService{mockKMSService: &mockKMSService{}}
	tr, err := New(&mockQuicBaaS{kmsService: rec})
	require.NoError(t, err)
	return tr, rec
}

// =============================================================================
// RevokeKey
// =============================================================================

func TestHandleKMSRevokeKey_SDKShapeMapsReason(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	// Mirror the SDK client's unexported revokeKeyRequest shape.
	_, err := tr.Request(context.Background(), "quicbaas.KMS", "RevokeKey",
		&struct{ KeyID, Reason string }{KeyID: "k1", Reason: "compromised"})
	require.NoError(t, err)
	require.NotNil(t, rec.lastRevoke)
	assert.Equal(t, "k1", rec.lastRevoke.KeyID)
	assert.Equal(t, "compromised", rec.lastRevoke.Reason)
}

func TestHandleKMSRevokeKey_ServiceType(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "RevokeKey",
		&service.RevokeKeyRequest{KeyID: "k2", Reason: "rotation"})
	require.NoError(t, err)
	require.NotNil(t, rec.lastRevoke)
	assert.Equal(t, "k2", rec.lastRevoke.KeyID)
	assert.Equal(t, "rotation", rec.lastRevoke.Reason)
}

func TestHandleKMSRevokeKey_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "RevokeKey", "bad")
	require.Error(t, err)
}

// =============================================================================
// Metadata / policy SDK shapes
// =============================================================================

func TestHandleKMSGetKeyMetadata_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyMetadata",
		&struct{ KeyID string }{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, rec.lastGetMetadata)
	assert.Equal(t, "k1", rec.lastGetMetadata.KeyID)
}

func TestHandleKMSGetKeyMetadata_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyMetadata", 42)
	require.Error(t, err)
}

func TestHandleKMSUpdateKeyMetadata_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "UpdateKeyMetadata",
		&kmsclient.UpdateKeyMetadataRequest{KeyID: "k1", Name: "renamed", RemoveTags: []string{"stale"}})
	require.NoError(t, err)
	require.NotNil(t, rec.lastUpdateMeta)
	assert.Equal(t, "k1", rec.lastUpdateMeta.KeyID)
	require.NotNil(t, rec.lastUpdateMeta.Name)
	assert.Equal(t, "renamed", *rec.lastUpdateMeta.Name)
	assert.Nil(t, rec.lastUpdateMeta.Description)
	assert.Equal(t, []string{"stale"}, rec.lastUpdateMeta.RemoveTags)
}

func TestHandleKMSUpdateKeyMetadata_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "UpdateKeyMetadata", "bad")
	require.Error(t, err)
}

func TestHandleKMSSetKeyPolicy_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyPolicy",
		&kmsclient.SetKeyPolicyRequest{
			KeyID: "k1",
			Policy: &kmsclient.KeyPolicy{
				KeyID:             "k1",
				AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
				MaxUsageCount:     100,
				UpdatedAt:         time.Now(),
			},
		})
	require.NoError(t, err)
	require.NotNil(t, rec.lastSetPolicy)
	assert.Equal(t, "k1", rec.lastSetPolicy.KeyID)
	require.NotNil(t, rec.lastSetPolicy.Policy)
	require.NotNil(t, rec.lastSetPolicy.Policy.MaxUsageCount)
	assert.Equal(t, uint64(100), *rec.lastSetPolicy.Policy.MaxUsageCount)
}

// A negative policy version is refused before the service sees it: converted
// to the service's uint64 it would wrap to a huge version.
func TestHandleKMSSetKeyPolicy_RefusesNegativeVersion(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyPolicy",
		&kmsclient.SetKeyPolicyRequest{KeyID: "k1", Policy: &kmsclient.KeyPolicy{Version: -1}})
	require.True(t, errors.IsInvalidInput(err), "err = %v, want invalid input", err)
	assert.Contains(t, err.Error(), "policy version")
	assert.Nil(t, rec.lastSetPolicy, "a negative version never reaches the service")
}

func TestHandleKMSSetKeyPolicy_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyPolicy", "bad")
	require.Error(t, err)
}

func TestHandleKMSGetKeyPolicy_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyPolicy",
		&struct{ KeyID string }{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, rec.lastGetPolicy)
	assert.Equal(t, "k1", rec.lastGetPolicy.KeyID)
}

func TestHandleKMSGetKeyPolicy_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyPolicy", 42)
	require.Error(t, err)
}

// =============================================================================
// Rotation policy dispatch
// =============================================================================

func TestHandleKMSSetKeyRotationPolicy_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	// Mirror the SDK client's unexported flat setKeyRotationPolicyRequest.
	_, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy",
		&struct {
			KeyID           string
			AutoRotate      bool
			IntervalSeconds int64
			RetainVersions  int
			Enabled         bool
		}{KeyID: "k1", AutoRotate: true, IntervalSeconds: 86400, RetainVersions: 3, Enabled: true})
	require.NoError(t, err)
	require.NotNil(t, rec.lastSetRotation)
	assert.Equal(t, "k1", rec.lastSetRotation.KeyID)
	require.NotNil(t, rec.lastSetRotation.Policy)
	assert.Equal(t, 24*time.Hour, rec.lastSetRotation.Policy.Interval)
	assert.True(t, rec.lastSetRotation.Policy.AutoRotate)
	assert.True(t, rec.lastSetRotation.Policy.Enabled)
	assert.Equal(t, 3, rec.lastSetRotation.Policy.RetainVersions)
}

func TestHandleKMSSetKeyRotationPolicy_ServiceType(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy",
		&service.SetKeyRotationPolicyRequest{
			KeyID:  "k2",
			Policy: &service.RotationPolicy{Enabled: true, Interval: time.Hour},
		})
	require.NoError(t, err)
	require.NotNil(t, rec.lastSetRotation)
	assert.Equal(t, "k2", rec.lastSetRotation.KeyID)
}

func TestHandleKMSSetKeyRotationPolicy_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy", "bad")
	require.Error(t, err)
}

func TestHandleKMSGetKeyRotationPolicy_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyRotationPolicy",
		&struct {
			KeyID string `json:"key_id"`
		}{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, rec.lastGetRotation)
	assert.Equal(t, "k1", rec.lastGetRotation.KeyID)
}

func TestHandleKMSGetKeyRotationPolicy_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "GetKeyRotationPolicy", 42)
	require.Error(t, err)
}

func TestHandleKMSDeleteKeyRotationPolicy_SDKShape(t *testing.T) {
	tr, rec := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "DeleteKeyRotationPolicy",
		&struct {
			KeyID string `json:"key_id"`
		}{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, rec.lastDelRotation)
	assert.Equal(t, "k1", rec.lastDelRotation.KeyID)
}

func TestHandleKMSDeleteKeyRotationPolicy_InvalidType(t *testing.T) {
	tr, _ := newRecordingKMSTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.KMS", "DeleteKeyRotationPolicy", 42)
	require.Error(t, err)
}
