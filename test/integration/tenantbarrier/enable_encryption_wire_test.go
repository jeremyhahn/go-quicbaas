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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// go-quicbaas#990 — who may drive POST /v1/barrier/enable-encryption.
//
// The go-qrdb wire mount takes its scope from a body tenant_id, so it is
// registered identity-gated and the handler authorizes the scope the body
// selected through the running node's own composed authorizer
// (pkg/server/restBarrierScopeAuthorizer). These cases drive the daemon with
// real identities — a tenant-admin bearer, the system-admin PIV bearer — and
// the request body is go-qrdb's own client DTO, so what is pinned here is what
// go-qrdb's REST transport meets in production.
//
// The adapter's delegation and failure handling are unit-tested in memory in
// pkg/server/rest_barrier_scope_authorizer_test.go.
// =============================================================================

// wireEnableEncryptionPath is the go-qrdb wire mount, served on the same REST
// listener as /api/v1.
const wireEnableEncryptionPath = "/v1/barrier/enable-encryption"

// wirePassphraseRequest is a passphrase-target migration request as go-qrdb's
// SDK marshals it. An empty tenantID selects the system barrier.
func wirePassphraseRequest(tenantID string) qrdbtransport.BarrierEnableEncryptionRequest {
	return qrdbtransport.BarrierEnableEncryptionRequest{
		TenantID:   tenantID,
		Strategy:   "passphrase",
		Passphrase: []byte("enable-encryption-wire-integration-passphrase"),
	}
}

// initNoneBarrier initializes tenantID's barrier with the none strategy under
// that tenant's own admin bearer: the only state enable-encryption migrates
// from.
func initNoneBarrier(t *testing.T, tenantID, bearer string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
	defer cancel()
	status, _, body, err := initTenantBarrier(ctx, tenantID, bearer, tenantBarrierInitReq{Strategy: "none"})
	require.NoErrorf(t, err, "init none barrier for %s: network error", tenantID)
	require.Equalf(t, http.StatusNoContent, status, "init none barrier for %s; body=%s", tenantID, body)
}

// TestTenantBarrier_WireEnableEncryptionScope pins which barrier each caller
// may migrate through the go-qrdb wire mount, and that a refused request
// migrates nothing.
func TestTenantBarrier_WireEnableEncryptionScope(t *testing.T) {
	adminToken := adminBearer(t)
	ts := fmt.Sprintf("%d", time.Now().UnixNano())

	own := "tb-ee-own-" + ts
	other := "tb-ee-other-" + ts
	ownBearer := tenantAdminBearer(t, own)
	otherBearer := tenantAdminBearer(t, other)
	initNoneBarrier(t, own, ownBearer)
	initNoneBarrier(t, other, otherBearer)

	// The refusals run first and target the other tenant's barrier (or the
	// system barrier), so none of them can be satisfied by the one migration
	// this test admits.
	refusals := []struct {
		name   string
		bearer string
		req    qrdbtransport.BarrierEnableEncryptionRequest
		want   []int
	}{
		{
			name:   "tenant_admin_cannot_migrate_the_system_barrier",
			bearer: ownBearer, req: wirePassphraseRequest(""),
			want: []int{http.StatusForbidden},
		},
		{
			name:   "tenant_admin_cannot_migrate_another_tenant",
			bearer: ownBearer, req: wirePassphraseRequest(other),
			want: []int{http.StatusForbidden},
		},
		{
			// System-tenant callers hold no per-tenant key: every tenant
			// barrier mutation is refused to them, as on the path-scoped
			// routes (TestTenantBarrier_SystemTenantPosture).
			name:   "system_admin_cannot_migrate_a_tenant_barrier",
			bearer: adminToken, req: wirePassphraseRequest(other),
			want: []int{http.StatusForbidden, http.StatusUnauthorized},
		},
		{
			name:   "an_unverifiable_bearer_is_refused",
			bearer: "forged-" + ts, req: wirePassphraseRequest(other),
			want: []int{http.StatusUnauthorized},
		},
		{
			name:   "no_credential_is_refused",
			bearer: "", req: wirePassphraseRequest(other),
			want: []int{http.StatusUnauthorized},
		},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
			defer cancel()
			status, body, err := doREST(ctx, http.MethodPost, wireEnableEncryptionPath, tc.bearer, tc.req)
			require.NoError(t, err, "POST %s: network error", wireEnableEncryptionPath)
			assert.Containsf(t, tc.want, status, "status=%d body=%s", status, body)
		})
	}

	t.Run("refused_requests_left_the_other_tenant_unmigrated", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
		defer cancel()
		assertBarrierStatus(t, ctx, other, otherBearer, false, true, "none")
	})

	t.Run("tenant_admin_migrates_its_own_tenant", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
		defer cancel()
		status, body, err := doREST(ctx, http.MethodPost, wireEnableEncryptionPath, ownBearer, wirePassphraseRequest(own))
		require.NoError(t, err, "POST %s: network error", wireEnableEncryptionPath)
		require.Equalf(t, http.StatusOK, status, "body=%s", body)

		// The answer is the shape go-qrdb's REST transport decodes.
		var resp qrdbtransport.BarrierEnableEncryptionResponse
		require.NoErrorf(t, json.Unmarshal(body, &resp), "decode the go-qrdb response; body=%s", body)
		assert.Equal(t, "migration_started", resp.Status)
		assert.Equal(t, "tenant", resp.Scope)
	})
}
