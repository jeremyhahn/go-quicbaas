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
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The API-backed AccountStore contract (go-quicbaas#734).
//
// The store must satisfy all seven methods of orchestration.AccountStore,
// because AccountResolver composes over the whole interface. Three of them do
// real work over the API, one is a documented local no-op, and four refuse.
// The tests below cover every one, in both directions.
// =============================================================================

// newTestAPIAccountStore builds the daemon's store against a fake central.
func newTestAPIAccountStore(t *testing.T, central *fakeCentral) (*apiAccountStore, *accountCredentialCache) {
	t.Helper()
	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: central.server.URL,
		TenantID:   central.tenantID,
		HTTPClient: central.server.Client(),
		Tokens:     &staticTokenSource{},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewAccountAPIClient() error = %v", err)
	}
	cache := &accountCredentialCache{}
	return newAPIAccountStore(client, cache), cache
}

// TestAPIAccountStore_ReadsTheTenantsPool covers Get and ListByTenant.
func TestAPIAccountStore_ReadsTheTenantsPool(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	central.addAccount("acct-2", "codex", "", false)
	store, _ := newTestAPIAccountStore(t, central)
	ctx := context.Background()

	t.Run("get", func(t *testing.T) {
		account, err := store.Get(ctx, "tenant-a", "acct-1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if account.ID != "acct-1" || account.Provider != orchestration.AccountProviderClaude {
			t.Errorf("Get() returned %+v, want acct-1/claude", account)
		}
		if !account.Default {
			t.Error("Get() lost the scope-default flag the strategy selects on")
		}
	})

	t.Run("list filters by provider", func(t *testing.T) {
		accounts, err := store.ListByTenant(ctx, "tenant-a", orchestration.AccountProviderCodex, "")
		if err != nil {
			t.Fatalf("ListByTenant() error = %v", err)
		}
		if len(accounts) != 1 || accounts[0].ID != "acct-2" {
			t.Errorf("ListByTenant(codex) returned %d accounts, want just acct-2", len(accounts))
		}
	})

	t.Run("list unfiltered", func(t *testing.T) {
		accounts, err := store.ListByTenant(ctx, "tenant-a", "", "")
		if err != nil {
			t.Fatalf("ListByTenant() error = %v", err)
		}
		if len(accounts) != 2 {
			t.Errorf("ListByTenant() returned %d accounts, want 2", len(accounts))
		}
	})
}

// TestAPIAccountStore_RefusesAnotherTenant proves the store cannot be pointed
// at a pool this daemon is not enrolled in.
func TestAPIAccountStore_RefusesAnotherTenant(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	store, _ := newTestAPIAccountStore(t, central)
	ctx := context.Background()

	var mismatch *AccountAPITenantMismatchError

	if _, err := store.Get(ctx, "tenant-b", "acct-1"); !errors.As(err, &mismatch) {
		t.Errorf("Get(other tenant) error = %v (%T), want *AccountAPITenantMismatchError", err, err)
	}
	if _, err := store.ListByTenant(ctx, "tenant-b", "", ""); !errors.As(err, &mismatch) {
		t.Errorf("ListByTenant(other tenant) error = %v (%T), want *AccountAPITenantMismatchError", err, err)
	}
	if err := store.Touch(ctx, "tenant-b", "acct-1", orchestration.TouchAccountOptions{}); !errors.As(err, &mismatch) {
		t.Errorf("Touch(other tenant) error = %v (%T), want *AccountAPITenantMismatchError", err, err)
	}
	if !strings.Contains(mismatch.Error(), "tenant-b") || !strings.Contains(mismatch.Error(), "tenant-a") {
		t.Errorf("mismatch %q does not name both tenants", mismatch.Error())
	}
}

// TestAPIAccountStore_TouchSucceedsLocally pins the documented no-op: the
// usage stamp happens on the central inside the release, so reporting a
// failure here would put a warning on every spawn for bookkeeping that is
// already correct.
func TestAPIAccountStore_TouchSucceedsLocally(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	store, _ := newTestAPIAccountStore(t, central)

	if err := store.Touch(context.Background(), "tenant-a", "acct-1",
		orchestration.TouchAccountOptions{MarkUsed: true}); err != nil {
		t.Fatalf("Touch() error = %v, want nil", err)
	}
	// It must not have reached the control plane either: a Touch is not a read.
	if central.gets != 0 || central.lists != 0 {
		t.Errorf("Touch issued %d gets and %d lists; it must make no control-plane call",
			central.gets, central.lists)
	}
}

// TestAPIAccountStore_RefusesEveryMutation walks the four verbs a worker host
// must not perform on the pool it draws from.
func TestAPIAccountStore_RefusesEveryMutation(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	store, _ := newTestAPIAccountStore(t, central)
	ctx := context.Background()

	cases := []struct {
		name   string
		call   func() error
		method string
	}{
		{"create", func() error {
			return store.Create(ctx, &orchestration.Account{ID: "acct-new", TenantID: "tenant-a"})
		}, "Create"},
		{"create nil", func() error { return store.Create(ctx, nil) }, "Create"},
		{"update", func() error {
			return store.Update(ctx, &orchestration.Account{ID: "acct-1", TenantID: "tenant-a"})
		}, "Update"},
		{"update nil", func() error { return store.Update(ctx, nil) }, "Update"},
		{"delete", func() error { return store.Delete(ctx, "tenant-a", "acct-1") }, "Delete"},
		{"set default", func() error { return store.SetDefault(ctx, "tenant-a", "acct-1") }, "SetDefault"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			var readOnly *AccountStoreReadOnlyError
			if !errors.As(err, &readOnly) {
				t.Fatalf("error = %v (%T), want *AccountStoreReadOnlyError", err, err)
			}
			if readOnly.Method != tc.method {
				t.Errorf("refusal names method %q, want %q", readOnly.Method, tc.method)
			}
			if !strings.Contains(readOnly.Error(), "control plane") {
				t.Errorf("refusal %q does not say where the verb belongs", readOnly.Error())
			}
		})
	}
	if central.gets != 0 || central.lists != 0 {
		t.Errorf("a refused mutation reached the control plane (%d gets, %d lists)", central.gets, central.lists)
	}
}

// TestAPIAccountStore_ObservesReadsIntoTheCache proves the invalidation wiring:
// reading a record whose UpdatedAt moved drops the credential released under
// the older one. This is what makes a central rotation reach a worker host.
func TestAPIAccountStore_ObservesReadsIntoTheCache(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	store, cache := newTestAPIAccountStore(t, central)
	ctx := context.Background()

	released := time.Now().UTC().Add(-time.Hour)
	cache.store("acct-1", mustCachedCredential(t, []byte(`{}`), released, time.Now().UTC().Add(time.Hour)))
	if _, ok := cache.lookup("acct-1", time.Now().UTC()); !ok {
		t.Fatal("test setup: entry is not cached")
	}

	// A read that reports the SAME envelope-replacement time keeps the entry —
	// including when the record's UpdatedAt has moved for an unrelated reason
	// (the usage stamp a release itself makes), which is the case that broke
	// the cache when it keyed on UpdatedAt.
	central.mu.Lock()
	central.accounts["acct-1"].record.LastRefreshedAt = released
	central.accounts["acct-1"].record.UpdatedAt = time.Now().UTC()
	central.mu.Unlock()
	if _, err := store.Get(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, ok := cache.lookup("acct-1", time.Now().UTC()); !ok {
		t.Error("an unchanged record evicted the cached credential; every spawn would re-release")
	}

	// A read reporting a NEWER write time drops it.
	central.rotate("acct-1", "rotated")
	if _, err := store.Get(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Get() after rotation error = %v", err)
	}
	if _, ok := cache.lookup("acct-1", time.Now().UTC()); ok {
		t.Error("a rotated record left the retired credential cached")
	}
}

// TestAPIAccountStore_ToleratesNoObserver covers the nil-observer path, which
// is the correct wiring for a caller that keeps no cache.
func TestAPIAccountStore_ToleratesNoObserver(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: central.server.URL,
		TenantID:   "tenant-a",
		HTTPClient: central.server.Client(),
		Tokens:     &staticTokenSource{},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewAccountAPIClient() error = %v", err)
	}
	store := newAPIAccountStore(client, nil)

	if _, err := store.Get(context.Background(), "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Get() with no observer error = %v", err)
	}
}

// =============================================================================
// Client-level cases the store path does not reach
// =============================================================================

// TestAccountAPIClient_AccountTokensReadsTheCentralLedger covers the usage
// lookup the least-usage strategy depends on, plus its tenant guard.
func TestAccountAPIClient_AccountTokensReadsTheCentralLedger(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	central.usage["acct-1"] = 4242

	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: central.server.URL,
		TenantID:   "tenant-a",
		HTTPClient: central.server.Client(),
		Tokens:     &staticTokenSource{},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewAccountAPIClient() error = %v", err)
	}

	tokens, err := client.AccountTokens(context.Background(), "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("AccountTokens() error = %v", err)
	}
	if tokens != 4242 {
		t.Errorf("AccountTokens() = %d, want 4242", tokens)
	}

	if _, err := client.AccountTokens(context.Background(), "tenant-b", "acct-1"); err == nil {
		t.Error("AccountTokens() answered for another tenant")
	}
}

// TestAccountAPIClient_GetAccountRejectsAnEmptyEnvelope proves a control plane
// answering 200 with no record is a fault, not an empty answer: the resolver
// would otherwise dereference nothing.
func TestAccountAPIClient_GetAccountRejectsAnEmptyEnvelope(t *testing.T) {
	// The fake's /{id} route answers 404 for an unknown account, so the
	// empty-envelope case needs a server that answers 200 with {} instead.
	empty := newFakeCentral(t, "tenant-a")
	empty.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{})
	})

	client, err := NewAccountAPIClient(AccountAPIClientConfig{
		ServerAddr: empty.server.URL,
		TenantID:   "tenant-a",
		HTTPClient: empty.server.Client(),
		Tokens:     &staticTokenSource{},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewAccountAPIClient() error = %v", err)
	}

	_, err = client.GetAccount(context.Background(), "acct-1")
	var apiErr *AccountAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want *AccountAPIError", err, err)
	}
	if !strings.Contains(apiErr.Error(), "no account record") {
		t.Errorf("error %q does not name the fault", apiErr.Error())
	}
}

// TestAccountAPIError_ReportsWhatItKnows covers the error's rendering in each
// of the shapes it is constructed in.
func TestAccountAPIError_ReportsWhatItKnows(t *testing.T) {
	cause := errors.New("connection refused")
	withCause := &AccountAPIError{Op: "GET /x", Cause: cause}
	if !errors.Is(withCause, cause) {
		t.Error("AccountAPIError does not unwrap its cause")
	}
	if !strings.Contains(withCause.Error(), "connection refused") {
		t.Errorf("error %q lost the cause", withCause.Error())
	}

	withStatus := &AccountAPIError{Op: "POST /y", AccountID: "acct-1", Status: 409, Message: "revoked"}
	msg := withStatus.Error()
	for _, want := range []string{"POST /y", "acct-1", "409", "revoked"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q is missing %q", msg, want)
		}
	}
	if (&AccountAPIError{Op: "bare"}).Unwrap() != nil {
		t.Error("a causeless error unwrapped to something")
	}
}

// TestAccountAPIError_UnwrapsAKnownFaultClass proves the envelope's fault class
// is rebuilt into the orchestration sentinel, alongside any transport cause.
func TestAccountAPIError_UnwrapsAKnownFaultClass(t *testing.T) {
	cause := errors.New("connection reset")
	err := &AccountAPIError{Op: "POST /m", Status: 409, FaultClass: "credential_not_found", Cause: cause}
	if !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Error("a credential_not_found fault class does not unwrap to ErrAccountCredentialNotFound")
	}
	if !errors.Is(err, cause) {
		t.Error("the fault class displaced the transport cause")
	}
	cooling := &AccountAPIError{Op: "POST /m", Status: 409, FaultClass: "accounts_cooling_down"}
	if !errors.Is(cooling, orchestration.ErrAccountsCoolingDown) {
		t.Error("an accounts_cooling_down fault class does not unwrap to ErrAccountsCoolingDown")
	}
}

// TestAccountAPIError_IgnoresAnUnknownFaultClass proves a class this build does
// not know leaves the error what it was: no sentinel, no configuration fault.
func TestAccountAPIError_IgnoresAnUnknownFaultClass(t *testing.T) {
	err := &AccountAPIError{Op: "POST /m", Status: 409, FaultClass: "from_a_newer_build"}
	if got := err.Unwrap(); len(got) != 0 {
		t.Errorf("Unwrap() = %v, want nothing", got)
	}
	if class := orchestration.SpawnFaultClass(err); class != "" {
		t.Errorf("SpawnFaultClass() = %q, want none", class)
	}
}

// TestTruncateAccountAPIDetail_BoundsTheMessage covers the short and long
// cases: a message under the bound is untouched, one over it is cut.
func TestTruncateAccountAPIDetail_BoundsTheMessage(t *testing.T) {
	if got := truncateAccountAPIDetail("account is revoked"); got != "account is revoked" {
		t.Errorf("short detail = %q, want it unchanged", got)
	}
	long := strings.Repeat("x", accountAPIMaxErrorDetail+100)
	got := truncateAccountAPIDetail(long)
	if !strings.HasPrefix(got, strings.Repeat("x", accountAPIMaxErrorDetail)) || !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("long detail = %q, want the bound's prefix marked truncated", got)
	}
}

// TestTruncateAccountAPIDetail_KeepsARuneWhole proves a cut that would land
// inside a multi-byte rune backs off to its start rather than emitting
// invalid UTF-8.
func TestTruncateAccountAPIDetail_KeepsARuneWhole(t *testing.T) {
	detail := strings.Repeat("x", accountAPIMaxErrorDetail-1) + "é" + "tail"
	got := truncateAccountAPIDetail(detail)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated detail is not valid UTF-8: %q", got)
	}
	if strings.Contains(got, "é") {
		t.Errorf("truncated detail kept a rune that straddles the bound: %q", got)
	}
}
