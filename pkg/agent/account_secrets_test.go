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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The daemon's read-through credential store (go-quicbaas#734).
//
// Two properties are load-bearing and are tested directly here rather than
// only through the resolver:
//
//  1. Get is the ONLY method that produces credential material. Every write-ish
//     method refuses, so a bug that reached one fails visibly instead of
//     leaving this host the sole holder of a credential.
//  2. The freshness window is derived from the credential's OWN expiry, with a
//     margin that keeps AccountTokenManager's (impossible-here) renewal path
//     out of reach.
// =============================================================================

// newTestMaterializingSecrets builds the daemon's credential store against a
// fake central.
func newTestMaterializingSecrets(t *testing.T, central *fakeCentral) (*materializingSecrets, *accountCredentialCache) {
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
	return newMaterializingSecrets(client, cache, &testLogger{t: t}), cache
}

// accountPath is the SecretsManager path for one account, built through the
// exported helper so the test cannot drift from the scheme.
func accountPath(t *testing.T, tenantID, accountID string) string {
	t.Helper()
	path, err := orchestration.AccountSecretPath(tenantID, accountID)
	if err != nil {
		t.Fatalf("AccountSecretPath() error = %v", err)
	}
	return path
}

// TestMaterializingSecrets_GetReleasesAndCaches covers the one working method.
func TestMaterializingSecrets_GetReleasesAndCaches(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, _ := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	data, metadata, err := secrets.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	body, ok := data[orchestration.AccountSecretDataKey]
	if !ok || len(body) == 0 {
		t.Fatalf("Get() returned no envelope under %q", orchestration.AccountSecretDataKey)
	}
	var cred orchestration.AccountCredential
	if err := json.Unmarshal(body, &cred); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	// Shape, not value.
	if cred.AccessToken == "" || cred.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Error("released envelope is missing the material a spawn needs")
	}
	if cred.RefreshToken != "" {
		t.Error("released envelope carries a refresh token a daemon must never hold")
	}
	if metadata["source"] != "control_plane" {
		t.Errorf("metadata source = %q, want control_plane", metadata["source"])
	}

	// Second read is served from cache.
	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if got := central.releaseCount("acct-1"); got != 1 {
		t.Errorf("released %d times across two reads, want 1", got)
	}
}

// TestMaterializingSecrets_GetReturnsACopy proves a caller that mutates the
// bytes it received cannot corrupt what the next spawn is handed.
func TestMaterializingSecrets_GetReturnsACopy(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, _ := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	first, _, err := secrets.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	body := first[orchestration.AccountSecretDataKey]
	for i := range body {
		body[i] = 'x'
	}

	second, _, err := secrets.Get(ctx, path)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	var cred orchestration.AccountCredential
	if err := json.Unmarshal(second[orchestration.AccountSecretDataKey], &cred); err != nil {
		t.Fatalf("cached envelope was corrupted by a previous reader: %v", err)
	}
	if cred.AccessToken == "" {
		t.Error("cached envelope lost its token after a reader mutated its copy")
	}
}

// TestMaterializingSecrets_RefusesAForeignPath proves the store never guesses
// at an address it does not own: a misparsed path would read one account's
// credential under another account's name.
func TestMaterializingSecrets_RefusesAForeignPath(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	secrets, _ := newTestMaterializingSecrets(t, central)

	for _, path := range []string{
		"",
		"some/other/namespace/x",
		"orchestration/agent-accounts/",
		"orchestration/agent-accounts/tenant-a",
		"orchestration/agent-accounts//acct-1",
		"orchestration/agent-accounts/tenant-a/acct-1/extra",
	} {
		_, _, err := secrets.Get(context.Background(), path)
		var unsupported *AccountSecretsUnsupportedPathError
		if !errors.As(err, &unsupported) {
			t.Errorf("Get(%q) error = %v (%T), want *AccountSecretsUnsupportedPathError", path, err, err)
		}
	}
	if central.releaseCount("acct-1") != 0 {
		t.Error("a foreign path reached the control plane")
	}
}

// TestMaterializingSecrets_RefusesAnotherTenantsPath proves a well-formed path
// naming a different tenant is refused before it becomes a request.
func TestMaterializingSecrets_RefusesAnotherTenantsPath(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	secrets, _ := newTestMaterializingSecrets(t, central)

	_, _, err := secrets.Get(context.Background(), accountPath(t, "tenant-b", "acct-1"))
	var mismatch *AccountAPITenantMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v (%T), want *AccountAPITenantMismatchError", err, err)
	}
}

// TestMaterializingSecrets_RefusesEveryWrite walks the surface a daemon must
// not have. Each refusal names the method so a stack trace is not needed to
// tell which one was reached.
func TestMaterializingSecrets_RefusesEveryWrite(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	secrets, _ := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	cases := []struct {
		name   string
		call   func() error
		method string
	}{
		{"store", func() error { return secrets.Store(ctx, path, nil, nil) }, "Store"},
		{"store with options", func() error {
			return secrets.StoreWithOptions(ctx, path, nil, nil, &interfaces.StoreOptions{TenantID: "tenant-a"})
		}, "StoreWithOptions"},
		{"get version", func() error { _, _, err := secrets.GetVersion(ctx, path, 1); return err }, "GetVersion"},
		{"list", func() error { _, err := secrets.List(ctx, "orchestration/"); return err }, "List"},
		{"destroy", func() error { return secrets.Destroy(ctx, path) }, "Destroy"},
		{"list versions", func() error { _, err := secrets.ListVersions(ctx, path); return err }, "ListVersions"},
		{"rollback", func() error { return secrets.Rollback(ctx, path, 1) }, "Rollback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			var readOnly *AccountSecretsReadOnlyError
			if !errors.As(err, &readOnly) {
				t.Fatalf("error = %v (%T), want *AccountSecretsReadOnlyError", err, err)
			}
			if readOnly.Method != tc.method {
				t.Errorf("refusal names %q, want %q", readOnly.Method, tc.method)
			}
			if !strings.Contains(readOnly.Error(), "control plane") {
				t.Errorf("refusal %q does not say who owns the credential", readOnly.Error())
			}
		})
	}
}

// TestMaterializingSecrets_DeleteForgetsLocally proves the one refusal-adjacent
// method that does something: forgetting a cached envelope is exactly what a
// daemon may do with it, and the durable record is untouched.
func TestMaterializingSecrets_DeleteForgetsLocally(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	secrets, cache := newTestMaterializingSecrets(t, central)
	ctx := context.Background()
	path := accountPath(t, "tenant-a", "acct-1")

	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, ok := cache.lookup("acct-1", time.Now().UTC()); !ok {
		t.Fatal("test setup: the release was not cached")
	}

	if err := secrets.Delete(ctx, path); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, ok := cache.lookup("acct-1", time.Now().UTC()); ok {
		t.Error("Delete() left the envelope cached")
	}

	// The next read re-releases rather than failing: the record still exists
	// on the control plane.
	if _, _, err := secrets.Get(ctx, path); err != nil {
		t.Fatalf("Get() after Delete() error = %v", err)
	}
	if got := central.releaseCount("acct-1"); got != 2 {
		t.Errorf("released %d times, want 2", got)
	}

	var unsupported *AccountSecretsUnsupportedPathError
	if err := secrets.Delete(ctx, "not/an/account/path"); !errors.As(err, &unsupported) {
		t.Errorf("Delete(foreign path) error = %v (%T), want *AccountSecretsUnsupportedPathError", err, err)
	}
}

// TestMaterializingSecrets_CloseDropsEveryEnvelope proves a stopped daemon
// holds no credentials.
func TestMaterializingSecrets_CloseDropsEveryEnvelope(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
	central.addAccount("acct-2", "claude", "", false)
	secrets, cache := newTestMaterializingSecrets(t, central)
	ctx := context.Background()

	for _, id := range []string{"acct-1", "acct-2"} {
		if _, _, err := secrets.Get(ctx, accountPath(t, "tenant-a", id)); err != nil {
			t.Fatalf("Get(%s) error = %v", id, err)
		}
	}
	if err := secrets.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for _, id := range []string{"acct-1", "acct-2"} {
		if _, ok := cache.lookup(id, time.Now().UTC()); ok {
			t.Errorf("Close() left %s cached", id)
		}
	}
}

// TestMaterializingSecrets_BackendTypeIsIdentifiable keeps the store legible in
// diagnostics rather than claiming to be the platform vault.
func TestMaterializingSecrets_BackendTypeIsIdentifiable(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	secrets, _ := newTestMaterializingSecrets(t, central)
	if got := secrets.GetBackendType(); got != "agent-materializing" {
		t.Errorf("GetBackendType() = %q, want agent-materializing", got)
	}
}

// =============================================================================
// Freshness
// =============================================================================

// TestAccountCacheFreshUntil covers every branch of the TTL decision, which is
// what keeps AccountTokenManager's renewal path — impossible on a daemon —
// unreachable rather than merely unlikely.
func TestAccountCacheFreshUntil(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		expiresAt time.Time
		want      time.Time
	}{
		{
			name:      "no declared expiry falls back to the ceiling",
			expiresAt: time.Time{},
			want:      now.Add(accountCacheMaxTTL),
		},
		{
			name:      "a long-lived credential is capped by the ceiling",
			expiresAt: now.Add(24 * time.Hour),
			want:      now.Add(accountCacheMaxTTL),
		},
		{
			name:      "a short-lived credential expires a margin early",
			expiresAt: now.Add(5 * time.Minute),
			want:      now.Add(5 * time.Minute).Add(-accountCacheRenewalMargin),
		},
		{
			name:      "a credential already inside its margin is not cached",
			expiresAt: now.Add(accountCacheRenewalMargin / 2),
			want:      now,
		},
		{
			name:      "an expired credential is not cached",
			expiresAt: now.Add(-time.Hour),
			want:      now,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := accountCacheFreshUntil(tc.expiresAt, now); !got.Equal(tc.want) {
				t.Errorf("accountCacheFreshUntil() = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestAccountCacheRenewalMargin_ExceedsTheManagersSkew pins the relationship
// the comment on the constant asserts. If the margin ever stopped exceeding the
// token manager's own skew, a daemon would start trying to renew a credential
// it has no refresh token for and fail every pooled spawn.
func TestAccountCacheRenewalMargin_ExceedsTheManagersSkew(t *testing.T) {
	if accountCacheRenewalMargin <= orchestration.DefaultAccountRefreshSkew {
		t.Fatalf("cache margin %s does not exceed the token manager's skew %s; "+
			"the daemon would reach a renewal path it cannot complete",
			accountCacheRenewalMargin, orchestration.DefaultAccountRefreshSkew)
	}
}

// TestAccountCredentialCache_Observe covers the invalidation rule directly,
// including the two cases that must NOT evict.
func TestAccountCredentialCache_Observe(t *testing.T) {
	released := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	fresh := func() *cachedAccountCredential {
		return mustCachedCredential(t, []byte(`{}`), released, time.Now().UTC().Add(time.Hour))
	}

	cases := []struct {
		name            string
		accountID       string
		lastRefreshedAt time.Time
		wantGone        bool
	}{
		{"newer replacement evicts", "acct-1", released.Add(time.Second), true},
		{"same replacement keeps", "acct-1", released, false},
		{"older replacement keeps", "acct-1", released.Add(-time.Hour), false},
		{"never replaced keeps", "acct-1", time.Time{}, false},
		{"empty id keeps", "", released.Add(time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := &accountCredentialCache{}
			cache.store("acct-1", fresh())
			cache.Observe(tc.accountID, tc.lastRefreshedAt)
			_, present := cache.lookup("acct-1", time.Now().UTC())
			if present == tc.wantGone {
				t.Errorf("entry present = %t, want %t", present, !tc.wantGone)
			}
		})
	}

	// Observing an account that is not cached at all is a no-op, not a panic.
	cache := &accountCredentialCache{}
	cache.Observe("never-cached", time.Now().UTC())
}

// TestAccountCredentialCache_LookupRespectsFreshness proves a stale entry is a
// miss rather than being served.
func TestAccountCredentialCache_LookupRespectsFreshness(t *testing.T) {
	now := time.Now().UTC()
	cache := &accountCredentialCache{}
	cache.store("acct-1", mustCachedCredential(t, []byte(`{}`), time.Time{}, now.Add(time.Minute)))

	if _, ok := cache.lookup("acct-1", now); !ok {
		t.Error("a fresh entry was reported as a miss")
	}
	if _, ok := cache.lookup("acct-1", now.Add(2*time.Minute)); ok {
		t.Error("a stale entry was served")
	}
	if _, ok := cache.lookup("absent", now); ok {
		t.Error("an absent entry was reported as a hit")
	}
}

// TestAccountSecretsErrors_AreLegible covers the error strings, which are what
// an operator actually reads when a worker host refuses a spawn.
func TestAccountSecretsErrors_AreLegible(t *testing.T) {
	readOnly := (&AccountSecretsReadOnlyError{Method: "Store", Path: "orchestration/agent-accounts/t/a"}).Error()
	if !strings.Contains(readOnly, "Store") || !strings.Contains(readOnly, "agent-accounts") {
		t.Errorf("read-only refusal %q is missing the method or the path", readOnly)
	}
	unsupported := (&AccountSecretsUnsupportedPathError{Path: "elsewhere"}).Error()
	if !strings.Contains(unsupported, "elsewhere") {
		t.Errorf("unsupported-path refusal %q does not name the path", unsupported)
	}
}

// =============================================================================
// Secure-memory hardening (go-quicbaas#757)
//
// A released envelope is a live provider OAuth token. These tests pin the
// properties that keep it out of swappable, GC-lingering heap: it lives in a
// memguard buffer, the buffer is wiped the moment its entry leaves the cache,
// and the token value never reaches a log.
// =============================================================================

// mustCachedCredential seals encoded into a cache entry, failing the test if
// the secure buffer cannot be allocated. It is the only way tests build an
// entry, so no test constructs one whose zero reference count would leave it
// unreadable.
func mustCachedCredential(t *testing.T, encoded []byte, releasedUnder, freshUntil time.Time) *cachedAccountCredential {
	t.Helper()
	entry, err := newCachedAccountCredential(encoded, releasedUnder, freshUntil, accountDeliveryGrant{})
	if err != nil {
		t.Fatalf("newCachedAccountCredential() error = %v", err)
	}
	return entry
}

// TestCachedAccountCredential_EvictionWipesTheBuffer proves the load-bearing
// hardening property: once an entry is dropped, its memguard buffer is
// destroyed and no plaintext is retained. A reader that arrives after the
// eviction sees a destroyed buffer, not a stale token.
func TestCachedAccountCredential_EvictionWipesTheBuffer(t *testing.T) {
	const secret = `{"access_token":"super-secret-token"}`
	cache := &accountCredentialCache{}
	entry := mustCachedCredential(t, []byte(secret), time.Time{}, time.Now().UTC().Add(time.Hour))
	cache.store("acct-1", entry)

	// While cached, the buffer still holds the sealed envelope.
	if got := entry.secret.Bytes(); !bytes.Equal(got, []byte(secret)) {
		t.Fatalf("cached buffer = %q, want the sealed envelope", got)
	}

	cache.Forget("acct-1")

	// Eviction dropped the cache's only reference, so the buffer is destroyed
	// and returns no bytes — the plaintext is not left for the GC.
	if got := entry.secret.Bytes(); got != nil {
		t.Errorf("buffer still readable after eviction: %q", got)
	}
	// A retired entry can never be read again.
	if _, ok := entry.copyOut(); ok {
		t.Error("copyOut succeeded on a retired entry")
	}
}

// TestCachedAccountCredential_StoreWipesTheReplaced proves a rotation that
// replaces an entry wipes the superseded buffer rather than leaving the old
// token resident until GC.
func TestCachedAccountCredential_StoreWipesTheReplaced(t *testing.T) {
	cache := &accountCredentialCache{}
	old := mustCachedCredential(t, []byte(`{"access_token":"old"}`), time.Time{}, time.Now().UTC().Add(time.Hour))
	cache.store("acct-1", old)

	replacement := mustCachedCredential(t, []byte(`{"access_token":"new"}`), time.Time{}, time.Now().UTC().Add(time.Hour))
	cache.store("acct-1", replacement)

	if got := old.secret.Bytes(); got != nil {
		t.Errorf("replaced buffer still readable: %q", got)
	}
	body, ok := replacement.copyOut()
	if !ok || !bytes.Contains(body, []byte(`"new"`)) {
		t.Errorf("replacement not served: body=%q ok=%t", body, ok)
	}
}

// TestMaterializingSecrets_CloseWipesEveryBuffer proves a stopped daemon leaves
// no credential plaintext behind in secure memory.
func TestMaterializingSecrets_CloseWipesEveryBuffer(t *testing.T) {
	cache := &accountCredentialCache{}
	entries := make([]*cachedAccountCredential, 0, 2)
	for _, id := range []string{"acct-1", "acct-2"} {
		e := mustCachedCredential(t, []byte(`{"access_token":"t-`+id+`"}`), time.Time{}, time.Now().UTC().Add(time.Hour))
		cache.store(id, e)
		entries = append(entries, e)
	}
	store := newMaterializingSecrets(nil, cache, nil)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for i, e := range entries {
		if got := e.secret.Bytes(); got != nil {
			t.Errorf("entry %d buffer still readable after Close: %q", i, got)
		}
	}
}

// TestCachedAccountCredential_ReadOutlivesEviction proves the reference count
// keeps a reader safe: a copyOut in progress reads valid bytes even though the
// cache evicted the entry, and the buffer is wiped only once the reader is
// done. Run under -race this is the concurrent-safety proof for the lock-free
// read path.
func TestCachedAccountCredential_ReadOutlivesEviction(t *testing.T) {
	const secret = `{"access_token":"racing-token"}`
	cache := &accountCredentialCache{}
	entry := mustCachedCredential(t, []byte(secret), time.Time{}, time.Now().UTC().Add(time.Hour))
	cache.store("acct-1", entry)

	// A reader holds a reference across a concurrent eviction.
	if !entry.acquire() {
		t.Fatal("acquire on a live entry failed")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cache.Forget("acct-1")
	}()
	wg.Wait()

	// The buffer must still be readable: our reference outranks the eviction.
	if got := entry.secret.Bytes(); !bytes.Equal(got, []byte(secret)) {
		t.Errorf("in-flight read saw %q, want the sealed envelope", got)
	}
	entry.release()

	// Now the last reference is gone and the buffer is wiped.
	if got := entry.secret.Bytes(); got != nil {
		t.Errorf("buffer still readable after the last reader released: %q", got)
	}
}

// capturingLogger records every formatted line so a test can assert what did —
// and did not — reach the log sink.
type capturingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *capturingLogger) record(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}

func (l *capturingLogger) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func (l *capturingLogger) Debug(msg string, _ ...any)        { l.record(msg) }
func (l *capturingLogger) Debugf(f string, a ...interface{}) { l.record(fmt.Sprintf(f, a...)) }
func (l *capturingLogger) Info(msg string, _ ...any)         { l.record(msg) }
func (l *capturingLogger) Infof(f string, a ...interface{})  { l.record(fmt.Sprintf(f, a...)) }
func (l *capturingLogger) Warn(msg string, _ ...any)         { l.record(msg) }
func (l *capturingLogger) Warnf(f string, a ...interface{})  { l.record(fmt.Sprintf(f, a...)) }
func (l *capturingLogger) Error(msg string, _ ...any)        { l.record(msg) }
func (l *capturingLogger) Errorf(f string, a ...interface{}) { l.record(fmt.Sprintf(f, a...)) }
func (l *capturingLogger) Fatal(msg string)                  { l.record(msg) }
func (l *capturingLogger) Fatalf(f string, a ...interface{}) { l.record(fmt.Sprintf(f, a...)) }
func (l *capturingLogger) WithField(_ string, _ interface{}) interfaces.Logger {
	return l
}
func (l *capturingLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *capturingLogger) WithError(_ error) interfaces.Logger                   { return l }

// TestMaterializingSecrets_NeverLogsTheEnvelope proves the release path logs
// the account ID and the cache expiry, and never the token itself.
func TestMaterializingSecrets_NeverLogsTheEnvelope(t *testing.T) {
	central := newFakeCentral(t, "tenant-a")
	central.addAccount("acct-1", "claude", "", true)
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
	log := &capturingLogger{}
	secrets := newMaterializingSecrets(client, &accountCredentialCache{}, log)

	data, _, err := secrets.Get(context.Background(), accountPath(t, "tenant-a", "acct-1"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	var cred orchestration.AccountCredential
	if err := json.Unmarshal(data[orchestration.AccountSecretDataKey], &cred); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if cred.AccessToken == "" {
		t.Fatal("test setup: released envelope carried no token to check against")
	}

	logged := log.joined()
	if strings.Contains(logged, cred.AccessToken) {
		t.Errorf("the access token reached the log:\n%s", logged)
	}
	if !strings.Contains(logged, "acct-1") {
		t.Errorf("the release was not logged at all:\n%s", logged)
	}
}
