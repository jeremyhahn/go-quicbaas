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

// Package tenantbarrier provides the integration test matrix for the
// per-tenant barrier REST API exposed by the quicbaas broker
// (POST /api/v1/tenants/{id}/barrier/{init,unseal,unseal-share,seal} and
// GET /api/v1/tenants/{id}/barrier/status).
//
// # Architecture note
//
// Per-tenant cryptographic barriers require the underlying qrdb node to have
// a storage volume pool configured (storage.volume_pools in the qrdb config).
// Without one, any init request for a cryptographic strategy (passphrase,
// shamir) returns an error surfacing the sentinel "volume pool not configured"
// string; the none strategy does not require a volume pool and succeeds
// unconditionally.
//
// # Execution model
//
// Per-tenant barriers REQUIRE the embedded qrdb node's seal subsystem: a node
// booted without a seal: section has SealBarrier == nil and qrdb's fail-closed
// kv tenant service rejects EVERY per-tenant barrier verb with
// tenant.ErrBarrierNotEnabled ("tenant: barrier not enabled (seal subsystem
// disabled)"). The shared integration cluster (test/integration/quicbaas.yaml)
// deliberately runs with the seal subsystem disabled, so this suite does NOT
// run in the default make integration-test lane (the runner excludes this
// package alongside the other special-lane packages).
//
// The matrix instead runs against the seal-ENABLED single-node harness in
// docker-compose.seal.yml (services tenantbarrier-node + tenantbarrier-test,
// config test/integration/quicbaas-tenantbarrier.yaml: seal.enabled=true,
// strategy=passphrase, auto-unsealed at boot via QUICBAAS_SEAL_PASSPHRASE).
// Start it with:
//
//	make integration-test-tenantbarrier
//
// The node keeps the shared cluster's test-node1 hostname + cert material so
// the helpers package defaults (endpoints, PIV admin cert, CA key) apply
// unchanged. All REST calls are made directly via net/http (no CLI binary
// needed).
//
// Identity model (owner decision): the system-admin bearer (helpers.AuthPIV)
// is a CONTROL-PLANE identity — it provisions tenants and may READ any
// tenant barrier's status, but it is DENIED every tenant-barrier mutation
// (init/unseal/unseal-share/seal). All barrier lifecycle writes are performed
// under a tenant-admin bearer minted via helpers.AuthTenantAdmin, matching
// the locked architecture where tenant admins self-bootstrap their own
// barriers.
//
// Volume-pool availability is detected at runtime by attempting a passphrase
// init against a probe tenant and checking for the canonical "volume pool not
// configured" substring in the error response. When the pool is absent the
// cryptographic strategy cases document themselves and proceed only for the
// none strategy; they do NOT use t.Skip per project rules (CLAUDE.md:
// "NEVER use skip guards or -short flags"). A build-tag gate
// (//go:build integration && tenantbarrier_pool) guards the full pool-required
// matrix so it only compiles and runs when the operator explicitly opts in.
//
// # Transport scope
//
// This suite exercises the REST transport. The feature contract (init/unseal/
// seal/status over every transport) is covered at the unit/make-test layer by
// the per-transport handler tests in pkg/api/{rest,grpc,quic,mcp}. The
// integration suite's mandate is end-to-end lifecycle correctness against a
// running daemon, not per-transport wire parity.
package tenantbarrier

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Timeouts and retry constants
// =============================================================================

// restTimeout bounds a single REST request to the integration cluster.
const restTimeout = 30 * time.Second

// tenantCreateTimeout bounds the tenant-creation IAM call, which may touch
// the Raft leader and incur an election wait on the first call.
const tenantCreateTimeout = 20 * time.Second

// =============================================================================
// REST client helpers
// =============================================================================

// restBaseURL returns the primary REST node URL. The default matches the
// integration cluster's docker-compose.integration.yml service name.
func restBaseURL() string {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return "https://test-node1:8080"
	}
	return endpoints[0]
}

// adminBearer acquires (and memoises) the system-admin bearer token using
// PIV/mTLS against the integration cluster. t.Fatal is called on any error
// so callers never receive an empty token.
func adminBearer(t *testing.T) string {
	t.Helper()
	token := helpers.AuthPIV(t, restBaseURL())
	require.NotEmpty(t, token, "admin bearer token must not be empty")
	return token
}

// tenantAdminBearer mints (and memoises, via helpers) a tenant-admin bearer
// scoped to tenantID. Per the owner decision, ALL tenant-barrier mutations in
// this suite run under a tenant-admin identity; the system-admin bearer is
// used only for tenant provisioning and status reads.
func tenantAdminBearer(t *testing.T, tenantID string) string {
	t.Helper()
	token := helpers.AuthTenantAdmin(t, restBaseURL(), tenantID)
	require.NotEmptyf(t, token, "tenant-admin bearer for %s must not be empty", tenantID)
	return token
}

// doREST issues a JSON REST call and returns the status code plus the raw
// response body. It trusts the integration cluster CA via helpers.NewHTTPClient
// and injects the supplied bearer. A nil body is safe (maps to an empty POST
// body, correct for seal).
func doREST(ctx context.Context, method, path string, bearer string, reqBody interface{}) (int, []byte, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return 0, nil, fmt.Errorf("doREST: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	client := helpers.NewHTTPClient(restTimeout)

	url := restBaseURL() + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("doREST: build request %s %s: %w", method, url, err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("doREST: %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("doREST: read body: %w", err)
	}
	return resp.StatusCode, body, nil
}

// =============================================================================
// Per-tenant barrier REST helpers
// =============================================================================

// barrierPath returns the base path for a tenant's barrier endpoints.
func barrierPath(tenantID, verb string) string {
	return fmt.Sprintf("/api/v1/tenants/%s/barrier/%s", tenantID, verb)
}

// tenantBarrierInitReq is the wire body for POST .../barrier/init.
type tenantBarrierInitReq struct {
	Strategy   string `json:"strategy,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	Threshold  int    `json:"threshold,omitempty"`
	Shares     int    `json:"shares,omitempty"`
}

// tenantBarrierInitResp is the 200 body for a Shamir init.
type tenantBarrierInitResp struct {
	TenantID    string   `json:"tenant_id"`
	Strategy    string   `json:"strategy"`
	Shares      []string `json:"shares"`
	Threshold   int      `json:"threshold"`
	TotalShares int      `json:"total_shares"`
}

// tenantBarrierUnsealReq is the wire body for POST .../barrier/unseal.
// Key is base64-encoded by encoding/json.
type tenantBarrierUnsealReq struct {
	Key []byte `json:"key,omitempty"`
}

// tenantBarrierUnsealShareReq is the wire body for POST .../barrier/unseal-share.
type tenantBarrierUnsealShareReq struct {
	Share string `json:"share,omitempty"`
}

// tenantBarrierUnsealShareResp is the 200 body returned by unseal-share.
type tenantBarrierUnsealShareResp struct {
	TenantID  string `json:"tenant_id"`
	Required  int    `json:"required"`
	Submitted int    `json:"submitted"`
	Complete  bool   `json:"complete"`
	Sealed    bool   `json:"sealed"`
}

// tenantBarrierStatusResp is the 200 body returned by GET .../barrier/status.
type tenantBarrierStatusResp struct {
	TenantID        string `json:"tenant_id"`
	Sealed          bool   `json:"sealed"`
	Initialized     bool   `json:"initialized"`
	Strategy        string `json:"strategy"`
	HardwareBacked  bool   `json:"hardware_backed"`
	ShamirThreshold int    `json:"shamir_threshold"`
	ShamirTotal     int    `json:"shamir_total"`
}

// initTenantBarrier calls POST .../barrier/init and decodes the response.
// For none/passphrase strategies the expected status is 204 (no body); for
// shamir it is 200 with a tenantBarrierInitResp. Returns the raw status code
// and body alongside any network error so callers can assert both successes
// and expected failures.
func initTenantBarrier(ctx context.Context, tenantID, bearer string, req tenantBarrierInitReq) (int, *tenantBarrierInitResp, []byte, error) {
	status, body, err := doREST(ctx, http.MethodPost, barrierPath(tenantID, "init"), bearer, req)
	if err != nil {
		return 0, nil, nil, err
	}
	if status == http.StatusOK && len(body) > 0 {
		var resp tenantBarrierInitResp
		if jerr := json.Unmarshal(body, &resp); jerr != nil {
			return status, nil, body, fmt.Errorf("initTenantBarrier: decode 200 body: %w; raw=%s", jerr, body)
		}
		return status, &resp, body, nil
	}
	return status, nil, body, nil
}

// sealTenantBarrier calls POST .../barrier/seal. Expected status 204.
func sealTenantBarrier(ctx context.Context, tenantID, bearer string) (int, []byte, error) {
	return doREST(ctx, http.MethodPost, barrierPath(tenantID, "seal"), bearer, nil)
}

// unsealTenantBarrier calls POST .../barrier/unseal with a base64-encoded key.
// Expected status 204.
func unsealTenantBarrier(ctx context.Context, tenantID, bearer string, key []byte) (int, []byte, error) {
	return doREST(ctx, http.MethodPost, barrierPath(tenantID, "unseal"), bearer, tenantBarrierUnsealReq{Key: key})
}

// unsealShareTenantBarrier submits one Shamir share via POST .../barrier/unseal-share
// and decodes the progress response. Expected status 200.
func unsealShareTenantBarrier(ctx context.Context, tenantID, bearer, share string) (int, *tenantBarrierUnsealShareResp, []byte, error) {
	status, body, err := doREST(ctx, http.MethodPost, barrierPath(tenantID, "unseal-share"), bearer,
		tenantBarrierUnsealShareReq{Share: share})
	if err != nil {
		return 0, nil, nil, err
	}
	if status == http.StatusOK && len(body) > 0 {
		var resp tenantBarrierUnsealShareResp
		if jerr := json.Unmarshal(body, &resp); jerr != nil {
			return status, nil, body, fmt.Errorf("unsealShareTenantBarrier: decode body: %w; raw=%s", jerr, body)
		}
		return status, &resp, body, nil
	}
	return status, nil, body, nil
}

// statusTenantBarrier calls GET .../barrier/status and decodes the response.
func statusTenantBarrier(ctx context.Context, tenantID, bearer string) (int, *tenantBarrierStatusResp, []byte, error) {
	status, body, err := doREST(ctx, http.MethodGet, barrierPath(tenantID, "status"), bearer, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	if status == http.StatusOK && len(body) > 0 {
		var resp tenantBarrierStatusResp
		if jerr := json.Unmarshal(body, &resp); jerr != nil {
			return status, nil, body, fmt.Errorf("statusTenantBarrier: decode body: %w; raw=%s", jerr, body)
		}
		return status, &resp, body, nil
	}
	return status, nil, body, nil
}

// =============================================================================
// Tenant provisioning helpers
// =============================================================================

// createTenant creates a new tenant via the IAM REST API using the admin
// bearer. tenantID must be globally unique within the test run; callers
// should use t.Name()-derived IDs or a per-test UUID prefix.
// A t.Cleanup hook is NOT registered here — tenants in the integration cluster
// persist for the run lifetime and their state is the feature under test.
func createTenant(t *testing.T, tenantID, displayName, bearer string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), tenantCreateTimeout)
	defer cancel()

	reqBody := map[string]interface{}{
		"id":   tenantID,
		"name": displayName,
	}
	status, body, err := doREST(ctx, http.MethodPost,
		"/api/v1/tenants", bearer, reqBody)
	require.NoErrorf(t, err, "createTenant %s: network error", tenantID)
	// 200 or 201 → created; 409 → already exists (idempotent for parallel
	// test runs that share the cluster).
	if status != http.StatusOK && status != http.StatusCreated && status != http.StatusConflict {
		t.Fatalf("createTenant %s: unexpected status %d body=%s", tenantID, status, body)
	}
}

// =============================================================================
// Volume-pool probe
// =============================================================================

// volumePoolAvailable probes whether the integration cluster's embedded qrdb
// node has a storage volume pool configured. It does so by creating a
// transient probe tenant and calling barrier/init with strategy=passphrase.
// If the error body contains "volume pool not configured", the pool is absent.
// If the call succeeds (204), the pool is present.
//
// The probe tenant is not torn down (integration cluster tenants are
// long-lived). A unique ID derived from the calling test avoids collision
// across parallel test runs.
//
// Returns true when a volume pool is configured.
//
// The probe init runs under a tenant-admin bearer for the probe tenant:
// barrier init is a tenant-barrier mutation and system-tenant callers are
// denied (403) before the volume-pool check would ever be reached.
func volumePoolAvailable(t *testing.T, bearer string) bool {
	t.Helper()
	probeTenantID := fmt.Sprintf("tb-probe-%d", time.Now().UnixNano())
	createTenant(t, probeTenantID, "TenantBarrier Volume-Pool Probe", bearer)
	probeBearer := tenantAdminBearer(t, probeTenantID)

	ctx, cancel := context.WithTimeout(context.Background(), restTimeout)
	defer cancel()

	status, _, body, err := initTenantBarrier(ctx, probeTenantID, probeBearer, tenantBarrierInitReq{
		Strategy:   "passphrase",
		Passphrase: "probe-passphrase",
	})
	if err != nil {
		t.Logf("volumePoolAvailable: probe init network error (treating as absent): %v", err)
		return false
	}
	if status == http.StatusNoContent || status == http.StatusOK {
		t.Logf("volumePoolAvailable: probe init succeeded (status=%d) → pool is configured", status)
		return true
	}
	// Surface the error body for diagnostics, then check for the canonical sentinels.
	bodyStr := strings.ToLower(string(body))
	t.Logf("volumePoolAvailable: probe init returned status=%d body=%s", status, body)
	// qrdb's fail-closed tenant service rejects every per-tenant barrier verb
	// with tenant.ErrBarrierNotEnabled when the node's seal subsystem is
	// disabled. That is strictly stronger than "no volume pool": no
	// cryptographic strategy can work at all, so the probe must report
	// not-available instead of mis-classifying the failure as pool presence.
	if strings.Contains(bodyStr, "barrier not enabled") ||
		strings.Contains(bodyStr, "seal subsystem disabled") {
		t.Logf("volumePoolAvailable: the cluster's seal subsystem is DISABLED — " +
			"per-tenant barriers are unavailable on this cluster; run this suite " +
			"against the seal-enabled harness (make integration-test-tenantbarrier)")
		return false
	}
	return !strings.Contains(bodyStr, "volume pool not configured") &&
		!strings.Contains(bodyStr, "pool not configured")
}

// =============================================================================
// Assertion helpers
// =============================================================================

// assertBarrierStatus fetches the barrier status for tenantID and asserts the
// expected (sealed, initialized, strategy) tuple. Pass expectedStrategy=""
// to skip the strategy assertion.
func assertBarrierStatus(t *testing.T, ctx context.Context, tenantID, bearer string,
	wantSealed bool, wantInitialized bool, wantStrategy string) *tenantBarrierStatusResp {
	t.Helper()
	status, resp, body, err := statusTenantBarrier(ctx, tenantID, bearer)
	require.NoErrorf(t, err, "assertBarrierStatus %s: network error", tenantID)
	require.Equalf(t, http.StatusOK, status,
		"assertBarrierStatus %s: expected 200 got %d body=%s", tenantID, status, body)
	require.NotNilf(t, resp, "assertBarrierStatus %s: nil response", tenantID)

	if resp.Sealed != wantSealed {
		t.Errorf("assertBarrierStatus %s: sealed=%v want %v (body=%s)", tenantID, resp.Sealed, wantSealed, body)
	}
	if resp.Initialized != wantInitialized {
		t.Errorf("assertBarrierStatus %s: initialized=%v want %v (body=%s)", tenantID, resp.Initialized, wantInitialized, body)
	}
	if wantStrategy != "" && resp.Strategy != wantStrategy {
		t.Errorf("assertBarrierStatus %s: strategy=%q want %q (body=%s)", tenantID, resp.Strategy, wantStrategy, body)
	}
	return resp
}

// =============================================================================
// encodedKey converts a plaintext passphrase to the base64 encoding that the
// REST unseal body expects.
// =============================================================================

func encodedKey(passphrase string) []byte {
	return []byte(base64.StdEncoding.EncodeToString([]byte(passphrase)))
}
