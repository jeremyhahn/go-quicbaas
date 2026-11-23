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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestUpdateMetadata_TenantNamespaceScopedOnce proves a non-system tenant's
// metadata update reads and rewrites the vector in its own scoped namespace,
// scoped once; scoping it twice made the update fail for every tenant.
func TestUpdateMetadata_TenantNamespaceScopedOnce(t *testing.T) {
	const tenantID = "tenant-a"
	scoped := "tenant/" + tenantID + "/test-ns"

	svc, storage, vt, _ := newVecSvcWithSDK(t, scoped, 3)

	var getNamespace string
	storage.getVectorFn = func(_ context.Context, namespace string, _ uint64) (*interfaces.VectorValue, error) {
		getNamespace = namespace
		return &interfaces.VectorValue{
			ID:        1,
			Embedding: []float32{1, 2, 3},
			Metadata:  map[string]string{"old": "val"},
		}, nil
	}

	var upsertNamespace string
	var upsertMetadata map[string]interface{}
	vt.upsertVectorFn = func(_ context.Context, req *qrdbtransport.UpsertVectorRequest) (*qrdbtransport.UpsertVectorResponse, error) {
		upsertNamespace = req.Namespace
		upsertMetadata = req.Metadata
		return &qrdbtransport.UpsertVectorResponse{ID: req.ID, Version: 2, Success: true}, nil
	}

	resp, err := svc.UpdateMetadata(tenantctx.WithTenant(context.Background(), tenantID), &UpdateVectorMetadataRequest{
		Namespace:     "test-ns",
		ID:            1,
		Metadata:      map[string]interface{}{"new": "val"},
		MergeMetadata: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)

	assert.Equal(t, scoped, getNamespace)
	assert.Equal(t, scoped, upsertNamespace)
	assert.Equal(t, "val", upsertMetadata["new"])
	assert.Equal(t, "val", upsertMetadata["old"])
}
