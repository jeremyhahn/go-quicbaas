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

package credentials

import (
	"bytes"
	"context"
	"crypto/rand"
	stderrors "errors"
	"io"
	"log/slog"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// newMemoryStore returns qrdb's manual-strategy credential service holding
// value under name.
func newMemoryStore(t *testing.T, name string, value []byte) *qrdbsdk.VersionedCredentialService {
	t.Helper()
	store, err := qrdbsdk.NewVersionedCredentialService(
		&qrdbsdk.CredentialConfig{Strategy: qrdbsdk.CredentialStrategyManual},
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("NewVersionedCredentialService: %v", err)
	}
	if value != nil {
		if err := store.StoreCredential(context.Background(), name, value); err != nil {
			t.Fatalf("StoreCredential: %v", err)
		}
	}
	return store
}

func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return key
}

func TestNewMemory_LendsTheStoredCredential(t *testing.T) {
	key := randomKey(t)
	store := newMemoryStore(t, "audit-sign-key", key)

	m, err := NewMemory(context.Background(), store, "audit-sign-key")
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	defer m.Destroy()

	if !m.IsAlive() {
		t.Fatal("IsAlive = false, want true")
	}
	if got := m.Size(); got != len(key) {
		t.Fatalf("Size = %d, want %d", got, len(key))
	}
	err = m.WithBytes(func(b []byte) error {
		if !bytes.Equal(b, key) {
			t.Error("borrowed bytes differ from the stored credential")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithBytes: %v", err)
	}
}

func TestNewMemory_RefusesBadInput(t *testing.T) {
	store := newMemoryStore(t, "audit-sign-key", randomKey(t))

	if _, err := NewMemory(context.Background(), nil, "audit-sign-key"); !stderrors.Is(err, ErrMemoryStoreNil) {
		t.Errorf("nil store: err = %v, want ErrMemoryStoreNil", err)
	}
	if _, err := NewMemory(context.Background(), store, ""); !stderrors.Is(err, ErrMemoryNameEmpty) {
		t.Errorf("empty name: err = %v, want ErrMemoryNameEmpty", err)
	}
}

func TestNewMemory_RefusesAnUnprovisionedCredential(t *testing.T) {
	store := newMemoryStore(t, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewMemory(ctx, store, "audit-sign-key")
	var retrieveErr *MemoryRetrieveError
	if !stderrors.As(err, &retrieveErr) {
		t.Fatalf("err = %v, want *MemoryRetrieveError", err)
	}
	if retrieveErr.Name != "audit-sign-key" {
		t.Errorf("Name = %q, want audit-sign-key", retrieveErr.Name)
	}
	if !stderrors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
}

func TestMemory_DestroyRefusesFurtherBorrows(t *testing.T) {
	store := newMemoryStore(t, "audit-sign-key", randomKey(t))
	m, err := NewMemory(context.Background(), store, "audit-sign-key")
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}

	m.Destroy()
	m.Destroy() // idempotent

	if m.IsAlive() {
		t.Error("IsAlive = true after Destroy")
	}
	if got := m.Size(); got != 0 {
		t.Errorf("Size = %d after Destroy, want 0", got)
	}
	called := false
	err = m.WithBytes(func([]byte) error {
		called = true
		return nil
	})
	if !stderrors.Is(err, ErrMemoryDestroyed) {
		t.Errorf("WithBytes err = %v, want ErrMemoryDestroyed", err)
	}
	if called {
		t.Error("WithBytes lent the bytes of a destroyed Memory")
	}
}

func TestMemory_WithBytesReturnsTheBorrowError(t *testing.T) {
	store := newMemoryStore(t, "audit-sign-key", randomKey(t))
	m, err := NewMemory(context.Background(), store, "audit-sign-key")
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	defer m.Destroy()

	errBorrow := stderrors.New("borrow failed")
	if err := m.WithBytes(func([]byte) error { return errBorrow }); !stderrors.Is(err, errBorrow) {
		t.Errorf("WithBytes err = %v, want the callback's error", err)
	}
}

func TestMemoryRetrieveError_Message(t *testing.T) {
	err := &MemoryRetrieveError{Name: "audit-sign-key", Err: context.Canceled}
	want := `retrieve credential "audit-sign-key": context canceled`
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
