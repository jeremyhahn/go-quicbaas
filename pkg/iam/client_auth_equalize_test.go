// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import (
	"context"
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// countDerivations swaps verifySecret for a counting wrapper for the duration
// of run and returns how many derivations run paid.
func countDerivations(t *testing.T, run func()) int {
	t.Helper()
	real := verifySecret
	t.Cleanup(func() { verifySecret = real })
	calls := 0
	verifySecret = func(stored, secret string) bool {
		calls++
		return real(stored, secret)
	}
	run()
	verifySecret = real
	return calls
}

func assertInvalidClientCredentials(t *testing.T, err error) {
	t.Helper()
	if err == nil || err.Error() != "invalid client credentials" {
		t.Fatalf("error = %v, want \"invalid client credentials\"", err)
	}
}

func TestValidateClient_UnknownClientPaysOneDerivation(t *testing.T) {
	m := NewAppManager(NewBackendStorage(newMockKVStore()), nil)
	for _, clientID := range []string{"app_t1_absent", "absent"} {
		t.Run(clientID, func(t *testing.T) {
			var err error
			got := countDerivations(t, func() {
				_, err = m.ValidateClient(context.Background(), clientID, "sk_guess")
			})
			if got != 1 {
				t.Fatalf("an unknown client must cost exactly 1 derivation, got %d", got)
			}
			assertInvalidClientCredentials(t, err)
		})
	}
}

func TestValidateClient_CorruptRecordPaysOneDerivation(t *testing.T) {
	kv := newMockKVStore()
	if err := kv.Put(appTenantContext(context.Background(), "t1"), appKey("t1", "app_t1_corrupt"), []byte("{not json")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	store := NewBackendStorage(kv)
	m := NewAppManager(store, nil)

	var err error
	got := countDerivations(t, func() {
		_, err = m.ValidateClient(context.Background(), "app_t1_corrupt", "sk_guess")
	})
	if got != 1 {
		t.Fatalf("a corrupt record must cost exactly 1 derivation, got %d", got)
	}
	assertInvalidClientCredentials(t, err)

	_, getErr := store.GetApp(context.Background(), "app_t1_corrupt")
	if !errors.As(getErr, new(*AppRecordCorruptError)) {
		t.Fatalf("GetApp error = %v, want AppRecordCorruptError", getErr)
	}
}

func newKnownClientManager(t *testing.T) *AppManager {
	t.Helper()
	hash, err := deriveSecretHash("sk_present")
	if err != nil {
		t.Fatalf("deriveSecretHash: %v", err)
	}
	store := NewBackendStorage(newMockKVStore())
	if err := store.CreateApp(context.Background(), &Application{ClientID: "app_t1_known", TenantID: "t1", ClientSecret: hash}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return NewAppManager(store, nil)
}

func TestValidateClient_KnownClientWrongSecretPaysOneDerivation(t *testing.T) {
	m := newKnownClientManager(t)
	var err error
	got := countDerivations(t, func() {
		_, err = m.ValidateClient(context.Background(), "app_t1_known", "sk_guess")
	})
	if got != 1 {
		t.Fatalf("a wrong secret must cost exactly 1 derivation, got %d", got)
	}
	assertInvalidClientCredentials(t, err)
}

func TestValidateClient_KnownClientRightSecretPaysOneDerivation(t *testing.T) {
	m := newKnownClientManager(t)
	var app *Application
	var err error
	got := countDerivations(t, func() {
		app, err = m.ValidateClient(context.Background(), "app_t1_known", "sk_present")
	})
	if got != 1 {
		t.Fatalf("the right secret must cost exactly 1 derivation, got %d", got)
	}
	if err != nil {
		t.Fatalf("ValidateClient: %v", err)
	}
	if app == nil || app.ClientID != "app_t1_known" {
		t.Fatalf("app = %+v, want app_t1_known", app)
	}
	if app.ClientSecret != "" {
		t.Fatal("ValidateClient returned the stored secret digest unredacted")
	}
}

func TestValidateClient_StoreOutageIsNotInvalidCredentials(t *testing.T) {
	kv := &failingKVStore{KVStore: newMockKVStore(), err: &dberrors.QBError{Code: dberrors.ErrInternal, Op: "mock.get"}}
	m := NewAppManager(NewBackendStorage(kv), nil)

	var err error
	got := countDerivations(t, func() {
		_, err = m.ValidateClient(context.Background(), "app_t1_any", "sk_guess")
	})
	if got != 0 {
		t.Fatalf("an outage must derive nothing, got %d", got)
	}
	if !errors.As(err, new(*ClientStoreUnavailableError)) {
		t.Fatalf("error = %v, want ClientStoreUnavailableError", err)
	}
}

func TestBackendStorage_ValidateClientSecret_CorruptRecordDerives(t *testing.T) {
	kv := newMockKVStore()
	if err := kv.Put(appTenantContext(context.Background(), "t1"), appKey("t1", "app_t1_corrupt"), []byte("{not json")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	store := NewBackendStorage(kv)

	var ok bool
	var err error
	got := countDerivations(t, func() {
		ok, err = store.ValidateClientSecret(context.Background(), "app_t1_corrupt", "sk_guess")
	})
	if got != 1 {
		t.Fatalf("a corrupt record must cost exactly 1 derivation, got %d", got)
	}
	if ok {
		t.Fatal("a corrupt record must not authenticate")
	}
	if !errors.As(err, new(*AppRecordCorruptError)) {
		t.Fatalf("error = %v, want AppRecordCorruptError", err)
	}
}
