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

//go:build !integration && vector && qrdb_vector

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// A composite vector verb served by the node needs only the permission its
// own operation declares. The service calls qrdb's matching native verb under
// the caller's identity, never another separately authorized verb, so a
// caller granted only the requested verb is served and a caller granted only
// a different verb is refused.
func TestVectorService_CompositeVerbsNeedOnlyTheirOwnVerb(t *testing.T) {
	cases := []struct {
		name  string
		verb  string
		other string
		call  func(ctx context.Context, svc service.VectorService) error
	}{
		{name: "hybrid search", verb: "vector:hybrid_search", other: "vector:search_similar", call: func(ctx context.Context, svc service.VectorService) error {
			resp, err := svc.HybridSearch(ctx, &service.HybridSearchRequest{
				Namespace: "acme-docs", QueryVector: []float32{1, 0, 0}, K: 1,
			})
			if err == nil && (len(resp.Results) != 1 || resp.Results[0].ID != 7) {
				return &unexpectedCountError{got: len(resp.Results)}
			}
			return err
		}},
		{name: "search by id", verb: "vector:search_by_id", other: "vector:search_similar", call: func(ctx context.Context, svc service.VectorService) error {
			resp, err := svc.SearchByID(ctx, &service.SearchByIDRequest{Namespace: "acme-docs", ID: 7, K: 1})
			if err == nil && (len(resp.Results) != 1 || resp.Results[0].ID != 7) {
				return &unexpectedCountError{got: len(resp.Results)}
			}
			return err
		}},
		{name: "update metadata", verb: "vector:update_metadata", other: "vector:upsert", call: func(ctx context.Context, svc service.VectorService) error {
			_, err := svc.UpdateMetadata(ctx, &service.UpdateVectorMetadataRequest{
				Namespace: "acme-docs", ID: 7, Metadata: map[string]interface{}{"lang": "go"},
			})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStrictPrincipalServer(t)
			newAcmeDocs(t, newNodeVectorService(t, s))
			// A service built after the namespace holds no index for it, as
			// after a restart, so the node answers and not a local index.
			svc := newNodeVectorService(t, s)

			require.NoError(t, tc.call(tenantVectorCaller(t, "acme", tc.verb), svc),
				"a caller granted %s must not also need another verb", tc.verb)

			err := tc.call(tenantVectorCaller(t, "acme", tc.other), svc)
			require.Error(t, err, "a caller granted only %s was served %s", tc.other, tc.name)
			assert.Contains(t, err.Error(), "alice cannot", "qrdb must judge the caller against %s", tc.verb)
		})
	}
}
