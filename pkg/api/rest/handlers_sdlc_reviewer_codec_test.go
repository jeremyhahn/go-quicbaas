//go:build rest && codec_json

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
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// The reviewer override is persisted through the sdlc DAO, which encodes
// with the JSON codec, so this test needs the codec_json tag as well as rest.
func TestSDLCReviewer_OverrideVerbs(t *testing.T) {
	server, svc := setupSDLCServer(t)
	binding := profiledRESTBinding("p1")
	require.NoError(t, svc.Store().PutBinding(context.Background(), "tenant-a", &binding))
	const route = "/api/v1/sdlc/projects/p1/pulls/41/reviewer"

	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, route, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var selection sdlc.ReviewerSelection
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &selection))
	assert.Equal(t, sdlc.DefaultReviewerProfileName, selection.Profile)
	assert.False(t, selection.Overridden)
	assert.True(t, selection.CountsTowardMerge)

	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, route, SDLCReviewerOverrideRequest{Profile: "qwen"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &selection))
	assert.Equal(t, "qwen", selection.Profile)
	assert.True(t, selection.Overridden)
	assert.False(t, selection.CountsTowardMerge)
	assert.Equal(t, sdlc.DefaultReviewStatusContext+"/qwen", selection.StatusContext)

	rec = sdlcCall(t, server, "tenant-a", http.MethodDelete, route, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &selection))
	assert.False(t, selection.Overridden)
	assert.Equal(t, sdlc.DefaultReviewerProfileName, selection.Profile)
}
