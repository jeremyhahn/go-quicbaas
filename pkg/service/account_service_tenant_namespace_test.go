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

package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// The tests in this file cross the boundary go-quicbaas#535 was lost on: an
// account written through the SERVICE under one tenant's context, then read
// through the SPAWN path (AccountResolver) under the account's own tenant.
//
// The orchestration KV routes by the tenant on the context, not by the tenant
// segment in the key, and a prefix-isolation tenant gets its own namespace
// (go-qrdb tenant.StoreResolver.sharedFor). The system admin provisioning
// tenant T's pooled account therefore ran in the SYSTEM namespace, and T's
// SDLC module — which resolves under T's context — found an empty pool.

// guardedTestKV is the store shape accountStore requires: a KVStore that can
// decide a guarded create/delete inside the store (see task_store.go
// guardedCreator / guardedDeleter).
type guardedTestKV interface {
	interfaces.KVStore
	PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error)
	CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
}

// tenantNamespacedKV mirrors pkg/server's tenantRoutingKVStore for this
// package's tests: every verb resolves the tenant from the CONTEXT — pkg/iam's
// key first, then pkg/orchestration's, exactly the order the production router
// honours — and runs against that tenant's own namespace, as go-qrdb's
// StoreResolver confines a prefix-isolation tenant to "{tenant}/". The key's
// own tenant segment is never consulted, which is the property #535 turned on.
type tenantNamespacedKV struct {
	namespaces sync.Map // tenant ID -> guardedTestKV
}

func newTenantNamespacedKV() *tenantNamespacedKV {
	return &tenantNamespacedKV{}
}

// tenantOf reads the request tenant the way pkg/server.tenantFromAnyContext
// does. An untenanted context lands in the system namespace, as an untenanted
// request does in production.
func tenantOf(ctx context.Context) string {
	if tenantID := iam.TenantFromContext(ctx); tenantID != "" {
		return tenantID
	}
	if tenantID := orchestration.TenantFromContext(ctx); tenantID != "" {
		return tenantID
	}
	return orchestration.SystemTenantID
}

// namespace returns tenantID's own store, creating it on first use.
func (kv *tenantNamespacedKV) namespace(tenantID string) guardedTestKV {
	if existing, ok := kv.namespaces.Load(tenantID); ok {
		return existing.(guardedTestKV)
	}
	fresh, ok := orchestration.NewMemoryTaskKV().(guardedTestKV)
	if !ok {
		panic("orchestration.NewMemoryTaskKV() does not carry the guarded verbs accountStore requires")
	}
	actual, _ := kv.namespaces.LoadOrStore(tenantID, fresh)
	return actual.(guardedTestKV)
}

func (kv *tenantNamespacedKV) route(ctx context.Context) guardedTestKV {
	return kv.namespace(tenantOf(ctx))
}

func (kv *tenantNamespacedKV) Put(ctx context.Context, key string, value []byte) error {
	return kv.route(ctx).Put(ctx, key, value)
}

func (kv *tenantNamespacedKV) Get(ctx context.Context, key string) ([]byte, error) {
	return kv.route(ctx).Get(ctx, key)
}

func (kv *tenantNamespacedKV) Delete(ctx context.Context, key string) error {
	return kv.route(ctx).Delete(ctx, key)
}

func (kv *tenantNamespacedKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return kv.route(ctx).Scan(ctx, prefix, fn)
}

func (kv *tenantNamespacedKV) List(ctx context.Context, prefix string) ([]string, error) {
	return kv.route(ctx).List(ctx, prefix)
}

func (kv *tenantNamespacedKV) Exists(ctx context.Context, key string) (bool, error) {
	return kv.route(ctx).Exists(ctx, key)
}

func (kv *tenantNamespacedKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return kv.route(ctx).PutIfAbsent(ctx, key, value)
}

func (kv *tenantNamespacedKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return kv.route(ctx).CompareAndDelete(ctx, key, expected)
}

// keysIn lists every key physically stored in tenantID's namespace whose key
// contains needle. It reads the namespace DIRECTLY — not through the router —
// so a test can say where a record actually landed.
func (kv *tenantNamespacedKV) keysIn(t *testing.T, tenantID, needle string) []string {
	t.Helper()
	all, err := kv.namespace(tenantID).List(context.Background(), "")
	if err != nil {
		t.Fatalf("List(%q) error = %v", tenantID, err)
	}
	var matched []string
	for _, key := range all {
		if strings.Contains(key, needle) {
			matched = append(matched, key)
		}
	}
	return matched
}

// tenantNamespaceFixture is an AccountService and the spawn-path resolver it
// must agree with, both over the SAME context-routed KV — metadata and
// credential envelope alike, as on a deployed node.
type tenantNamespaceFixture struct {
	kv       *tenantNamespacedKV
	store    orchestration.AccountStore
	secrets  *orchestration.AccountSecretStore
	service  AccountService
	resolver orchestration.AccountResolver
}

func newTenantNamespaceFixture(t *testing.T) *tenantNamespaceFixture {
	t.Helper()
	kv := newTenantNamespacedKV()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewKVBackend(kv)})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	store := orchestration.NewAccountStore(kv)
	secretStore := orchestration.NewAccountSecretStore(mgr)
	tokens := orchestration.NewAccountTokenManager(secretStore, store, 0)
	return &tenantNamespaceFixture{
		kv:       kv,
		store:    store,
		secrets:  secretStore,
		service:  NewAccountService(store, secretStore),
		resolver: orchestration.NewAccountResolver(store, tokens, orchestration.NewAccountStrategyRegistry(nil)),
	}
}

const (
	// nsTenant is the tenant whose pooled account is provisioned — the
	// automatethethings tenant on the burn-in node.
	nsTenant = "automatethethings"
	// nsProject is the tenant-registered project whose spawn selects it.
	nsProject = "go-truststrap"
)

// systemAdminContext is the shape the REST admin path hands the service: the
// caller's tenant on pkg/iam's key, nothing on orchestration's.
func systemAdminContext() context.Context {
	return iam.ContextWithTenant(context.Background(), orchestration.SystemTenantID)
}

// spawnContext is the shape sdlc.runtimeContext hands the resolver: the
// PROJECT's tenant on orchestration's key, nothing on pkg/iam's.
func spawnContext(tenantID string) context.Context {
	return orchestration.ContextWithTenant(context.Background(), tenantID)
}

func (f *tenantNamespaceFixture) createForTenant(t *testing.T, ctx context.Context, token string) *orchestration.Account {
	t.Helper()
	created, err := f.service.CreateAccount(ctx, &CreateAccountRequest{
		TenantID: nsTenant,
		Provider: orchestration.AccountProviderClaude,
		Default:  true,
		Credential: &orchestration.AccountCredential{
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
			AccessToken:       token,
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	return created.Account
}

func (f *tenantNamespaceFixture) resolveAsSpawn(t *testing.T) (*orchestration.ResolvedAccountCredential, error) {
	t.Helper()
	return f.resolver.Resolve(spawnContext(nsTenant), orchestration.AccountResolveRequest{
		TenantID:  nsTenant,
		ProjectID: nsProject,
		Provider:  orchestration.AccountProviderClaude,
		Auto:      true,
	})
}

// TestAccountService_TenantNamespace_SystemAdminCreateIsSelectableBySpawn is
// the boundary-crossing test #535 asked for. Against the unscoped service it
// fails with NoAccountAvailableError: the record lands in the system
// namespace and the tenant's spawn path reads an empty pool.
func TestAccountService_TenantNamespace_SystemAdminCreateIsSelectableBySpawn(t *testing.T) {
	f := newTenantNamespaceFixture(t)

	account := f.createForTenant(t, systemAdminContext(), "sk-ant-oat01-test")

	resolved, err := f.resolveAsSpawn(t)
	if err != nil {
		t.Fatalf("spawn-path Resolve() under tenant %q error = %v, want the account the system admin created", nsTenant, err)
	}
	if resolved.AccountID != account.ID {
		t.Fatalf("Resolve().AccountID = %q, want %q", resolved.AccountID, account.ID)
	}
	if got := resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "sk-ant-oat01-test" {
		t.Fatalf("Resolve().Env[CLAUDE_CODE_OAUTH_TOKEN] = %q, want the credential the admin supplied", got)
	}

	// The record and its envelope live in the TENANT's namespace, and nothing
	// of them in the caller's.
	if keys := f.kv.keysIn(t, nsTenant, account.ID); len(keys) == 0 {
		t.Fatalf("no key for account %s in namespace %q", account.ID, nsTenant)
	}
	if keys := f.kv.keysIn(t, orchestration.SystemTenantID, account.ID); len(keys) != 0 {
		t.Fatalf("account %s leaked into the system namespace: %v", account.ID, keys)
	}
}

// TestAccountService_TenantNamespace_ListAndGetAgreeWithSpawn pins the other
// half of the #535 symptom: the admin's list and get must report the SAME
// pool the spawn path selects from, not a namespace the module never reads.
func TestAccountService_TenantNamespace_ListAndGetAgreeWithSpawn(t *testing.T) {
	f := newTenantNamespaceFixture(t)
	account := f.createForTenant(t, systemAdminContext(), "sk-ant-oat01-test")

	listed, err := f.service.ListAccounts(systemAdminContext(), &ListAccountsRequest{TenantID: nsTenant})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if listed.Total != 1 || listed.Accounts[0].ID != account.ID {
		t.Fatalf("ListAccounts() = %+v, want exactly the created account", listed.Accounts)
	}

	got, err := f.service.GetAccount(systemAdminContext(), &GetAccountRequest{TenantID: nsTenant, AccountID: account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if !got.Account.Default {
		t.Fatalf("GetAccount().Default = false, want the scope default the create claimed")
	}

	// And the tenant's own view is the same pool.
	tenantView, err := f.store.ListByTenant(spawnContext(nsTenant), nsTenant, orchestration.AccountProviderClaude, "")
	if err != nil {
		t.Fatalf("ListByTenant() under tenant context error = %v", err)
	}
	if len(tenantView) != 1 || tenantView[0].ID != account.ID {
		t.Fatalf("tenant-context ListByTenant() = %+v, want the account the admin listed", tenantView)
	}
}

// TestAccountService_TenantNamespace_RotateReachesSpawn: the replacement
// envelope must land where the spawn path reads it.
func TestAccountService_TenantNamespace_RotateReachesSpawn(t *testing.T) {
	f := newTenantNamespaceFixture(t)
	account := f.createForTenant(t, systemAdminContext(), "sk-ant-old")

	if _, err := f.service.RotateAccountCredential(systemAdminContext(), &RotateAccountCredentialRequest{
		TenantID:  nsTenant,
		AccountID: account.ID,
		Credential: &orchestration.AccountCredential{
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
			AccessToken:       "sk-ant-new",
		},
	}); err != nil {
		t.Fatalf("RotateAccountCredential() error = %v", err)
	}

	resolved, err := f.resolveAsSpawn(t)
	if err != nil {
		t.Fatalf("Resolve() after rotate error = %v", err)
	}
	if got := resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "sk-ant-new" {
		t.Fatalf("Resolve().Env[CLAUDE_CODE_OAUTH_TOKEN] = %q after rotate, want sk-ant-new", got)
	}
}

// TestAccountService_TenantNamespace_DeleteEmptiesTenantPool: a delete issued
// by the admin must remove the record AND the envelope from the tenant's
// namespace, so the spawn path stops selecting it and nothing is orphaned.
func TestAccountService_TenantNamespace_DeleteEmptiesTenantPool(t *testing.T) {
	f := newTenantNamespaceFixture(t)
	account := f.createForTenant(t, systemAdminContext(), "sk-ant-oat01-test")

	if _, err := f.service.DeleteAccount(systemAdminContext(), &DeleteAccountRequest{TenantID: nsTenant, AccountID: account.ID}); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}

	if _, err := f.resolveAsSpawn(t); !errors.Is(err, orchestration.ErrNoAccountAvailable) {
		t.Fatalf("Resolve() after delete error = %v, want ErrNoAccountAvailable", err)
	}
	if keys := f.kv.keysIn(t, nsTenant, account.ID); len(keys) != 0 {
		t.Fatalf("delete left keys for account %s in namespace %q: %v", account.ID, nsTenant, keys)
	}
	if _, err := f.secrets.Get(spawnContext(nsTenant), nsTenant, account.ID); !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Fatalf("credential envelope survived delete: err = %v, want ErrAccountCredentialNotFound", err)
	}
}

// TestAccountService_TenantNamespace_RevokeAndSetDefaultUpdateTenantRecord:
// the metadata verbs must mutate the record the spawn path reads.
func TestAccountService_TenantNamespace_RevokeAndSetDefaultUpdateTenantRecord(t *testing.T) {
	f := newTenantNamespaceFixture(t)
	first := f.createForTenant(t, systemAdminContext(), "sk-ant-first")
	second := f.createForTenant(t, systemAdminContext(), "sk-ant-second")

	if _, err := f.service.SetDefaultAccount(systemAdminContext(), &SetDefaultAccountRequest{TenantID: nsTenant, AccountID: second.ID}); err != nil {
		t.Fatalf("SetDefaultAccount() error = %v", err)
	}
	resolved, err := f.resolveAsSpawn(t)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccountID != second.ID {
		t.Fatalf("Resolve().AccountID = %q after SetDefault, want %q", resolved.AccountID, second.ID)
	}

	if _, err := f.service.RevokeAccount(systemAdminContext(), &RevokeAccountRequest{TenantID: nsTenant, AccountID: second.ID, Reason: "rotated out"}); err != nil {
		t.Fatalf("RevokeAccount() error = %v", err)
	}
	got, err := f.store.Get(spawnContext(nsTenant), nsTenant, second.ID)
	if err != nil {
		t.Fatalf("Get() under tenant context error = %v", err)
	}
	if got.Status != orchestration.AccountStatusRevoked {
		t.Fatalf("tenant-context Get().Status = %s after revoke, want revoked", got.Status)
	}

	updated, err := f.service.UpdateAccount(systemAdminContext(), &UpdateAccountRequest{TenantID: nsTenant, AccountID: first.ID, DisplayName: "renamed"})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if updated.Account.DisplayName != "renamed" {
		t.Fatalf("UpdateAccount().DisplayName = %q, want renamed", updated.Account.DisplayName)
	}
	if got, err := f.store.Get(spawnContext(nsTenant), nsTenant, first.ID); err != nil || got.DisplayName != "renamed" {
		t.Fatalf("tenant-context Get() = %+v, %v; want DisplayName renamed", got, err)
	}
}

// TestAccountService_TenantNamespace_RequestTenantOverridesCarriedTenant: a
// context that already names a DIFFERENT tenant on either key is overridden by
// the request's tenant — the request is the authority, and a caller allowed to
// name another tenant was admitted to by RBAC before reaching the service.
func TestAccountService_TenantNamespace_RequestTenantOverridesCarriedTenant(t *testing.T) {
	f := newTenantNamespaceFixture(t)

	carried := orchestration.ContextWithTenant(iam.ContextWithTenant(context.Background(), "burnin"), "burnin")
	account := f.createForTenant(t, carried, "sk-ant-oat01-test")

	if keys := f.kv.keysIn(t, "burnin", account.ID); len(keys) != 0 {
		t.Fatalf("account %s landed in the carried tenant's namespace: %v", account.ID, keys)
	}
	if keys := f.kv.keysIn(t, nsTenant, account.ID); len(keys) == 0 {
		t.Fatalf("account %s not in the requested tenant's namespace", account.ID)
	}
	if _, err := f.resolveAsSpawn(t); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
}

// TestAccountService_TenantNamespace_UsageProbeRunsInTenant: the usage verb
// existence-checks the record before consulting the tracker, and that check
// must run in the tenant's namespace too — otherwise a real account reads as
// missing and usage can never be reported for a tenant's pool.
func TestAccountService_TenantNamespace_UsageProbeRunsInTenant(t *testing.T) {
	f := newTenantNamespaceFixture(t)
	account := f.createForTenant(t, systemAdminContext(), "sk-ant-oat01-test")

	_, err := f.service.GetAccountUsage(systemAdminContext(), &GetAccountUsageRequest{TenantID: nsTenant, AccountID: account.ID})
	var noTracker *AccountUsageTrackerNotConfiguredError
	if !errors.As(err, &noTracker) {
		t.Fatalf("GetAccountUsage() error = %v, want AccountUsageTrackerNotConfiguredError (record found, tracker absent)", err)
	}
}
