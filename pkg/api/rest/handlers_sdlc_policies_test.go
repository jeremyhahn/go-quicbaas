//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// TestSDLCBinding_ARebindKeepsTheTrainAndClustering is the REST half of the
// rebind regression for the throughput policies: a disabled train must stay
// disabled through a read-modify-write, not come back on as the default.
func TestSDLCBinding_ARebindKeepsTheTrainAndClustering(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBindableProject("p1")
	binding := restBinding("p1")
	binding.MergeTrain = sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7}
	binding.Clustering = sdlc.ClusteringPolicy{MaxSize: 3}

	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", binding)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var read sdlc.ProjectBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &read))
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", read)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var rebound sdlc.ProjectBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rebound))
	assert.Equal(t, binding.MergeTrain, rebound.MergeTrain)
	assert.Equal(t, binding.Clustering, rebound.Clustering)
}

// TestSDLCBinding_AnOutOfRangeTrainIsRefused proves the bound is enforced at
// the edge, before anything is stored.
func TestSDLCBinding_AnOutOfRangeTrainIsRefused(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBindableProject("p1")
	binding := restBinding("p1")
	binding.MergeTrain = sdlc.MergeTrainPolicy{MaxSize: sdlc.MaxMergeTrainSize + 1}
	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", binding)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
