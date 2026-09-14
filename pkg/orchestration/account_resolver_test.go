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
	"testing"
	"time"
)

func setupResolver(t *testing.T) (AccountResolver, AccountStore, *AccountSecretStore) {
	t.Helper()
	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	tokens := NewAccountTokenManager(secretStore, store, 60*time.Second)
	strategies := NewAccountStrategyRegistry(nil)
	resolver := NewAccountResolver(store, tokens, strategies)
	return resolver, store, secretStore
}

func TestAccountResolver_PinnedAccountID(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()

	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-1",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-1" || resolved.Strategy != "pinned" {
		t.Fatalf("Resolve() = %+v, want account=acct-1 strategy=pinned", resolved)
	}
	if resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok-1" {
		t.Fatalf("Resolve().Env = %+v, want CLAUDE_CODE_OAUTH_TOKEN=tok-1", resolved.Env)
	}

	got, _ := store.Get(ctx, "tenant-a", "acct-1")
	if got.LastUsedAt.IsZero() {
		t.Fatalf("LastUsedAt not recorded after Resolve()")
	}
}

func TestAccountResolver_PinnedAccount_WrongTenant_NotFound(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-b", Provider: AccountProviderClaude, AccountID: "acct-1"})
	if err == nil {
		t.Fatalf("Resolve() across tenants succeeded, want AccountNotFoundError")
	}
}

func TestAccountResolver_PinnedAccount_ProviderMismatch(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderCodex}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1"})
	var mismatch *AccountProviderMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountProviderMismatchError", err, err)
	}
}

func TestAccountResolver_PinnedAccount_Revoked(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	account := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusRevoked}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"})

	_, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, AccountID: "acct-1"})
	var revoked *AccountRevokedError
	if !errors.As(err, &revoked) {
		t.Fatalf("Resolve() error = %v (%T), want *AccountRevokedError", err, err)
	}
}

func TestAccountResolver_Auto_UsesStaticStrategy(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	a := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	b := &Account{ID: "acct-2", TenantID: "tenant-a", Provider: AccountProviderClaude, Default: true}
	for _, acc := range []*Account{a, b} {
		if err := store.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s) error = %v", acc.ID, err)
		}
		if err := secretStore.Put(ctx, "tenant-a", acc.ID, &AccountCredential{AccessToken: "tok-" + acc.ID}); err != nil {
			t.Fatalf("Put(%s) error = %v", acc.ID, err)
		}
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-2" {
		t.Fatalf("Resolve() picked %s, want acct-2 (the Default)", resolved.AccountID)
	}
	if resolved.Strategy != StaticAccountStrategyName {
		t.Fatalf("Resolve().Strategy = %s, want %s", resolved.Strategy, StaticAccountStrategyName)
	}
}

func TestAccountResolver_Auto_ExcludesRevokedAndErrored(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	revoked := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusRevoked}
	errored := &Account{ID: "acct-2", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusError}
	active := &Account{ID: "acct-3", TenantID: "tenant-a", Provider: AccountProviderClaude, Status: AccountStatusActive}
	for _, acc := range []*Account{revoked, errored, active} {
		if err := store.Create(ctx, acc); err != nil {
			t.Fatalf("Create(%s) error = %v", acc.ID, err)
		}
		_ = secretStore.Put(ctx, "tenant-a", acc.ID, &AccountCredential{AccessToken: "tok"})
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-3" {
		t.Fatalf("Resolve() picked %s, want acct-3 (only active candidate)", resolved.AccountID)
	}
}

func TestAccountResolver_Auto_NoCandidates(t *testing.T) {
	resolver, _, _ := setupResolver(t)
	_, err := resolver.Resolve(context.Background(), AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true})
	var notAvailable *NoAccountAvailableError
	if !errors.As(err, &notAvailable) {
		t.Fatalf("Resolve() error = %v (%T), want *NoAccountAvailableError", err, err)
	}
}

func TestAccountResolver_NeitherPinnedNorAuto_Errors(t *testing.T) {
	resolver, _, _ := setupResolver(t)
	_, err := resolver.Resolve(context.Background(), AccountResolveRequest{TenantID: "tenant-a", Provider: AccountProviderClaude})
	if err == nil {
		t.Fatalf("Resolve() error = nil, want an error when neither AccountID nor Auto is set")
	}
}

func TestAccountResolver_Auto_ProjectScopedFallsBackToTenantWide(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()
	tenantWide := &Account{ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude}
	if err := store.Create(ctx, tenantWide); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "tok"})

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-x", Provider: AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != "acct-1" {
		t.Fatalf("Resolve() = %s, want acct-1 (tenant-wide fallback)", resolved.AccountID)
	}
}

// TestAccountResolver_Auto_SeparatesAnEmptyPoolFromACoolingDownOne is
// go-quicbaas#235's round-5 blocker at its source.
//
// Both answers used to be ErrNoAccountAvailable, and the SDLC module parks a
// work item for a human on that — correct for a tenant that registered no
// accounts, and wrong for a pool whose accounts are all on a rate-limit
// cooldown. The second clears itself, usually in minutes, so parking it puts a
// person in front of work that was about to become dispatchable.
func TestAccountResolver_Auto_SeparatesAnEmptyPoolFromACoolingDownOne(t *testing.T) {
	future := time.Now().UTC().Add(7 * time.Minute).Truncate(time.Second)

	tests := []struct {
		name string
		// seed populates the pool.
		seed func(t *testing.T, ctx context.Context, store AccountStore)
		// wantCooling is whether this must be reported as a cooldown.
		wantCooling bool
		wantCount   int
		wantAt      time.Time
	}{
		{
			name:        "no accounts at all",
			seed:        func(*testing.T, context.Context, AccountStore) {},
			wantCooling: false,
		},
		{
			name: "every account on a hold that expires",
			seed: func(t *testing.T, ctx context.Context, store AccountStore) {
				for _, id := range []string{"acct-1", "acct-2"} {
					at := future
					if id == "acct-2" {
						at = future.Add(3 * time.Minute)
					}
					if err := store.Create(ctx, &Account{
						ID: id, TenantID: "tenant-a", Provider: AccountProviderClaude,
						Status: AccountStatusLimited, AvailableAt: at,
					}); err != nil {
						t.Fatalf("Create(%s): %v", id, err)
					}
				}
			},
			wantCooling: true,
			wantCount:   2,
			// THE EARLIEST, not any of them: a caller waiting for the pool
			// waits for the first one back.
			wantAt: future,
		},
		{
			// NOT A COOLDOWN, and this is the case review of #235 caught.
			// LimitHoldActive treats a zero AvailableAt as held INDEFINITELY —
			// nothing promised when it comes back — which is exactly what
			// "clears itself" cannot mean. Reported as a cooldown, the SDLC module
			// requeues and retries forever against a pool with no return date.
			// It parks instead, like any other pool that needs a human.
			name: "a hold with no stated expiry is not self-clearing",
			seed: func(t *testing.T, ctx context.Context, store AccountStore) {
				if err := store.Create(ctx, &Account{
					ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusLimited,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
			wantCooling: false,
		},
		{
			// MIXED, AND IT IS A COOLDOWN. One account will never come back on
			// its own; the other returns at a stated time, and that is what
			// makes progress possible. The answer is the time that account
			// returns, not "unknown".
			name: "one indefinite hold beside one that expires",
			seed: func(t *testing.T, ctx context.Context, store AccountStore) {
				if err := store.Create(ctx, &Account{
					ID: "acct-stuck", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusLimited,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
				if err := store.Create(ctx, &Account{
					ID: "acct-returning", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusLimited, AvailableAt: future,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
			wantCooling: true,
			// One: the indefinite hold is not something to wait for.
			wantCount: 1,
			wantAt:    future,
		},
		{
			// A pool that needs a human for two different reasons still needs
			// a human.
			name: "an indefinite hold beside a revoked account",
			seed: func(t *testing.T, ctx context.Context, store AccountStore) {
				if err := store.Create(ctx, &Account{
					ID: "acct-stuck", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusLimited,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
				if err := store.Create(ctx, &Account{
					ID: "acct-gone", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusRevoked,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
			wantCooling: false,
		},
		{
			// NOT A COOLDOWN. A revoked account is not coming back on its own,
			// so a pool of nothing but revoked accounts is misconfigured and
			// must park exactly as an empty one does.
			name: "every account revoked",
			seed: func(t *testing.T, ctx context.Context, store AccountStore) {
				if err := store.Create(ctx, &Account{
					ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusRevoked,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
			wantCooling: false,
		},
		{
			// Nor is one the CALLER excluded: the request is not waiting for
			// an account it asked not to use.
			name: "the only cooling account is outside the allow-list",
			seed: func(t *testing.T, ctx context.Context, store AccountStore) {
				if err := store.Create(ctx, &Account{
					ID: "acct-other", TenantID: "tenant-a", Provider: AccountProviderClaude,
					Status: AccountStatusLimited, AvailableAt: future,
				}); err != nil {
					t.Fatalf("Create: %v", err)
				}
			},
			wantCooling: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver, store, _ := setupResolver(t)
			ctx := context.Background()
			tc.seed(t, ctx, store)

			req := AccountResolveRequest{
				TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
			}
			if tc.name == "the only cooling account is outside the allow-list" {
				req.AllowedAccountIDs = []string{"acct-chosen"}
			}

			_, err := resolver.Resolve(ctx, req)
			if err == nil {
				t.Fatal("Resolve succeeded against a pool with nothing selectable")
			}

			// EVERY CASE still answers the general question the same way, so
			// no existing caller changes behaviour.
			if !errors.Is(err, ErrNoAccountAvailable) {
				t.Errorf("err = %v, want it to still satisfy ErrNoAccountAvailable", err)
			}

			gotCooling := errors.Is(err, ErrAccountsCoolingDown)
			if gotCooling != tc.wantCooling {
				t.Fatalf("reported as cooling down = %v, want %v (err %v)",
					gotCooling, tc.wantCooling, err)
			}
			if !tc.wantCooling {
				return
			}

			var cooling *AccountsCoolingDownError
			if !errors.As(err, &cooling) {
				t.Fatalf("err = %T, want the typed cooldown carrying the return time", err)
			}
			if cooling.Candidates != tc.wantCount {
				t.Errorf("candidates = %d, want %d", cooling.Candidates, tc.wantCount)
			}
			if !cooling.AvailableAt.Equal(tc.wantAt) {
				t.Errorf("available_at = %v, want %v", cooling.AvailableAt, tc.wantAt)
			}
			// THE INVARIANT THE REQUEUE RESTS ON: a cooldown always says when.
			// Without it the SDLC module would be told to wait on a condition with
			// no end, which is the livelock this whole distinction exists to
			// avoid.
			if cooling.AvailableAt.IsZero() {
				t.Error("a cooldown was reported with no return time; nothing self-clears " +
					"at an unknown moment")
			}
		})
	}
}

// TestAccountResolver_Auto_ALapsedHoldIsNotACooldown is the boundary: an
// account whose hold has expired is selectable again, so nothing is waiting.
func TestAccountResolver_Auto_ALapsedHoldIsNotACooldown(t *testing.T) {
	resolver, store, secretStore := setupResolver(t)
	ctx := context.Background()

	if err := store.Create(ctx, &Account{
		ID: "acct-1", TenantID: "tenant-a", Provider: AccountProviderClaude,
		Status: AccountStatusLimited, AvailableAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := secretStore.Put(ctx, "tenant-a", "acct-1", &AccountCredential{
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN", AccessToken: "tok-1",
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	resolved, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", Provider: AccountProviderClaude, Auto: true,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v; a lapsed hold must rejoin the pool", err)
	}
	if resolved.AccountID != "acct-1" {
		t.Errorf("resolved %q, want acct-1", resolved.AccountID)
	}
}

// TestAccountResolver_Auto_ProjectScopedCooldownFallsBackToTheTenantPool covers
// the path the two-step lookup makes easy to get wrong.
//
// A project-scoped pool that is empty falls back to the tenant-wide one, and
// the cooldown answer has to be computed over whichever set was finally
// considered — reporting only the project's candidates would call a busy
// tenant pool "no account available" and park it.
func TestAccountResolver_Auto_ProjectScopedCooldownFallsBackToTheTenantPool(t *testing.T) {
	resolver, store, _ := setupResolver(t)
	ctx := context.Background()
	future := time.Now().UTC().Add(6 * time.Minute).Truncate(time.Second)

	// Nothing scoped to the project; one unscoped account, cooling down.
	if err := store.Create(ctx, &Account{
		ID: "acct-tenant", TenantID: "tenant-a", Provider: AccountProviderClaude,
		Status: AccountStatusLimited, AvailableAt: future,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-1",
		Provider: AccountProviderClaude, Auto: true,
	})
	if err == nil {
		t.Fatal("Resolve succeeded against a pool with nothing selectable")
	}

	var cooling *AccountsCoolingDownError
	if !errors.As(err, &cooling) {
		t.Fatalf("err = %v (%T), want the tenant-wide pool's cooldown; a project-scoped "+
			"lookup that reports only its own candidates parks a pool that is merely busy",
			err, err)
	}
	if cooling.Candidates != 1 || !cooling.AvailableAt.Equal(future) {
		t.Errorf("cooldown = %+v, want 1 candidate returning at %v", cooling, future)
	}
	if cooling.ProjectID != "proj-1" {
		t.Errorf("project = %q, want the project the request named", cooling.ProjectID)
	}
}

// TestAccountResolver_Auto_AProjectScopedCooldownIsNotHiddenByAnEmptyTenantPool
// is the mirror: the project's own cooling account must be counted even when
// the tenant-wide fallback finds nothing at all.
func TestAccountResolver_Auto_AProjectScopedCooldownIsNotHiddenByAnEmptyTenantPool(t *testing.T) {
	resolver, store, _ := setupResolver(t)
	ctx := context.Background()
	future := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)

	if err := store.Create(ctx, &Account{
		ID: "acct-proj", TenantID: "tenant-a", ProjectID: "proj-1",
		Provider: AccountProviderClaude,
		Status:   AccountStatusLimited, AvailableAt: future,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-1",
		Provider: AccountProviderClaude, Auto: true,
	})
	var cooling *AccountsCoolingDownError
	if !errors.As(err, &cooling) {
		t.Fatalf("err = %v (%T), want the project's own cooldown", err, err)
	}
	if cooling.Candidates != 1 || !cooling.AvailableAt.Equal(future) {
		t.Errorf("cooldown = %+v, want 1 candidate returning at %v", cooling, future)
	}
}

// failingTenantWideStore answers project-scoped reads from the real store and
// fails the tenant-wide fallback read, which is the one lookup the resolver
// used to make best-effort.
type failingTenantWideStore struct {
	AccountStore
	err error
}

func (s *failingTenantWideStore) ListByTenant(ctx context.Context, tenantID string, provider AccountProvider, projectID string) ([]*Account, error) {
	// The fallback is the read with an EMPTY project scope; the first read
	// names the project. Distinguishing them is what lets this test fail only
	// the lookup under review.
	if projectID == "" {
		return nil, s.err
	}
	return s.AccountStore.ListByTenant(ctx, tenantID, provider, projectID)
}

// TestAccountResolver_Auto_AFailedFallbackReadIsNotAnEmptyPool is
// go-quicbaas#235's round-9 blocker.
//
// The tenant-wide fallback read was best-effort: its error was dropped and
// resolution fell through to ErrNoAccountAvailable. That was survivable while
// "no account available" meant only "try again later" — and stopped being
// survivable when #235 gave the error a consequence. The SDLC module now reads
// it as a proven configuration fault and PARKS the work item for a human, so a
// transient store failure stranded work behind a person even when an eligible
// tenant-wide account was sitting there unread.
func TestAccountResolver_Auto_AFailedFallbackReadIsNotAnEmptyPool(t *testing.T) {
	ctx := context.Background()
	storeErr := errors.New("kv: transient read failure")

	base := NewInMemoryAccountStore()
	// An account the fallback WOULD have found. Its presence is the point: the
	// pool is not empty, and answering "no account available" is not merely
	// imprecise, it is false.
	if err := base.Create(ctx, &Account{
		ID: "acct-tenant", TenantID: "tenant-a", Provider: AccountProviderClaude,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	secretStore := newTestAccountSecretStore(t)
	resolver := NewAccountResolver(
		&failingTenantWideStore{AccountStore: base, err: storeErr},
		NewAccountTokenManager(secretStore, base, 60*time.Second),
		NewAccountStrategyRegistry(nil),
	)

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-1",
		Provider: AccountProviderClaude, Auto: true,
	})
	if err == nil {
		t.Fatal("Resolve succeeded against a store that could not answer")
	}

	// THE STORE'S OWN ERROR, PRESERVED. A caller that retries on a backend
	// failure and parks on a configuration one must be able to tell them apart.
	if !errors.Is(err, storeErr) {
		t.Errorf("err = %v, want the store failure preserved", err)
	}
	if errors.Is(err, ErrNoAccountAvailable) {
		t.Error("a store that could not answer was reported as an empty pool; the SDLC " +
			"SDLC module parks on that, so a transient read failure would strand the work " +
			"item behind a human")
	}
	if errors.Is(err, ErrAccountsCoolingDown) {
		t.Error("a store failure was reported as a cooldown")
	}
}

// TestAccountResolver_Auto_AFailedProjectScopedReadAlreadySurfaced is the
// control: the FIRST read has always returned its error, and this pins that it
// still does — the fix must not have moved the problem rather than removed it.
func TestAccountResolver_Auto_AFailedProjectScopedReadAlreadySurfaced(t *testing.T) {
	ctx := context.Background()
	storeErr := errors.New("kv: transient read failure")

	resolver := NewAccountResolver(
		&alwaysFailingListStore{AccountStore: NewInMemoryAccountStore(), err: storeErr},
		NewAccountTokenManager(newTestAccountSecretStore(t), NewInMemoryAccountStore(), 60*time.Second),
		NewAccountStrategyRegistry(nil),
	)

	_, err := resolver.Resolve(ctx, AccountResolveRequest{
		TenantID: "tenant-a", ProjectID: "proj-1",
		Provider: AccountProviderClaude, Auto: true,
	})
	if !errors.Is(err, storeErr) {
		t.Errorf("err = %v, want the store failure preserved", err)
	}
	if errors.Is(err, ErrNoAccountAvailable) {
		t.Error("a failed project-scoped read was reported as an empty pool")
	}
}

// alwaysFailingListStore fails every listing.
type alwaysFailingListStore struct {
	AccountStore
	err error
}

func (s *alwaysFailingListStore) ListByTenant(context.Context, string, AccountProvider, string) ([]*Account, error) {
	return nil, s.err
}
