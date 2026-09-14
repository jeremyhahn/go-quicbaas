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
)

// The store refuses a verb whose context is scoped to a tenant other than the
// one the request names (go-quicbaas#535): the KV beneath it routes by the
// context's tenant, so serving the request would put the record in a namespace
// its own tenant field disagrees with.

// scopeVerb runs one AccountStore verb for tenantID under ctx and returns its
// error, so the refusal can be asserted uniformly across the surface.
type scopeVerb struct {
	name string
	call func(ctx context.Context, store AccountStore, tenantID string) error
}

func scopeVerbs() []scopeVerb {
	return []scopeVerb{
		{"Create", func(ctx context.Context, store AccountStore, tenantID string) error {
			return store.Create(ctx, &Account{ID: "acct-scope", TenantID: tenantID, Provider: AccountProviderClaude})
		}},
		{"Get", func(ctx context.Context, store AccountStore, tenantID string) error {
			_, err := store.Get(ctx, tenantID, "acct-scope")
			return err
		}},
		{"Update", func(ctx context.Context, store AccountStore, tenantID string) error {
			return store.Update(ctx, &Account{ID: "acct-scope", TenantID: tenantID, Provider: AccountProviderClaude})
		}},
		{"Delete", func(ctx context.Context, store AccountStore, tenantID string) error {
			return store.Delete(ctx, tenantID, "acct-scope")
		}},
		{"ListByTenant", func(ctx context.Context, store AccountStore, tenantID string) error {
			_, err := store.ListByTenant(ctx, tenantID, "", "")
			return err
		}},
		{"SetDefault", func(ctx context.Context, store AccountStore, tenantID string) error {
			return store.SetDefault(ctx, tenantID, "acct-scope")
		}},
		{"Touch", func(ctx context.Context, store AccountStore, tenantID string) error {
			return store.Touch(ctx, tenantID, "acct-scope", TouchAccountOptions{MarkUsed: true})
		}},
	}
}

// TestAccountStore_ScopeCheck_RefusesForeignTenantContext: every verb refuses
// a context scoped to another tenant, before touching the store.
func TestAccountStore_ScopeCheck_RefusesForeignTenantContext(t *testing.T) {
	for _, verb := range scopeVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			store := NewInMemoryAccountStore()
			ctx := ContextWithTenant(context.Background(), "system")

			err := verb.call(ctx, store, "automatethethings")

			var scope *AccountTenantScopeError
			if !errors.As(err, &scope) {
				t.Fatalf("%s under a system-scoped context for tenant automatethethings: error = %v, want AccountTenantScopeError", verb.name, err)
			}
			if scope.ContextTenant != "system" || scope.TenantID != "automatethethings" {
				t.Fatalf("AccountTenantScopeError = %+v, want context=system tenant=automatethethings", scope)
			}
			if !errors.Is(err, ErrAccountTenantScope) {
				t.Fatalf("errors.Is(err, ErrAccountTenantScope) = false for %v", err)
			}
			// Nothing was written under the wrong namespace's key.
			if verb.name == "Create" {
				if _, getErr := store.Get(context.Background(), "automatethethings", "acct-scope"); !errors.Is(getErr, ErrAccountNotFound) {
					t.Fatalf("refused Create still stored the record: Get error = %v", getErr)
				}
			}
		})
	}
}

// TestAccountStore_ScopeCheck_AllowsMatchingAndUnscopedContexts: the same
// tenant on the context, or no tenant at all (in-process callers, tests), is
// served normally.
func TestAccountStore_ScopeCheck_AllowsMatchingAndUnscopedContexts(t *testing.T) {
	contexts := map[string]context.Context{
		"matching": ContextWithTenant(context.Background(), "automatethethings"),
		"unscoped": context.Background(),
	}
	for name, ctx := range contexts {
		t.Run(name, func(t *testing.T) {
			store := NewInMemoryAccountStore()
			for _, verb := range scopeVerbs() {
				err := verb.call(ctx, store, "automatethethings")
				if errors.Is(err, ErrAccountTenantScope) {
					t.Fatalf("%s refused a %s context: %v", verb.name, name, err)
				}
			}
			listed, err := store.ListByTenant(ctx, "automatethethings", "", "")
			if err != nil {
				t.Fatalf("ListByTenant() error = %v", err)
			}
			// Create ran first and Delete ran later in the verb order, so the
			// pool is empty again; what matters is that every verb was served.
			if len(listed) != 0 {
				t.Fatalf("ListByTenant() = %d accounts after Create+Delete, want 0", len(listed))
			}
		})
	}
}

func TestAccountTenantScopeError_Error(t *testing.T) {
	err := &AccountTenantScopeError{ContextTenant: "system", TenantID: "t1"}
	want := "account: context is scoped to tenant system but the request names tenant t1"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}
