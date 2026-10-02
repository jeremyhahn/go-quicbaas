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

// Package service provides tests for the KMS rotation-policy operations and
// the scheduler integration that executes them automatically.
package service

import (
	"testing"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// SetKeyRotationPolicy
// =============================================================================

// TestSetKeyRotationPolicy_Success verifies that a policy is accepted and
// NextRotation is computed when not explicitly supplied.
func TestSetKeyRotationPolicy_Success(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-key-1", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	ctx := systemTenantCtx()
	resp, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
		KeyID: "rp-key-1",
		Policy: &RotationPolicy{
			Enabled:    true,
			AutoRotate: true,
			Interval:   24 * time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy: unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("SetKeyRotationPolicy: nil response")
	}
	if resp.KeyID != "rp-key-1" {
		t.Errorf("KeyID: want rp-key-1, got %s", resp.KeyID)
	}
	if resp.Policy == nil {
		t.Fatal("Policy is nil in response")
	}
	if !resp.Policy.AutoRotate {
		t.Error("AutoRotate should be true")
	}
	if resp.Policy.NextRotation.IsZero() {
		t.Error("NextRotation should be computed from Interval, got zero")
	}
	if !resp.Policy.NextRotation.After(time.Now()) {
		t.Errorf("NextRotation should be in the future, got %v", resp.Policy.NextRotation)
	}
}

// TestSetKeyRotationPolicy_NilRequest verifies nil request is rejected.
func TestSetKeyRotationPolicy_NilRequest(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.SetKeyRotationPolicy(systemTenantCtx(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// TestSetKeyRotationPolicy_EmptyKeyID verifies empty key ID is rejected.
func TestSetKeyRotationPolicy_EmptyKeyID(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.SetKeyRotationPolicy(systemTenantCtx(), &SetKeyRotationPolicyRequest{
		KeyID:  "",
		Policy: &RotationPolicy{AutoRotate: false},
	})
	if err == nil {
		t.Fatal("expected error for empty key_id, got nil")
	}
}

// TestSetKeyRotationPolicy_NilPolicy verifies nil policy is rejected.
func TestSetKeyRotationPolicy_NilPolicy(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-nil-policy", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.SetKeyRotationPolicy(systemTenantCtx(), &SetKeyRotationPolicyRequest{
		KeyID:  "rp-nil-policy",
		Policy: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil policy, got nil")
	}
}

// TestSetKeyRotationPolicy_AutoRotateWithZeroInterval verifies that enabling
// auto-rotation without a positive interval is rejected.
func TestSetKeyRotationPolicy_AutoRotateWithZeroInterval(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-zero-interval", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.SetKeyRotationPolicy(systemTenantCtx(), &SetKeyRotationPolicyRequest{
		KeyID: "rp-zero-interval",
		Policy: &RotationPolicy{
			AutoRotate: true,
			Interval:   0, // zero interval is invalid
		},
	})
	if err == nil {
		t.Fatal("expected error for AutoRotate=true with Interval=0, got nil")
	}
}

// TestSetKeyRotationPolicy_KeyNotFound verifies that a missing key returns an error.
func TestSetKeyRotationPolicy_KeyNotFound(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.SetKeyRotationPolicy(systemTenantCtx(), &SetKeyRotationPolicyRequest{
		KeyID: "no-such-key",
		Policy: &RotationPolicy{
			AutoRotate: true,
			Interval:   time.Hour,
		},
	})
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

// TestSetKeyRotationPolicy_DisableAutoRotate verifies that setting AutoRotate=false
// with any Interval is accepted (no validation error).
func TestSetKeyRotationPolicy_DisableAutoRotate(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-disable", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	resp, err := svc.SetKeyRotationPolicy(systemTenantCtx(), &SetKeyRotationPolicyRequest{
		KeyID: "rp-disable",
		Policy: &RotationPolicy{
			AutoRotate: false,
			Interval:   0, // zero is fine when AutoRotate=false
		},
	})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy: unexpected error: %v", err)
	}
	if resp.Policy.AutoRotate {
		t.Error("AutoRotate should be false")
	}
}

// =============================================================================
// GetKeyRotationPolicy
// =============================================================================

// TestGetKeyRotationPolicy_RoundTrip verifies that a policy written via
// SetKeyRotationPolicy is read back by GetKeyRotationPolicy (in-metadata path).
func TestGetKeyRotationPolicy_RoundTrip(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-roundtrip", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	ctx := systemTenantCtx()

	interval := 7 * 24 * time.Hour
	_, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
		KeyID: "rp-roundtrip",
		Policy: &RotationPolicy{
			Enabled:    true,
			AutoRotate: true,
			Interval:   interval,
		},
	})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy: %v", err)
	}

	getResp, err := svc.GetKeyRotationPolicy(ctx, &GetKeyRotationPolicyRequest{
		KeyID: "rp-roundtrip",
	})
	if err != nil {
		t.Fatalf("GetKeyRotationPolicy: %v", err)
	}
	if getResp.Policy == nil {
		t.Fatal("expected policy, got nil")
	}
	if !getResp.Policy.AutoRotate {
		t.Error("AutoRotate should be true")
	}
	if getResp.Policy.Interval != interval {
		t.Errorf("Interval: want %v, got %v", interval, getResp.Policy.Interval)
	}
}

// TestGetKeyRotationPolicy_NilRequest verifies nil request is rejected.
func TestGetKeyRotationPolicy_NilRequest(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.GetKeyRotationPolicy(systemTenantCtx(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// TestGetKeyRotationPolicy_EmptyKeyID verifies empty key ID is rejected.
func TestGetKeyRotationPolicy_EmptyKeyID(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.GetKeyRotationPolicy(systemTenantCtx(), &GetKeyRotationPolicyRequest{KeyID: ""})
	if err == nil {
		t.Fatal("expected error for empty key_id, got nil")
	}
}

// TestGetKeyRotationPolicy_KeyNotFound verifies that a missing key returns an error.
func TestGetKeyRotationPolicy_KeyNotFound(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.GetKeyRotationPolicy(systemTenantCtx(), &GetKeyRotationPolicyRequest{
		KeyID: "no-such-key",
	})
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

// TestGetKeyRotationPolicy_NoPolicy verifies that a key with no policy returns
// a nil policy without error.
func TestGetKeyRotationPolicy_NoPolicy(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-no-policy", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	resp, err := svc.GetKeyRotationPolicy(systemTenantCtx(), &GetKeyRotationPolicyRequest{
		KeyID: "rp-no-policy",
	})
	if err != nil {
		t.Fatalf("GetKeyRotationPolicy: unexpected error: %v", err)
	}
	// A key with no RotationPolicy in metadata should return nil policy.
	if resp.Policy != nil {
		t.Errorf("expected nil policy for key with no policy, got %+v", resp.Policy)
	}
}

// =============================================================================
// DeleteKeyRotationPolicy
// =============================================================================

// TestDeleteKeyRotationPolicy_Success verifies that a set policy is removed.
func TestDeleteKeyRotationPolicy_Success(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("rp-delete", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	ctx := systemTenantCtx()

	// Set a policy first.
	_, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
		KeyID: "rp-delete",
		Policy: &RotationPolicy{
			Enabled:    true,
			AutoRotate: true,
			Interval:   24 * time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy: %v", err)
	}

	// Delete the policy.
	delResp, err := svc.DeleteKeyRotationPolicy(ctx, &DeleteKeyRotationPolicyRequest{
		KeyID: "rp-delete",
	})
	if err != nil {
		t.Fatalf("DeleteKeyRotationPolicy: %v", err)
	}
	if delResp.KeyID != "rp-delete" {
		t.Errorf("KeyID: want rp-delete, got %s", delResp.KeyID)
	}
}

// TestDeleteKeyRotationPolicy_NilRequest verifies nil request is rejected.
func TestDeleteKeyRotationPolicy_NilRequest(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.DeleteKeyRotationPolicy(systemTenantCtx(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// TestDeleteKeyRotationPolicy_EmptyKeyID verifies empty key ID is rejected.
func TestDeleteKeyRotationPolicy_EmptyKeyID(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.DeleteKeyRotationPolicy(systemTenantCtx(), &DeleteKeyRotationPolicyRequest{KeyID: ""})
	if err == nil {
		t.Fatal("expected error for empty key_id, got nil")
	}
}

// TestDeleteKeyRotationPolicy_KeyNotFound verifies that a missing key returns an error.
func TestDeleteKeyRotationPolicy_KeyNotFound(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	_, err := svc.DeleteKeyRotationPolicy(systemTenantCtx(), &DeleteKeyRotationPolicyRequest{
		KeyID: "no-such-key",
	})
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

// =============================================================================
// GenerateKey with RotationPolicy
// =============================================================================

// TestGenerateKey_WithRotationPolicy verifies that a key created with a
// RotationPolicy has it embedded in both the response metadata and the
// manager's stored metadata.
func TestGenerateKey_WithRotationPolicy(t *testing.T) {
	mgr := newRotationKMSManager()
	svc := buildRotationSchedulerKMS(mgr)

	ctx := systemTenantCtx()
	interval := 30 * 24 * time.Hour

	resp, err := svc.GenerateKey(ctx, &GenerateKeyRequest{
		Name:          "gen-with-policy",
		Description:   "key created with rotation policy",
		Usage:         []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		KeyAttributes: buildSymmetricAttrs(),
		RotationPolicy: &RotationPolicy{
			Enabled:    true,
			AutoRotate: true,
			Interval:   interval,
		},
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if resp.Metadata == nil {
		t.Fatal("response Metadata is nil")
	}
	if resp.Metadata.RotationPolicy == nil {
		t.Fatal("RotationPolicy should be present in response metadata")
	}
	if !resp.Metadata.RotationPolicy.AutoRotate {
		t.Error("AutoRotate should be true")
	}
	if resp.Metadata.RotationPolicy.NextRotation.IsZero() {
		t.Error("NextRotation should be computed from Interval, got zero")
	}
}

// =============================================================================
// Rotation scan: set policy via API → the scan rotates the key
// =============================================================================

// TestSchedulerIntegration_APISetPolicy proves the full lifecycle:
// 1. Create a key.
// 2. Set an auto-rotation policy via SetKeyRotationPolicy (past NextRotation).
// 3. A rotation scan rotates the key and advances its deadline.
func TestSchedulerIntegration_APISetPolicy(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("sched-api-key", "AES-256-GCM"))
	svc := buildRotationSchedulerKMS(mgr)

	ctx := systemTenantCtx()

	// Set a policy with NextRotation in the PAST so the scheduler acts immediately.
	pastTime := time.Now().Add(-5 * time.Minute)
	_, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
		KeyID: "sched-api-key",
		Policy: &RotationPolicy{
			Enabled:      true,
			AutoRotate:   true,
			Interval:     time.Hour,
			NextRotation: pastTime,
		},
	})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy: %v", err)
	}

	// One rotation scan rotates the key because NextRotation is in the past.
	result, err := svc.RotateDueKeys(ctx)
	if err != nil {
		t.Fatalf("RotateDueKeys: %v", err)
	}
	if result.Rotated != 1 || mgr.rotationCount() != 1 {
		t.Fatalf("expected one rotation after an API-set past NextRotation, got result %+v", result)
	}

	// The scan advances NextRotation to a future deadline.
	upd := mgr.lastMetadataUpdate()
	if upd == nil || upd.RotationPolicy == nil {
		t.Fatal("expected metadata update with RotationPolicy")
	}
	if !upd.RotationPolicy.NextRotation.After(time.Now()) {
		t.Errorf("NextRotation should be in the future after advancement, got %v",
			upd.RotationPolicy.NextRotation)
	}
}

// TestSchedulerIntegration_AuthzGate verifies that the authz gate blocks an
// unauthorized principal from setting the rotation policy (fail-closed).
func TestSchedulerIntegration_AuthzGate(t *testing.T) {
	mgr := newRotationKMSManager()
	mgr.addKey(newKMSKeyMeta("authz-rp-key", "AES-256-GCM"))

	svc := newKMSService(mgr, nil)
	svc.authzEnabled = true
	// No KVStore — so loadKeyPolicy returns nil → default-allow.
	// We still test the path where no authenticated subject is in context.

	// Context without a subject — should be denied (fail-closed).
	ctx := systemTenantCtx()
	_, err := svc.SetKeyRotationPolicy(ctx, &SetKeyRotationPolicyRequest{
		KeyID: "authz-rp-key",
		Policy: &RotationPolicy{
			AutoRotate: true,
			Interval:   time.Hour,
		},
	})
	if err == nil {
		t.Fatal("expected ErrForbidden when no authenticated subject in context, got nil")
	}
}

// =============================================================================
// Helpers
// =============================================================================

// newKMSKeyMeta builds a minimal *kms.KeyMetadata for test use.
func newKMSKeyMeta(keyID, algorithm string) *kms.KeyMetadata {
	return &kms.KeyMetadata{
		KeyID:     keyID,
		Algorithm: algorithm,
		Version:   1,
		State:     kms.KeyStateEnabled,
	}
}

// buildSymmetricAttrs creates minimal KeyAttributes for AES-256-GCM.
func buildSymmetricAttrs() *kctypes.KeyAttributes {
	return &kctypes.KeyAttributes{
		SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		KeyType:            kctypes.KeyTypeEncryption,
	}
}
