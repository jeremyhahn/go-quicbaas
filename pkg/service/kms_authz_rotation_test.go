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

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// newProductionAuthzKMS builds the KMS service the way quicbaas.go does
// (newKMSService then WithKMSAuthzEnabled(true)) and stores a key whose
// policy allows only encrypt.
func newProductionAuthzKMS(t *testing.T, keyID string) *kmsServiceImpl {
	t.Helper()
	km := newMockKMSManager()
	km.keys[keyID] = &mockKeyData{keyID: keyID, status: "enabled"}
	svc := newKMSService(km, nil)
	WithKMSAuthzEnabled(true)(svc)
	svc.kvStore = newMockKVStore()

	_, err := svc.SetKeyPolicy(systemTenantCtx(), &SetKeyPolicyRequest{
		KeyID:  keyID,
		Policy: &KeyPolicy{KeyID: keyID, AllowedOperations: []KeyUsage{KeyUsageEncrypt}},
	})
	require.NoError(t, err)
	return svc
}

func TestKMSAuthz_RotationPolicyOpsOnOperationRestrictedKey(t *testing.T) {
	svc := newProductionAuthzKMS(t, "rot-key")
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	setResp, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
		KeyID:  "rot-key",
		Policy: &RotationPolicy{AutoRotate: true, Interval: time.Hour},
	})
	require.NoError(t, err, "set rotation policy on an encrypt-only key")
	assert.True(t, setResp.Policy.AutoRotate)

	getResp, err := svc.GetKeyRotationPolicy(ctx, &GetKeyRotationPolicyRequest{KeyID: "rot-key"})
	require.NoError(t, err, "get rotation policy on an encrypt-only key")
	require.NotNil(t, getResp.Policy)
	assert.Equal(t, time.Hour, getResp.Policy.Interval)

	_, err = svc.DeleteKeyRotationPolicy(ctx, &DeleteKeyRotationPolicyRequest{KeyID: "rot-key"})
	require.NoError(t, err, "delete rotation policy on an encrypt-only key")
}

func TestKMSAuthz_OperationOutsideAllowlistStillRefused(t *testing.T) {
	svc := newProductionAuthzKMS(t, "rot-key")
	ctx := subjectCtx(authenticatedSubject("user-1", "alice"))

	err := svc.checkKeyAuthz(ctx, "rot-key", "decrypt")
	require.Error(t, err)
	var qe *errors.QBError
	require.ErrorAs(t, err, &qe)
	assert.Equal(t, errors.ErrForbidden, qe.Code)
}
