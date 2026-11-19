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

//go:build integration
// +build integration

package iam_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// adminAPIView returns a view of tc for the delegated-administration API
// (/api/v1/admin/assignments and /api/v1/admin/impersonation). Admin
// assignments live in one process-wide store and name any tenant, so these
// calls belong to the fixture system admin. The view's raw HTTP client sends
// the system admin's bearer on every request; the bearer is fetched once when
// the view is built. tc itself is unchanged and stays unauthenticated.
func adminAPIView(t *testing.T, tc *testCluster) *testCluster {
	t.Helper()
	view := tc.asSystemAdmin()
	bearer, err := view.bearer(context.Background())
	require.NoError(t, err, "system admin bearer for the admin API")
	view.bearer = func(context.Context) (string, error) { return bearer, nil }
	transport, err := dataplane.NewBearerTransport(view.bearer, tc.client.Transport)
	require.NoError(t, err, "system admin transport for the admin API")
	client := *tc.client
	client.Transport = transport
	view.client = &client
	return view
}
