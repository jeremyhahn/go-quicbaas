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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// failingSecretBackend answers every Get with one chosen error and embeds a
// memory backend for everything else, so a test can say exactly what the
// secrets layer reported without reimplementing it.
type failingSecretBackend struct {
	secrets.Backend
	getErr error
}

func (b *failingSecretBackend) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	return nil, nil, b.getErr
}

func newAccountSecretStoreWithBackendError(t *testing.T, getErr error) *AccountSecretStore {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{
		Backend: &failingSecretBackend{Backend: secrets.NewMemoryBackend(), getErr: getErr},
	})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	return NewAccountSecretStore(mgr)
}

func newTestAccountSecretStore(t *testing.T) *AccountSecretStore {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	return NewAccountSecretStore(mgr)
}

func TestAccountSecretStore_PutGetRoundTrip(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	cred := &AccountCredential{
		AccessToken:       "at-1",
		RefreshToken:      "rt-1",
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
	}
	if err := store.Put(ctx, "tenant-a", "acct-1", cred); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "acct-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.AccessToken != "at-1" || got.RefreshToken != "rt-1" {
		t.Fatalf("Get() = %+v, want access_token=at-1 refresh_token=rt-1", got)
	}
}

// TestAccountSecretStore_TenantIsolation proves one tenant's stored
// credential is never returned for another tenant, even for the same
// accountID.
func TestAccountSecretStore_TenantIsolation(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "tenant-a-secret"}); err != nil {
		t.Fatalf("Put(tenant-a) error = %v", err)
	}

	if _, err := store.Get(ctx, "tenant-b", "acct-1"); err == nil {
		t.Fatalf("Get(tenant-b, acct-1) unexpectedly succeeded — tenant isolation broken")
	}
}

func TestAccountSecretStore_Get_NotFound(t *testing.T) {
	store := newTestAccountSecretStore(t)
	if _, err := store.Get(context.Background(), "tenant-a", "missing"); err == nil {
		t.Fatalf("Get() of a missing credential succeeded, want AccountCredentialNotFoundError")
	}
}

func TestAccountSecretStore_Delete_IsIdempotentAndRemoves(t *testing.T) {
	store := newTestAccountSecretStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, "tenant-a", "acct-1", &AccountCredential{AccessToken: "x"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Delete(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, "tenant-a", "acct-1"); err == nil {
		t.Fatalf("Get() after Delete() succeeded")
	}
	// Idempotent: deleting again must not error.
	if err := store.Delete(ctx, "tenant-a", "acct-1"); err != nil {
		t.Fatalf("second Delete() error = %v, want nil (idempotent)", err)
	}
}

// TestAccountSecretStore_Get_SeparatesAMissFromAnOutage is go-quicbaas#235's
// requirement on this store.
//
// The two used to be one answer, which was harmless while nothing branched on
// it. The SDLC module now does: a missing credential is a configuration fault that
// PARKS the work item and refunds its attempt, because nothing short of
// re-importing the credential will help. A secrets backend that timed out is
// the opposite — it fixes itself, and parking on it takes an item out of the
// queue for a condition nobody has to act on. Flattening them would have made
// every backend blip a permanent park.
func TestAccountSecretStore_Get_SeparatesAMissFromAnOutage(t *testing.T) {
	tests := []struct {
		name string
		// backendErr is what the secrets backend answers with.
		backendErr error
		// wantMissing is whether Get should report the credential absent.
		wantMissing bool
	}{
		{
			name:        "a genuine miss",
			backendErr:  secrets.ErrSecretNotFound,
			wantMissing: true,
		},
		{
			name:        "a deleted secret is also a miss",
			backendErr:  secrets.ErrSecretDeleted,
			wantMissing: true,
		},
		{
			// THE ONE THAT MATTERS. Reported as absent, this parks the work
			// item permanently on a backend that was briefly unreachable.
			name:        "a backend failure",
			backendErr:  errors.New("connection refused"),
			wantMissing: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newAccountSecretStoreWithBackendError(t, tc.backendErr)

			_, err := store.Get(context.Background(), "tenant-a", "acct-1")
			if err == nil {
				t.Fatal("Get succeeded against a backend that answered with an error")
			}

			gotMissing := errors.Is(err, ErrAccountCredentialNotFound)
			if gotMissing != tc.wantMissing {
				t.Errorf("reported credential-missing = %v, want %v (err %v)",
					gotMissing, tc.wantMissing, err)
			}
			if !tc.wantMissing && !errors.Is(err, ErrAccountStore) {
				t.Errorf("a backend failure must surface as a store error, got %v", err)
			}
			// Either way the credential itself is never in the message.
			if strings.Contains(err.Error(), "acct-1-secret") {
				t.Errorf("the error carried credential material: %v", err)
			}
		})
	}
}
