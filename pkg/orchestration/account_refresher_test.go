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

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// countingRefresher records how many times Refresh was called and returns a
// fresh, monotonically-numbered access token each time, so a test can prove
// single-flight collapses concurrent callers into one HTTP round trip.
type countingRefresher struct {
	calls atomic.Int64
	delay time.Duration
}

func (r *countingRefresher) Refresh(_ context.Context, cred *AccountCredential) (*AccountCredential, error) {
	n := r.calls.Add(1)
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	renewed := *cred
	renewed.AccessToken = "renewed-token"
	renewed.RefreshToken = "renewed-refresh"
	renewed.ExpiresAt = time.Now().Add(time.Hour)
	_ = n
	return &renewed, nil
}

// barrierRefresher blocks every Refresh until `want` of them are in flight at
// once, then releases them all.
//
// It turns concurrency from something a test INFERS from elapsed time into
// something the test cannot finish without: if the manager serialized two
// accounts, the first call would sit here waiting for a second that cannot
// start, and the wait would end at the context deadline with a message saying
// exactly that. There is no threshold to tune and nothing that a slow machine
// can push past.
type barrierRefresher struct {
	calls atomic.Int64

	mu      sync.Mutex
	arrived int
	want    int
	release chan struct{}
}

func newBarrierRefresher(want int) *barrierRefresher {
	return &barrierRefresher{want: want, release: make(chan struct{})}
}

func (r *barrierRefresher) Refresh(ctx context.Context, cred *AccountCredential) (*AccountCredential, error) {
	r.calls.Add(1)

	r.mu.Lock()
	r.arrived++
	if r.arrived == r.want {
		close(r.release)
	}
	r.mu.Unlock()

	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, fmt.Errorf("refresh waited for %d concurrent calls and saw %d before the context ended: %w",
			r.want, r.currentArrived(), ctx.Err())
	}

	renewed := *cred
	renewed.AccessToken = "renewed-token"
	renewed.RefreshToken = "renewed-refresh"
	renewed.ExpiresAt = time.Now().Add(time.Hour)
	return &renewed, nil
}

func (r *barrierRefresher) currentArrived() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.arrived
}

type failingRefresher struct{ err error }

func (r *failingRefresher) Refresh(_ context.Context, _ *AccountCredential) (*AccountCredential, error) {
	return nil, r.err
}

func setupTokenManager(t *testing.T) (*AccountTokenManager, AccountStore, *AccountSecretStore) {
	t.Helper()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	mgr := NewAccountTokenManager(secretStore, store, 60*time.Second)
	return mgr, store, secretStore
}

func TestAccountTokenManager_Resolve_StaticCredentialNeverRefreshed(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderCodex, RefreshPolicy: AccountRefreshNone}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{Env: map[string]string{"CODEX_API_KEY": "static-key"}}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	cred, err := mgr.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cred.Env["CODEX_API_KEY"] != "static-key" {
		t.Fatalf("Resolve() = %+v, want the static key untouched", cred)
	}
}

func TestAccountTokenManager_Resolve_RefreshesExpiredCredential(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	refresher := &countingRefresher{}
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute), // already expired
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	cred, err := mgr.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cred.AccessToken != "renewed-token" {
		t.Fatalf("Resolve() AccessToken = %s, want renewed-token", cred.AccessToken)
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresher called %d times, want 1", refresher.calls.Load())
	}

	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.LastRefreshedAt.IsZero() {
		t.Fatalf("account.LastRefreshedAt not updated after a successful refresh")
	}
}

// TestAccountTokenManager_SingleFlight_PerAccount proves five concurrent
// Resolve calls for the SAME account collapse into exactly one Refresh call,
// and the presented refresh_token is never replayed against the (fake)
// authorization server.
func TestAccountTokenManager_SingleFlight_PerAccount(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	refresher := &countingRefresher{delay: 20 * time.Millisecond}
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken:  "old-token",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	const concurrency = 5
	var wg sync.WaitGroup
	results := make([]*AccountCredential, concurrency)
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = mgr.Resolve(ctx, account)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Resolve()[%d] error = %v", i, err)
		}
		if results[i].AccessToken != "renewed-token" {
			t.Fatalf("Resolve()[%d].AccessToken = %s, want renewed-token", i, results[i].AccessToken)
		}
	}
	if refresher.calls.Load() != 1 {
		t.Fatalf("refresher called %d times across 5 concurrent spawns, want exactly 1 (single-flight)", refresher.calls.Load())
	}
}

// TestAccountTokenManager_SingleFlight_DifferentAccounts proves two DIFFERENT
// accounts refresh in parallel rather than serializing behind one lock.
// TestAccountTokenManager_SingleFlight_DifferentAccounts asserts that
// single-flight is keyed PER ACCOUNT: two different accounts refresh
// concurrently rather than queueing behind one another.
//
// It used to time the pair -- each refresh slept 30ms, and the test required
// the whole thing to finish in under 60ms. That is a performance threshold
// wearing a correctness hat: a loaded machine blows past 60ms while the code is
// behaving perfectly, which is how it reddened CI on PRs that do not touch this
// package (go-quicbaas#423).
//
// The barrier below asserts the same property without a clock. Both refreshes
// must be in flight at the same moment before either is allowed to return, so
// serialization cannot complete the test at all -- it can only time out, with a
// message naming how many calls arrived. The 10s context is a deadlock
// detector, not a budget: it distinguishes "never" from "eventually", which is
// the only thing left to distinguish.
func TestAccountTokenManager_SingleFlight_DifferentAccounts(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	refresher := newBarrierRefresher(2)
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	for _, id := range []string{"acct-1", "acct-2"} {
		account := &Account{ID: id, TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
		if err := store.Create(ctx, account); err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
		if err := secretStore.Put(ctx, "tenant-a", id, &AccountCredential{
			AccessToken: "old", RefreshToken: "rt-" + id, ExpiresAt: time.Now().Add(-time.Minute),
		}); err != nil {
			t.Fatalf("Put(%s) error = %v", id, err)
		}
	}

	resolveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, id := range []string{"acct-1", "acct-2"} {
		wg.Add(1)
		go func(accountID string) {
			defer wg.Done()
			account, _ := store.Get(resolveCtx, "tenant-a", accountID)
			if _, err := mgr.Resolve(resolveCtx, account); err != nil {
				t.Errorf("Resolve(%s) error = %v — different accounts appear to have serialized", accountID, err)
			}
		}(id)
	}
	wg.Wait()

	if refresher.calls.Load() != 2 {
		t.Fatalf("refresher called %d times for 2 distinct accounts, want 2", refresher.calls.Load())
	}
}

func TestAccountTokenManager_Resolve_RefreshFailure_MarksAccountError(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()
	mgr.RegisterRefresher(AccountProviderClaude, &failingRefresher{err: errors.New("token endpoint unreachable")})

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := mgr.Resolve(ctx, account); err == nil {
		t.Fatalf("Resolve() error = nil, want a refresh failure")
	}

	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.Status != AccountStatusError {
		t.Fatalf("account.Status = %s, want error after a failed refresh", got.Status)
	}
	if got.LastError == "" {
		t.Fatalf("account.LastError is empty after a failed refresh")
	}
}

func TestAccountTokenManager_Resolve_NoRefresherRegistered(t *testing.T) {
	mgr, store, secretStore := setupTokenManager(t)
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	_, err := mgr.Resolve(ctx, account)
	var notConfigured *AccountRefresherNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRefresherNotConfiguredError", err, err)
	}
}

// refreshStampFailures reads the refresh-stamp failure counter for one tenant
// and outcome. Each test uses its own tenant label so the process-wide counter
// cannot be moved by another test.
func refreshStampFailures(tenantID, outcome string) float64 {
	return promtestutil.ToFloat64(metrics.GetMetrics().OrchAccountRefreshStampFailures.With(
		prometheus.Labels{"tenant": tenantID, "outcome": outcome}))
}

// setupTouchFailingTokenManager returns a manager whose account store fails
// every Touch with touchErr, logging to the returned recordingLogger.
func setupTouchFailingTokenManager(t *testing.T, tenantID string, touchErr error, refresher TokenRefresher) (*AccountTokenManager, *touchFailingAccountStore, *recordingLogger, *Account) {
	t.Helper()
	ctx := context.Background()
	base := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	store := &touchFailingAccountStore{AccountStore: base, err: touchErr}
	logger := newRecordingLogger()
	mgr := NewAccountTokenManager(secretStore, store, 60*time.Second, WithAccountTokenLogger(logger))
	mgr.RegisterRefresher(AccountProviderClaude, refresher)

	account := &Account{ID: "acct-1", TenantID: tenantID, Provider: AccountProviderClaude, RefreshPolicy: AccountRefreshOAuth2}
	if err := base.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, tenantID, "acct-1", &AccountCredential{
		AccessToken: "old", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	return mgr, store, logger, account
}

// TestAccountTokenManager_Resolve_RefreshSuccessStampFailure proves a renewal
// whose outcome could not be written to the account record is logged, counted
// and reported as a failure rather than a success.
func TestAccountTokenManager_Resolve_RefreshSuccessStampFailure(t *testing.T) {
	const tenantID = "tenant-stamp-success"
	ctx := context.Background()
	touchErr := errors.New("account record write conflicted")
	mgr, store, logger, account := setupTouchFailingTokenManager(t, tenantID, touchErr, &countingRefresher{})
	before := refreshStampFailures(tenantID, AccountRefreshOutcomeSuccess)

	cred, err := mgr.Resolve(ctx, account)
	if err == nil {
		t.Fatalf("Resolve() = %+v, nil error; want the stamp failure reported", cred)
	}
	if cred != nil {
		t.Fatalf("Resolve() credential = %+v, want nil alongside the stamp failure", cred)
	}
	var stampErr *AccountRefreshStampFailedError
	if !errors.As(err, &stampErr) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRefreshStampFailedError", err, err)
	}
	if stampErr.AccountID != "acct-1" || stampErr.Outcome != AccountRefreshOutcomeSuccess {
		t.Fatalf("stamp error = %+v, want account acct-1 and outcome success", stampErr)
	}
	if !errors.Is(err, ErrAccountRefreshStampFailed) || !errors.Is(err, touchErr) {
		t.Fatalf("Resolve() error = %v, want it to match the stamp sentinel and the store's cause", err)
	}
	if store.touchCalls() != 1 {
		t.Fatalf("Touch called %d times, want 1", store.touchCalls())
	}

	warn := logger.atLevel("WARN")
	if !strings.Contains(warn, "acct-1") || !strings.Contains(warn, touchErr.Error()) {
		t.Fatalf("warnings = %q, want the account id and the store error", warn)
	}
	if got := refreshStampFailures(tenantID, AccountRefreshOutcomeSuccess) - before; got != 1 {
		t.Fatalf("refresh-stamp failure metric moved by %v, want 1", got)
	}
}

// TestAccountTokenManager_Resolve_RefreshFailureStampFailure proves a failed
// refresh whose error could not be written to the account record carries the
// stamp failure alongside the refresh failure, and logs and counts it.
func TestAccountTokenManager_Resolve_RefreshFailureStampFailure(t *testing.T) {
	const tenantID = "tenant-stamp-failure"
	ctx := context.Background()
	refreshErr := errors.New("token endpoint unreachable")
	touchErr := errors.New("store unavailable")
	mgr, _, logger, account := setupTouchFailingTokenManager(t, tenantID, touchErr, &failingRefresher{err: refreshErr})
	before := refreshStampFailures(tenantID, AccountRefreshOutcomeFailure)

	_, err := mgr.Resolve(ctx, account)
	var refreshFailed *AccountRefreshFailedError
	if !errors.As(err, &refreshFailed) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRefreshFailedError", err, err)
	}
	var stampErr *AccountRefreshStampFailedError
	if !errors.As(err, &stampErr) || stampErr.Outcome != AccountRefreshOutcomeFailure {
		t.Fatalf("Resolve() error = %v, want an *AccountRefreshStampFailedError for the failure outcome", err)
	}
	if !errors.Is(err, refreshErr) || !errors.Is(err, touchErr) {
		t.Fatalf("Resolve() error = %v, want both the refresh and the store error", err)
	}

	warn := logger.atLevel("WARN")
	if !strings.Contains(warn, "acct-1") || !strings.Contains(warn, touchErr.Error()) {
		t.Fatalf("warnings = %q, want the account id and the store error", warn)
	}
	if got := refreshStampFailures(tenantID, AccountRefreshOutcomeFailure) - before; got != 1 {
		t.Fatalf("refresh-stamp failure metric moved by %v, want 1", got)
	}
}

// TestAccountTokenManager_Resolve_RefreshSuccessStampedCleanly proves the
// success path is unchanged: nothing is logged, counted or returned when the
// outcome stamp succeeds.
func TestAccountTokenManager_Resolve_RefreshSuccessStampedCleanly(t *testing.T) {
	const tenantID = "tenant-stamp-clean"
	ctx := context.Background()
	mgr, store, logger, account := setupTouchFailingTokenManager(t, tenantID, nil, &countingRefresher{})
	before := refreshStampFailures(tenantID, AccountRefreshOutcomeSuccess)

	cred, err := mgr.Resolve(ctx, account)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cred.AccessToken != "renewed-token" {
		t.Fatalf("Resolve() AccessToken = %s, want renewed-token", cred.AccessToken)
	}
	if store.touchCalls() != 1 {
		t.Fatalf("Touch called %d times, want 1", store.touchCalls())
	}
	if warn := logger.atLevel("WARN"); warn != "" {
		t.Fatalf("warnings = %q, want none on a clean stamp", warn)
	}
	if got := refreshStampFailures(tenantID, AccountRefreshOutcomeSuccess) - before; got != 0 {
		t.Fatalf("refresh-stamp failure metric moved by %v, want 0", got)
	}
}

// TestAccountTokenManager_Resolve_RefreshFailureStampRefusedAsRevoked proves a
// failure stamp the store refuses because the account was revoked meanwhile is
// the intended outcome, not a stamp failure: the refresh error is returned
// alone, and nothing is logged or counted.
func TestAccountTokenManager_Resolve_RefreshFailureStampRefusedAsRevoked(t *testing.T) {
	const tenantID = "tenant-stamp-revoked"
	ctx := context.Background()
	refreshErr := errors.New("token endpoint unreachable")
	touchErr := &AccountRevokedError{AccountID: "acct-1"}
	mgr, _, logger, account := setupTouchFailingTokenManager(t, tenantID, touchErr, &failingRefresher{err: refreshErr})
	before := refreshStampFailures(tenantID, AccountRefreshOutcomeFailure)

	_, err := mgr.Resolve(ctx, account)
	var refreshFailed *AccountRefreshFailedError
	if !errors.As(err, &refreshFailed) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRefreshFailedError", err, err)
	}
	var stampErr *AccountRefreshStampFailedError
	if errors.As(err, &stampErr) {
		t.Fatalf("Resolve() error = %v, want no stamp failure for a revoked refusal", err)
	}
	if warn := logger.atLevel("WARN"); warn != "" {
		t.Fatalf("warnings = %q, want none", warn)
	}
	if got := refreshStampFailures(tenantID, AccountRefreshOutcomeFailure) - before; got != 0 {
		t.Fatalf("refresh-stamp failure metric moved by %v, want 0", got)
	}
}

// TestAccountTokenManager_Resolve_SecretRefStampFailure proves an unresolved
// secret reference whose error stamp could not be written reports the stamp
// failure alongside the read failure, and logs and counts it.
func TestAccountTokenManager_Resolve_SecretRefStampFailure(t *testing.T) {
	const tenantID = "tenant-stamp-secretref"
	ctx := context.Background()
	base := NewInMemoryAccountStore()
	touchErr := errors.New("store unavailable")
	store := &touchFailingAccountStore{AccountStore: base, err: touchErr}
	logger := newRecordingLogger()
	mgr := NewAccountTokenManager(NewAccountSecretStore(nil), store, 0,
		WithAccountSecretResolver(newMemorySecretResolver()), WithAccountTokenLogger(logger))
	account := &Account{
		ID:                  "acct-1",
		TenantID:            tenantID,
		Provider:            AccountProviderClaude,
		Status:              AccountStatusActive,
		RefreshPolicy:       AccountCredentialModeStaticToken,
		CredentialSecretRef: "agents/claude/missing",
	}
	if err := base.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	before := refreshStampFailures(tenantID, AccountRefreshOutcomeFailure)

	_, err := mgr.Resolve(ctx, account)
	var unresolved *AccountSecretRefUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountSecretRefUnresolvedError", err, err)
	}
	var stampErr *AccountRefreshStampFailedError
	if !errors.As(err, &stampErr) || stampErr.Outcome != AccountRefreshOutcomeFailure {
		t.Fatalf("Resolve() error = %v, want an *AccountRefreshStampFailedError for the failure outcome", err)
	}
	if !errors.Is(err, touchErr) {
		t.Fatalf("Resolve() error = %v, want it to carry the store error", err)
	}
	if store.touchCalls() != 1 {
		t.Fatalf("Touch called %d times, want 1", store.touchCalls())
	}
	warn := logger.atLevel("WARN")
	if !strings.Contains(warn, "acct-1") || !strings.Contains(warn, touchErr.Error()) {
		t.Fatalf("warnings = %q, want the account id and the store error", warn)
	}
	if got := refreshStampFailures(tenantID, AccountRefreshOutcomeFailure) - before; got != 1 {
		t.Fatalf("refresh-stamp failure metric moved by %v, want 1", got)
	}
}
