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

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The daemon's pooled-account resolver, end to end against a FAKE CENTRAL.
//
// The fake is an httptest server speaking the real v1 REST shapes, not a
// double for the client: what these tests are actually about is whether the
// daemon's stack behaves like the control plane's when the only thing that
// changed underneath it is where the records come from. A double for the
// client would assert the daemon calls itself correctly and prove nothing
// about that.
//
// NO TEST HERE ASSERTS A CREDENTIAL BY VALUE ANYWHERE IT COULD BE LOGGED.
// Assertions are on shape (which field is populated, which env key it lands
// in) and on call counts.
// =============================================================================

// fakeCentral is a stand-in control plane serving /api/v1/agent-accounts.
type fakeCentral struct {
	mu sync.Mutex

	// accounts is the pool, keyed by account ID.
	accounts map[string]*fakeCentralAccount

	// tenantID is the only tenant this fake serves.
	tenantID string

	// releases counts materialize calls per account.
	releases map[string]int

	// lists / gets count metadata reads.
	lists int
	gets  int

	// unauthorizedOnce refuses the next call with 401, once, to exercise the
	// token-refresh retry.
	unauthorizedOnce bool

	// usage is the token ledger the least-usage strategy balances on.
	usage map[string]int64

	// presentedLeases records, in order, the delivery grant each materialize
	// call presented (empty for a first release). It is what proves the daemon
	// re-presents the grant it holds rather than silently asking for a new one.
	presentedLeases []string

	// revokedLeases are grants the central refuses to release under, standing
	// in for the session conclusion having withdrawn them.
	revokedLeases map[string]bool

	// unleased makes every release answer with no grant, standing in for a
	// control plane with no lease manager wired.
	unleased bool

	// leasePerRelease issues a distinct grant for every first release
	// ("lease_<account>_<n>"), which is what a real control plane does for
	// single-use releases and what lets a test tell two spawns' grants apart.
	leasePerRelease bool

	server *httptest.Server
}

// fakeCentralAccount is one pooled account plus its releasable envelope.
type fakeCentralAccount struct {
	record    *orchestration.Account
	token     string
	envKey    string
	expiresAt time.Time

	// refusalStatus / refusalBody, when set, are the answer the central gives
	// instead of releasing — the shape a revoked, fenced or errored account
	// produces.
	refusalStatus int
	refusalBody   string

	// singleUse marks the account as reading its credential from a tenant
	// secret (credential_secret_ref): the central says so on every release.
	singleUse bool
}

// newFakeCentral starts a fake control plane and registers its shutdown.
func newFakeCentral(t *testing.T, tenantID string) *fakeCentral {
	t.Helper()
	c := &fakeCentral{
		accounts:      map[string]*fakeCentralAccount{},
		tenantID:      tenantID,
		releases:      map[string]int{},
		usage:         map[string]int64{},
		revokedLeases: map[string]bool{},
	}
	c.server = httptest.NewServer(http.HandlerFunc(c.serve))
	t.Cleanup(c.server.Close)
	return c
}

// addAccount registers one releasable static-token account.
func (c *fakeCentral) addAccount(id, provider, projectID string, isDefault bool) *fakeCentralAccount {
	c.mu.Lock()
	defer c.mu.Unlock()
	account := &fakeCentralAccount{
		record: &orchestration.Account{
			ID:            id,
			TenantID:      c.tenantID,
			ProjectID:     projectID,
			Provider:      orchestration.AccountProvider(provider),
			DisplayName:   id + "-seat",
			Default:       isDefault,
			Status:        orchestration.AccountStatusActive,
			RefreshPolicy: orchestration.AccountCredentialModeStaticToken,
			CreatedAt:     time.Now().UTC().Add(-time.Hour),
			UpdatedAt:     time.Now().UTC().Add(-time.Hour),
		},
		token:  "token-for-" + id,
		envKey: "CLAUDE_CODE_OAUTH_TOKEN",
	}
	c.accounts[id] = account
	return account
}

// rotate simulates an operator rotating a credential on the central: the
// envelope changes and the record's UpdatedAt advances.
func (c *fakeCentral) rotate(id, newToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	account, ok := c.accounts[id]
	if !ok {
		return
	}
	account.token = newToken
	// A rotation stamps MarkRefreshed on the central, which is what advances
	// LastRefreshedAt — the signal the daemon's cache keys on.
	account.record.LastRefreshedAt = time.Now().UTC().Add(time.Second)
	account.record.UpdatedAt = account.record.LastRefreshedAt
}

// writeReferencedSecret simulates an operator writing the tenant secret a
// referenced account names: the envelope changes and NOTHING on the account
// record moves — neither UpdatedAt nor LastRefreshedAt.
func (c *fakeCentral) writeReferencedSecret(id, newToken string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if account, ok := c.accounts[id]; ok {
		account.token = newToken
	}
}

// releaseCount reports how many times an account's envelope was released.
func (c *fakeCentral) releaseCount(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releases[id]
}

// serve routes the four verbs the daemon uses.
func (c *fakeCentral) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	if c.unauthorizedOnce {
		c.unauthorizedOnce = false
		c.mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	c.mu.Unlock()

	if r.URL.Query().Get("tenant_id") != c.tenantID {
		http.Error(w, `{"error":"tenant_id must be the caller's own tenant"}`, http.StatusForbidden)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agent-accounts")
	switch {
	case path == "" || path == "/":
		c.handleList(w, r)
	case strings.HasSuffix(path, "/materialize"):
		c.handleMaterialize(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/materialize"))
	case strings.HasSuffix(path, "/usage"):
		c.handleUsage(w, strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/usage"))
	default:
		c.handleGet(w, strings.TrimPrefix(path, "/"))
	}
}

// EVERY READ SNAPSHOTS THE RECORD BY VALUE UNDER THE LOCK, and marshals the
// copy after releasing it.
//
// The fake serves concurrent requests (httptest gives each connection its own
// goroutine) and a release MUTATES the record it answers from — that is the
// point of the usage stamp below. Handing the live *orchestration.Account to
// the JSON encoder after unlocking therefore raced a concurrent release, which
// the race detector caught in CI. Copying is enough: Account is a flat struct
// of strings, bools and times, so a value copy shares nothing with the fake.
func (c *fakeCentral) handleList(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	projectID := r.URL.Query().Get("project_id")

	c.mu.Lock()
	c.lists++
	records := make([]orchestration.Account, 0, len(c.accounts))
	for _, account := range c.accounts {
		if provider != "" && string(account.record.Provider) != provider {
			continue
		}
		if projectID != "" && account.record.ProjectID != projectID {
			continue
		}
		records = append(records, *account.record)
	}
	c.mu.Unlock()

	writeJSON(w, map[string]interface{}{"accounts": records, "total": len(records)})
}

func (c *fakeCentral) handleGet(w http.ResponseWriter, id string) {
	c.mu.Lock()
	c.gets++
	account, ok := c.accounts[id]
	var record orchestration.Account
	if ok {
		record = *account.record
	}
	c.mu.Unlock()

	if !ok {
		http.Error(w, `{"error":"account not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"account": record})
}

func (c *fakeCentral) handleUsage(w http.ResponseWriter, id string) {
	c.mu.Lock()
	tokens := c.usage[id]
	c.mu.Unlock()
	writeJSON(w, map[string]interface{}{"usage": map[string]interface{}{"total_tokens": tokens}})
}

func (c *fakeCentral) handleMaterialize(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ProjectID string `json:"project_id"`
		JobID     string `json:"job_id"`
		LeaseID   string `json:"lease_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	c.mu.Lock()
	c.presentedLeases = append(c.presentedLeases, body.LeaseID)
	leaseRevoked := body.LeaseID != "" && c.revokedLeases[body.LeaseID]
	// Resolved under the lock: the response is written after it is released,
	// and reading the lease configuration there would be a data race.
	grantedLease := c.leaseIDFor(id, body.LeaseID)
	account, ok := c.accounts[id]
	var (
		record        orchestration.Account
		token, envKey string
		expiresAt     time.Time
		refusalStatus int
		refusalBody   string
		singleUse     bool
	)
	if ok {
		c.releases[id]++
		// The release stamps usage on the central, exactly as the production
		// service does, so the record's UpdatedAt moves on every release. That
		// is precisely why the daemon's cache must not key on it.
		account.record.UpdatedAt = time.Now().UTC()
		account.record.LastUsedAt = account.record.UpdatedAt
		record = *account.record
		token, envKey, expiresAt = account.token, account.envKey, account.expiresAt
		refusalStatus, refusalBody = account.refusalStatus, account.refusalBody
		singleUse = account.singleUse
	}
	c.mu.Unlock()

	if !ok {
		http.Error(w, `{"error":"account not found"}`, http.StatusNotFound)
		return
	}
	if refusalStatus != 0 {
		http.Error(w, refusalBody, refusalStatus)
		return
	}
	// The server-side enforcement point, in miniature: a refresh under a
	// withdrawn grant is refused, whatever the daemon believes.
	if leaseRevoked {
		http.Error(w, `{"error":"account credential lease was revoked"}`, http.StatusForbidden)
		return
	}

	writeJSON(w, map[string]interface{}{
		"account_id":     id,
		"provider":       string(record.Provider),
		"project_id":     record.ProjectID,
		"refresh_policy": string(record.RefreshPolicy),
		"credential": map[string]interface{}{
			"access_token":         token,
			"access_token_env_key": envKey,
			"expires_at":           expiresAt,
		},
		"expires_at":        expiresAt,
		"updated_at":        record.UpdatedAt,
		"last_refreshed_at": record.LastRefreshedAt,
		"lease_id":          grantedLease,
		"lease_expires_at":  time.Now().Add(time.Hour),
		"single_use":        singleUse,
	})
}

// leaseIDFor answers with the grant the caller presented, or mints one for a
// first release — the shape the real verb has. An unleased central answers with
// none, which is what a control plane with no lease manager does.
//
// The caller holds c.mu.
func (c *fakeCentral) leaseIDFor(accountID, presented string) string {
	if c.unleased {
		return ""
	}
	if presented != "" {
		return presented
	}
	if c.leasePerRelease {
		return "lease_" + accountID + "_" + strconv.Itoa(c.releases[accountID])
	}
	return "lease_" + accountID
}

// leaseNothing makes the central release without issuing a grant, standing in
// for a control plane whose lease manager is unavailable (the sealed window).
func (c *fakeCentral) leaseNothing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unleased = true
}

// revokeLease marks a grant withdrawn, as the session reaper does.
func (c *fakeCentral) revokeLease(leaseID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.revokedLeases[leaseID] = true
}

// leasesPresented returns the grants materialize calls have presented so far.
func (c *fakeCentral) leasesPresented() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.presentedLeases...)
}

// writeJSON writes one JSON body.
func writeJSON(w http.ResponseWriter, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(err)
	}
}

// staticTokenSource is the daemon's bearer supply in these tests. It counts
// invalidations so the 401-retry path can be observed.
type staticTokenSource struct {
	mu            sync.Mutex
	invalidations int
}

func (s *staticTokenSource) Token(context.Context) (string, error) { return "bearer-token", nil }

func (s *staticTokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidations++
}

func (s *staticTokenSource) invalidationCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invalidations
}

// newTestAccountPool composes the daemon's resolver against a fake central.
func newTestAccountPool(t *testing.T, central *fakeCentral, tokens TokenSource) *AccountPool {
	t.Helper()
	pool, err := NewAccountPool(AccountPoolConfig{
		ServerAddr: central.server.URL,
		TenantID:   central.tenantID,
		HTTPClient: central.server.Client(),
		Tokens:     tokens,
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewAccountPool() error = %v", err)
	}
	return pool
}

// =============================================================================
// Construction
// =============================================================================

// TestNewAccountPool_RefusesAnIncompleteConfiguration proves a wiring mistake
// fails at startup rather than as a per-spawn "no account available", which
// reads like an empty pool and sends an operator to the wrong host.
func TestNewAccountPool_RefusesAnIncompleteConfiguration(t *testing.T) {
	base := AccountPoolConfig{
		ServerAddr: "https://control.invalid",
		TenantID:   "tenant-a",
		HTTPClient: http.DefaultClient,
		Tokens:     &staticTokenSource{},
		Logger:     &testLogger{t: t},
	}
	cases := []struct {
		name    string
		mutate  func(*AccountPoolConfig)
		wantErr error
	}{
		{"no server", func(c *AccountPoolConfig) { c.ServerAddr = "" }, ErrAccountClientNoServer},
		{"no tenant", func(c *AccountPoolConfig) { c.TenantID = "" }, ErrAccountClientNoTenant},
		{"no http client", func(c *AccountPoolConfig) { c.HTTPClient = nil }, ErrAccountClientNoHTTPClient},
		{"no tokens", func(c *AccountPoolConfig) { c.Tokens = nil }, ErrAccountClientNoTokens},
		{"no logger", func(c *AccountPoolConfig) { c.Logger = nil }, ErrAccountClientNoLogger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			pool, err := NewAccountPool(cfg)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewAccountPool() error = %v, want %v", err, tc.wantErr)
			}
			if pool != nil {
				t.Error("NewAccountPool returned a pool alongside an error")
			}
		})
	}
}

// =============================================================================
// Resolution
// =============================================================================

// TestAccountPool_ResolvesAPinnedAccount is the case the burn-in blocked on: a
// spawn naming an account ID resolves to injectable credential material on the
// worker host.
func TestAccountPool_ResolvesAPinnedAccount(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID:  "tenant-a",
		Provider:  orchestration.AccountProviderClaude,
		AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-1" {
		t.Errorf("resolved account %q, want acct-1", resolved.AccountID)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] == "" {
		t.Error("resolution projected no token into the provider's environment variable")
	}
	if resolved.Env["QUICBAAS_ACCOUNT_ID"] != "" {
		// The QUICBAAS_* markers are added by the ACP backend, not the
		// resolver; asserting their absence here keeps the two layers'
		// responsibilities honest.
		t.Error("resolver added the backend's own observability markers")
	}
	if central.releaseCount("acct-1") != 1 {
		t.Errorf("released %d times, want exactly 1", central.releaseCount("acct-1"))
	}
}

// TestAccountPool_ResolvesAutoThroughTheDefaultAccount proves strategy-based
// selection works over API-backed metadata exactly as it does over a KV.
func TestAccountPool_ResolvesAutoThroughTheDefaultAccount(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", false)
	central.addAccount("acct-default", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a",
		Provider: orchestration.AccountProviderClaude,
		Auto:     true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-default" {
		t.Errorf("auto selection chose %q, want the scope default acct-default", resolved.AccountID)
	}
}

// TestAccountPool_HonoursAllowAndExclude proves the rotation policy the central
// forwards in a Spawn is applied here, which is the whole reason the selectors
// travel instead of the credential.
func TestAccountPool_HonoursAllowAndExclude(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	central.addAccount("acct-2", "claude", "", false)
	pool := newTestAccountPool(t, central, &staticTokenSource{})
	ctx := context.Background()

	t.Run("allow narrows the pool", func(t *testing.T) {
		resolved, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
			TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true,
			AllowedAccountIDs: []string{"acct-2"},
		})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if resolved.AccountID != "acct-2" {
			t.Errorf("allow-listed resolution chose %q, want acct-2", resolved.AccountID)
		}
	})

	t.Run("exclude removes the default", func(t *testing.T) {
		resolved, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
			TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true,
			ExcludeAccountIDs: []string{"acct-1"},
		})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if resolved.AccountID != "acct-2" {
			t.Errorf("exclusion resolution chose %q, want acct-2", resolved.AccountID)
		}
	})

	t.Run("excluding everything is refused", func(t *testing.T) {
		_, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
			TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, Auto: true,
			ExcludeAccountIDs: []string{"acct-1", "acct-2"},
		})
		var unavailable *orchestration.NoAccountAvailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("error = %v (%T), want *NoAccountAvailableError", err, err)
		}
	})
}

// TestAccountPool_RegistersTheLeastUsageStrategy proves a spawn naming the
// strategy resolves rather than being refused "no account available" — which
// on the daemon would be indistinguishable from an empty pool.
func TestAccountPool_RegistersTheLeastUsageStrategy(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-busy", "claude", "", true)
	central.addAccount("acct-idle", "claude", "", false)
	central.usage["acct-busy"] = 1_000_000

	pool := newTestAccountPool(t, central, &staticTokenSource{})
	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID:     "tenant-a",
		Provider:     orchestration.AccountProviderClaude,
		Auto:         true,
		StrategyName: orchestration.LeastUsageAccountStrategyName,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-idle" {
		t.Errorf("least-usage selection chose %q, want acct-idle", resolved.AccountID)
	}
	if resolved.Strategy != orchestration.LeastUsageAccountStrategyName {
		t.Errorf("resolution recorded strategy %q, want %q",
			resolved.Strategy, orchestration.LeastUsageAccountStrategyName)
	}
}

// TestAccountPool_ServesRepeatSpawnsFromCache proves the cache exists: N spawns
// against one account produce ONE credential release, because each release is a
// real handout with its own audit event on the central.
func TestAccountPool_ServesRepeatSpawnsFromCache(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if _, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
			TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
		}); err != nil {
			t.Fatalf("Resolve() %d error = %v", i, err)
		}
	}
	if got := central.releaseCount("acct-1"); got != 1 {
		t.Errorf("released %d times across 4 spawns, want 1", got)
	}
	hits, misses := pool.Stats()
	if hits != 3 || misses != 1 {
		t.Errorf("cache hits/misses = %d/%d, want 3/1", hits, misses)
	}
}

// TestAccountPool_ReMaterializesAfterACentralRotation is the rotation contract:
// an operator replacing the credential on the central reaches the worker host
// on the very next spawn, without the daemon polling for secrets.
func TestAccountPool_ReMaterializesAfterACentralRotation(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})
	ctx := context.Background()

	first, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("first Resolve() error = %v", err)
	}

	central.rotate("acct-1", "rotated-token")

	second, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("post-rotation Resolve() error = %v", err)
	}

	if central.releaseCount("acct-1") != 2 {
		t.Errorf("released %d times, want 2 (one per side of the rotation)", central.releaseCount("acct-1"))
	}
	// Compared, never printed: the spawn must not keep using the retired
	// credential.
	if first.Env["CLAUDE_CODE_OAUTH_TOKEN"] == second.Env["CLAUDE_CODE_OAUTH_TOKEN"] {
		t.Error("post-rotation spawn reused the retired credential")
	}
}

// TestAccountPool_SecretWriteReachesTheNextSpawn is the rotation contract for
// a REFERENCED account (go-quicbaas#1023): writing its tenant secret moves no
// account timestamp, so a warm cache would have served the retired value for
// up to its whole window. A single-use release is re-released per spawn, and
// the very next spawn gets the replacement.
func TestAccountPool_SecretWriteReachesTheNextSpawn(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true).singleUse = true
	pool := newTestAccountPool(t, central, &staticTokenSource{})
	ctx := context.Background()
	req := orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	}

	first, err := pool.Resolver.Resolve(ctx, req)
	if err != nil {
		t.Fatalf("first Resolve() error = %v", err)
	}

	central.writeReferencedSecret("acct-1", "replacement-token")

	second, err := pool.Resolver.Resolve(ctx, req)
	if err != nil {
		t.Fatalf("post-write Resolve() error = %v", err)
	}
	// Compared, never printed.
	if first.Env["CLAUDE_CODE_OAUTH_TOKEN"] == second.Env["CLAUDE_CODE_OAUTH_TOKEN"] {
		t.Error("the spawn after a secret write reused the retired credential")
	}
	if second.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "replacement-token" {
		t.Error("the spawn after a secret write did not receive the replacement")
	}
	if got := central.releaseCount("acct-1"); got != 2 {
		t.Errorf("released %d times, want 2 (a single-use envelope is re-released per spawn)", got)
	}
}

// TestAccountPool_AResolutionReportsItsOwnGrantNotAConcurrentSpawns is the
// round-3 review finding on go-quicbaas#1028: a single-use release is stored
// already expired, so a concurrent spawn B of the same account releases again
// and replaces the cache entry between spawn A's lookup and A's grant report.
// Reading the grant back from the cache then made A report B's grant, leaving
// A's own release untracked by session teardown. Each resolution now carries
// the grant it was served under, so the interleaving is exercised
// deterministically: A looks up, B looks up and replaces, A reports.
func TestAccountPool_AResolutionReportsItsOwnGrantNotAConcurrentSpawns(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.leasePerRelease = true
	central.addAccount("acct-1", "claude", "", true).singleUse = true
	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: central.server.URL, TenantID: central.tenantID,
		HTTPClient: central.server.Client(), Tokens: &staticTokenSource{}, Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := &accountCredentialCache{}
	secrets := newMaterializingSecrets(client, cache, &testLogger{t: t})
	reporter := newDeliveryGrantReporter(cache)
	scope := orchestration.AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-1"}

	ctxA, _ := orchestration.WithDeliveryGrantSlot(context.Background())
	ctxB, _ := orchestration.WithDeliveryGrantSlot(context.Background())
	if _, err := secrets.resolve(ctxA, "acct-1"); err != nil {
		t.Fatalf("spawn A: %v", err)
	}
	if _, err := secrets.resolve(ctxB, "acct-1"); err != nil {
		t.Fatalf("spawn B: %v", err)
	}

	grantA, err := reporter.GrantDelivery(ctxA, scope)
	if err != nil || grantA == nil {
		t.Fatalf("spawn A's grant = %v, %v", grantA, err)
	}
	grantB, err := reporter.GrantDelivery(ctxB, scope)
	if err != nil || grantB == nil {
		t.Fatalf("spawn B's grant = %v, %v", grantB, err)
	}
	if grantA.LeaseID != "lease_acct-1_0" || grantB.LeaseID != "lease_acct-1_1" {
		t.Fatalf("grants A=%q B=%q, want each spawn's own release (lease_acct-1_0, lease_acct-1_1)",
			grantA.LeaseID, grantB.LeaseID)
	}
}

// TestAccountPool_AGrantReportWithNoResolutionFallsBackToTheCache: a caller
// outside a resolution (no slot) still gets the grant the cached copy holds.
func TestAccountPool_AGrantReportWithNoResolutionFallsBackToTheCache(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: central.server.URL, TenantID: central.tenantID,
		HTTPClient: central.server.Client(), Tokens: &staticTokenSource{}, Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := &accountCredentialCache{}
	secrets := newMaterializingSecrets(client, cache, &testLogger{t: t})
	if _, err := secrets.resolve(context.Background(), "acct-1"); err != nil {
		t.Fatal(err)
	}
	grant, err := newDeliveryGrantReporter(cache).GrantDelivery(context.Background(),
		orchestration.AccountLeaseScope{TenantID: "tenant-a", AccountID: "acct-1"})
	if err != nil || grant == nil || grant.LeaseID != "lease_acct-1" {
		t.Fatalf("grant = %+v, %v; want the cached copy's lease_acct-1", grant, err)
	}
}

// TestAccountPool_InlineAccountStaysCachedAcrossAnUnsignalledChange proves
// the single-use rule is confined to referenced accounts: an inline envelope
// is still served from cache until a signal (last_refreshed_at) says it went
// stale, so the per-spawn release is not the default for every account.
func TestAccountPool_InlineAccountStaysCachedAcrossAnUnsignalledChange(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})
	ctx := context.Background()
	req := orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	}

	first, err := pool.Resolver.Resolve(ctx, req)
	if err != nil {
		t.Fatalf("first Resolve() error = %v", err)
	}
	central.writeReferencedSecret("acct-1", "unsignalled-token")
	second, err := pool.Resolver.Resolve(ctx, req)
	if err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	if first.Env["CLAUDE_CODE_OAUTH_TOKEN"] != second.Env["CLAUDE_CODE_OAUTH_TOKEN"] {
		t.Error("an inline account was re-released with no staleness signal")
	}
	if got := central.releaseCount("acct-1"); got != 1 {
		t.Errorf("released %d times, want 1 (served from cache)", got)
	}
}

// TestAccountPool_ReMaterializesWhenTheCachedEnvelopeExpires proves the TTL is
// the credential's own, not a fixed window: an envelope inside its renewal
// margin is re-released rather than served.
func TestAccountPool_ReMaterializesWhenTheCachedEnvelopeExpires(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	account := central.addAccount("acct-1", "claude", "", true)
	// Expiring inside the renewal margin: never cacheable.
	account.expiresAt = time.Now().UTC().Add(accountCacheRenewalMargin / 2)

	pool := newTestAccountPool(t, central, &staticTokenSource{})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := pool.Resolver.Resolve(ctx, orchestration.AccountResolveRequest{
			TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
		}); err != nil {
			t.Fatalf("Resolve() %d error = %v", i, err)
		}
	}
	if got := central.releaseCount("acct-1"); got != 3 {
		t.Errorf("released %d times, want 3: an envelope inside its renewal margin must never be cached", got)
	}
}

// TestAccountPool_SurfacesACentralRefusal proves the control plane's answer
// reaches the spawn intact rather than being flattened into "no account".
func TestAccountPool_SurfacesACentralRefusal(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	account := central.addAccount("acct-1", "claude", "", true)
	account.refusalStatus = http.StatusConflict
	account.refusalBody = `{"error":"account not releasable: status revoked"}`

	pool := newTestAccountPool(t, central, &staticTokenSource{})
	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	})
	if err == nil {
		t.Fatal("a refused release resolved successfully")
	}
	var apiErr *AccountAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want *AccountAPIError", err, err)
	}
	if apiErr.Status != http.StatusConflict {
		t.Errorf("refusal reported status %d, want 409", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "revoked") {
		t.Errorf("refusal %q lost the control plane's explanation", apiErr.Error())
	}
}

// TestAccountPool_MissingCredentialRefusalKeepsItsFaultClass proves a release
// the control plane refused because the account's credential secret is missing
// reaches the spawn as a MISSING CREDENTIAL, not an opaque store error. That is
// the class the control plane parks uncharged; losing it charged the attempt.
func TestAccountPool_MissingCredentialRefusalKeepsItsFaultClass(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	account := central.addAccount("acct-1", "claude", "", true)
	account.refusalStatus = http.StatusConflict
	account.refusalBody = `{"error":"account: credential not found: credential_secret_ref agents/claude/x: the secret does not exist in this tenant","fault_class":"credential_not_found"}`

	pool := newTestAccountPool(t, central, &staticTokenSource{})
	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	})
	if !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("error = %v, want one matching ErrAccountCredentialNotFound", err)
	}
	if got := orchestration.SpawnFaultClass(err); got != "credential_not_found" {
		t.Errorf("SpawnFaultClass() = %q, want credential_not_found", got)
	}
	var apiErr *AccountAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Errorf("error = %v, want the control plane's 409 refusal kept intact", err)
	}
}

// TestAccountPool_LongRefusalKeepsItsFaultClass proves a refusal body well past
// the detail bound still yields its class, with the detail itself bounded.
func TestAccountPool_LongRefusalKeepsItsFaultClass(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	account := central.addAccount("acct-1", "claude", "", true)
	account.refusalStatus = http.StatusConflict
	account.refusalBody = `{"error":"account: credential not found: ` + strings.Repeat("detail ", 400) +
		`","fault_class":"credential_not_found"}`

	pool := newTestAccountPool(t, central, &staticTokenSource{})
	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	})
	if !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("error = %v, want the class decoded from a long body", err)
	}
	var apiErr *AccountAPIError
	if !errors.As(err, &apiErr) || len(apiErr.Message) > accountAPIMaxErrorDetail+len("... (truncated)") {
		t.Errorf("error detail is not bounded: %d bytes", len(apiErr.Message))
	}
}

// TestAccountPool_OtherConflictIsNotAMissingCredential proves the fault class
// comes from the envelope, not the status: a 409 for a revoked account, or one
// naming a class this build does not know, is no configuration fault.
func TestAccountPool_OtherConflictIsNotAMissingCredential(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no fault class", body: `{"error":"account not releasable: status revoked"}`},
		{name: "unknown fault class", body: `{"error":"something new","fault_class":"from_a_newer_build"}`},
		{name: "not json", body: "conflict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			central := newFakeCentral(t, "tenant-a")
			account := central.addAccount("acct-1", "claude", "", true)
			account.refusalStatus = http.StatusConflict
			account.refusalBody = tt.body

			pool := newTestAccountPool(t, central, &staticTokenSource{})
			_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
				TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
			})
			if err == nil {
				t.Fatal("a refused release resolved successfully")
			}
			if errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
				t.Errorf("error = %v was classified as a missing credential", err)
			}
			if got := orchestration.SpawnFaultClass(err); got != "" {
				t.Errorf("SpawnFaultClass() = %q, want none", got)
			}
		})
	}
}

// TestAccountPool_RefusesAnAccountItDoesNotHold covers the not-found path.
func TestAccountPool_RefusesAnAccountItDoesNotHold(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "ghost",
	})
	var apiErr *AccountAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want *AccountAPIError", err, err)
	}
	if apiErr.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", apiErr.Status)
	}
}

// TestAccountPool_RefusesAnotherTenant proves a daemon cannot reach a pool it
// is not enrolled in, even when something upstream asks it to.
func TestAccountPool_RefusesAnotherTenant(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-b", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	})
	var mismatch *AccountAPITenantMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v (%T), want *AccountAPITenantMismatchError", err, err)
	}
}

// TestAccountPool_RetriesOnceAfterA401 proves the daemon recovers from a token
// the control plane rotated out from under it, exactly as the heartbeat does.
func TestAccountPool_RetriesOnceAfterA401(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	central.unauthorizedOnce = true

	tokens := &staticTokenSource{}
	pool := newTestAccountPool(t, central, tokens)

	if _, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
	}); err != nil {
		t.Fatalf("Resolve() after a single 401 error = %v", err)
	}
	if tokens.invalidationCount() != 1 {
		t.Errorf("token invalidated %d times, want exactly 1", tokens.invalidationCount())
	}
}

// TestAccountPool_ConcurrentSpawnsProduceOneRelease proves the single-flight:
// a burst against one account is one credential handout, not one per spawn.
func TestAccountPool_ConcurrentSpawnsProduceOneRelease(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	pool := newTestAccountPool(t, central, &staticTokenSource{})

	const spawns = 8
	var wg sync.WaitGroup
	errCh := make(chan error, spawns)
	wg.Add(spawns)
	for i := 0; i < spawns; i++ {
		go func() {
			defer wg.Done()
			_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
				TenantID: "tenant-a", Provider: orchestration.AccountProviderClaude, AccountID: "acct-1",
			})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Resolve() error = %v", err)
		}
	}
	if got := central.releaseCount("acct-1"); got != 1 {
		t.Errorf("released %d times across %d concurrent spawns, want 1", got, spawns)
	}
}

// TestAccountPool_StatsOnANilPool covers the accessor's bad-receiver path.
func TestAccountPool_StatsOnANilPool(t *testing.T) {
	var pool *AccountPool
	if hits, misses := pool.Stats(); hits != 0 || misses != 0 {
		t.Errorf("nil pool reported %d/%d, want 0/0", hits, misses)
	}
	if hits, misses := (&AccountPool{}).Stats(); hits != 0 || misses != 0 {
		t.Errorf("uninitialized pool reported %d/%d, want 0/0", hits, misses)
	}
}
