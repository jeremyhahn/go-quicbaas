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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// countingKMSKeyRotator counts the scans it is asked to run.
type countingKMSKeyRotator struct {
	calls atomic.Int32
}

func (c *countingKMSKeyRotator) RotateDueKeys(context.Context) (*KMSRotationResult, error) {
	c.calls.Add(1)
	return &KMSRotationResult{}, nil
}

// A tenant-owned job naming the rotation handler must not run the
// cross-tenant rotation scan; a system job still does.
func TestKMSRotationHandler_Handle_RefusesTenantOwnedJob(t *testing.T) {
	rotator := &countingKMSKeyRotator{}
	h := NewKMSRotationHandler(rotator)

	_, err := h.Handle(context.Background(), &scheduler.Job{
		ID:        1,
		OwnerType: scheduler.OwnerTypeTenant,
		OwnerID:   "acme",
		Handler:   HandlerKMSRotation,
	})
	require.Error(t, err, "a tenant-owned job is refused")
	var invalid *scheduler.InvalidJobError
	assert.ErrorAs(t, err, &invalid)
	assert.Equal(t, int32(0), rotator.calls.Load(), "the scan is not run for a tenant job")

	res, err := h.Handle(context.Background(), &scheduler.Job{
		ID:        2,
		OwnerType: scheduler.OwnerTypeSystem,
		Handler:   HandlerKMSRotation,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Equal(t, int32(1), rotator.calls.Load(), "a system job runs the scan once")
}
