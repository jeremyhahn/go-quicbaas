// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package tenantbarrier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// go-quicbaas#995: the per-tenant barrier at the root /v1/tenants path.
//
// go-qrdb's REST transport (the one `tenant barrier ... --transport rest`
// drives) addresses the per-tenant barrier at
// /v1/tenants/{tenant_id}/barrier/{init,status,unseal,unseal-share,seal},
// not at quicbaas's /api/v1 mirror. These cases send exactly what that
// transport sends (its own DTOs, its own paths) to the daemon's REST
// listener, so a 404 there is the defect #995 names.
// =============================================================================

// wireTenantBarrierPath is the go-qrdb transport's path for a tenant
// barrier verb.
func wireTenantBarrierPath(tenantID, verb string) string {
	return fmt.Sprintf("/v1/tenants/%s/barrier/%s", tenantID, verb)
}

// wireTenantBarrierStatus reads tenantID's barrier status through the root
// mount and decodes go-qrdb's own response DTO.
func wireTenantBarrierStatus(t *testing.T, tenantID, bearer string) qrdbtransport.TenantBarrierStatusResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
	defer cancel()
	status, body, err := doREST(ctx, http.MethodGet, wireTenantBarrierPath(tenantID, "status"), bearer, nil)
	require.NoErrorf(t, err, "GET %s: network error", wireTenantBarrierPath(tenantID, "status"))
	require.NotEqualf(t, http.StatusNotFound, status,
		"GET %s 404'd: the per-tenant barrier is not mounted at the root /v1/tenants path (#995); body=%s",
		wireTenantBarrierPath(tenantID, "status"), body)
	require.Equalf(t, http.StatusOK, status, "GET %s; body=%s", wireTenantBarrierPath(tenantID, "status"), body)
	var resp qrdbtransport.TenantBarrierStatusResponse
	require.NoErrorf(t, json.Unmarshal(body, &resp), "decode wire status: %s", body)
	return resp
}

// TestTenantBarrier_RootMountStatusMatchesAPIMirror proves the root mount
// serves the same tenant's barrier as the /api/v1 mirror.
func TestTenantBarrier_RootMountStatusMatchesAPIMirror(t *testing.T) {
	tenantID := fmt.Sprintf("tb-root-status-%d", time.Now().UnixNano())
	bearer := tenantAdminBearer(t, tenantID)
	initNoneBarrier(t, tenantID, bearer)

	wire := wireTenantBarrierStatus(t, tenantID, bearer)
	require.True(t, wire.Initialized, "root-mount status: barrier initialized")
	require.Equal(t, "none", wire.Strategy, "root-mount status: strategy")

	ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
	defer cancel()
	status, api, body, err := statusTenantBarrier(ctx, tenantID, bearer)
	require.NoError(t, err)
	require.Equalf(t, http.StatusOK, status, "/api/v1 status; body=%s", body)
	require.NotNil(t, api)
	require.Equal(t, api.Sealed, wire.Sealed, "root mount and /api/v1 mirror report the same sealed state")
	require.Equal(t, api.Initialized, wire.Initialized, "root mount and /api/v1 mirror report the same initialized state")
}

// TestTenantBarrier_RootMountInit proves `tenant barrier init --transport
// rest` lands: an init sent the way go-qrdb's transport sends it initializes
// the tenant's barrier.
func TestTenantBarrier_RootMountInit(t *testing.T) {
	tenantID := fmt.Sprintf("tb-root-init-%d", time.Now().UnixNano())
	bearer := tenantAdminBearer(t, tenantID)

	ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
	defer cancel()
	req := qrdbtransport.TenantBarrierInitRequest{TenantID: tenantID, Strategy: "none"}
	status, body, err := doREST(ctx, http.MethodPost, wireTenantBarrierPath(tenantID, "init"), bearer, req)
	require.NoError(t, err)
	require.NotEqualf(t, http.StatusNotFound, status,
		"POST %s 404'd: the per-tenant barrier is not mounted at the root /v1/tenants path (#995); body=%s",
		wireTenantBarrierPath(tenantID, "init"), body)
	require.Truef(t, status >= 200 && status < 300, "POST %s: got %d; body=%s",
		wireTenantBarrierPath(tenantID, "init"), status, body)

	wire := wireTenantBarrierStatus(t, tenantID, bearer)
	require.True(t, wire.Initialized, "init through the root mount initializes the tenant's barrier")
}

// TestTenantBarrier_RootMountIsTenantIsolated proves one tenant's admin cannot
// read another tenant's barrier through the root mount.
func TestTenantBarrier_RootMountIsTenantIsolated(t *testing.T) {
	ts := time.Now().UnixNano()
	own := fmt.Sprintf("tb-root-own-%d", ts)
	other := fmt.Sprintf("tb-root-other-%d", ts)
	ownBearer := tenantAdminBearer(t, own)
	otherBearer := tenantAdminBearer(t, other)
	initNoneBarrier(t, other, otherBearer)

	ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
	defer cancel()
	status, body, err := doREST(ctx, http.MethodGet, wireTenantBarrierPath(other, "status"), ownBearer, nil)
	require.NoError(t, err)
	require.Truef(t, status >= 400 && status < 500,
		"tenant %s's admin read tenant %s's barrier through the root mount: got %d; body=%s", own, other, status, body)
}
