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

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKMSService_DEKSafetyInterlock_Lifecycle walks a DEK through the safety
// interlock: SetDEKSafety caps its AEAD usage, the use that reaches the cap is
// refused, RotateDEK gives a fresh key version whose usage starts at zero so
// it is accepted again, and ClearDEKSafety lifts the per-DEK cap.
func TestKMSService_DEKSafetyInterlock_Lifecycle(t *testing.T) {
	svc := newKMSService(newMockKMSManager(), newMockLogger())
	ctx := systemTenantCtx()

	const path = "/records/interlock"
	_, err := svc.CreateDEK(ctx, &CreateDEKRequest{Path: path, Algorithm: KeyAlgAES256GCM})
	require.NoError(t, err)

	tenantID, err := prepareTenantContext(ctx)
	require.NoError(t, err)
	scoped := tenantScopedKeyID(tenantID, normalizeDEKPath(path))
	use := func() error { return svc.safetyTracker.RecordDEKEncrypt(ctx, scoped, 16) }

	_, err = svc.SetDEKSafety(ctx, &SetDEKSafetyRequest{Path: path, MaxOperations: 3, MaxBytes: 1 << 20, WarningPercent: 50})
	require.NoError(t, err)

	require.NoError(t, use(), "first use under the cap")
	require.NoError(t, use(), "second use under the cap")
	require.Error(t, use(), "the use that reaches the cap is refused")
	require.Error(t, use(), "the interlock holds until the DEK is rotated")

	status, err := svc.GetDEKStatus(ctx, &GetDEKStatusRequest{Path: path})
	require.NoError(t, err)
	require.Equal(t, uint64(4), status.DEK.UsageCount, "status reports the usage the interlock counts")

	rotated, err := svc.RotateDEK(ctx, &RotateDEKRequest{Path: path})
	require.NoError(t, err)
	require.Equal(t, rotated.OldVersion+1, rotated.NewVersion)

	status, err = svc.GetDEKStatus(ctx, &GetDEKStatusRequest{Path: path})
	require.NoError(t, err)
	require.Zero(t, status.DEK.UsageCount, "rotation starts the new version's usage at zero")
	require.NoError(t, use(), "the rotated DEK is usable again under the same cap")

	_, err = svc.ClearDEKSafety(ctx, &ClearDEKSafetyRequest{Path: path})
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, use(), "with the per-DEK cap cleared, use %d is accepted", i)
	}
}
