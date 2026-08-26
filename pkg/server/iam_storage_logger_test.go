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

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// memKV is an in-memory interfaces.KVStore, enough for the app-record paths
// BackendStorage uses. The interface is embedded so a method this test does not
// exercise fails loudly rather than silently returning a zero value.
type memKV struct {
	interfaces.KVStore
	mu sync.Mutex
	m  map[string][]byte
}

func newMemKV() *memKV { return &memKV{m: map[string][]byte{}} }

func (k *memKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = append([]byte(nil), value...)
	return nil
}

// Get returns a typed not-found ERROR for a missing key, not (nil, nil).
// BackendStorage.CreateApp treats a nil error as "this key already exists", so
// a double that reports absence as success turns every create into
// ErrAlreadyExists -- and the test would fail for a reason that has nothing to
// do with what it is testing.
func (k *memKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "memKV.get"}
	}
	return append([]byte(nil), v...), nil
}

func (k *memKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, key)
	return nil
}

func (k *memKV) Exists(_ context.Context, key string) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.m[key]
	return ok, nil
}

func (k *memKV) List(_ context.Context, prefix string) ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for key := range k.m {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out, nil
}

func (k *memKV) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	k.mu.Lock()
	snapshot := make(map[string][]byte, len(k.m))
	for key, v := range k.m {
		if strings.HasPrefix(key, prefix) {
			snapshot[key] = append([]byte(nil), v...)
		}
	}
	k.mu.Unlock()
	for key, v := range snapshot {
		if err := fn(key, v); err != nil {
			return err
		}
	}
	return nil
}

// TestNewIAMBackendStorage_WarnsOnPreKDFSecret pins the production wiring that
// gives IAM storage its operator-facing logger.
//
// The regression this guards is deliberately silent. Drop the logger and
// ValidateClientSecret still refuses a pre-KDF record, and the refusal is still
// collapsed into an opaque invalid_client before it reaches the caller — so the
// endpoint behaves IDENTICALLY from outside and every functional test stays
// green. The only casualty is the operator's one signal that a rotation is
// required, lost exactly during the upgrade that needs it.
//
// pkg/iam already proves the warning fires when a logger is supplied. What was
// unpinned is that pkg/server SUPPLIES one: the reviewer of #118 deleted the
// argument and nothing failed. So this drives newIAMBackendStorage — the real
// constructor initializeIAMComponents calls — rather than rebuilding the
// storage here, which would only test a copy.
func TestNewIAMBackendStorage_WarnsOnPreKDFSecret(t *testing.T) {
	ctx := context.Background()

	captured := &warnCapture{}
	store := newIAMBackendStorage(newMemKV(), captured)

	// A pre-#37 record: a stored secret that does not parse as the current
	// PBKDF2 digest. bcrypt's shape stands in for "whatever the old form was";
	// what matters is only that it is not a current digest.
	const clientID = "app_legacy_rotate_me"
	legacy := "$2a$10$abcdefghijklmnopqrstuv0123456789ABCDEFGHIJKLMNOPQRSTUV"
	if err := store.CreateApp(ctx, &iam.Application{
		ClientID:     clientID,
		TenantID:     "system",
		ClientSecret: legacy,
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// The storage reports the refusal as a typed error. The caller collapses it
	// into an opaque invalid_client before it reaches the wire, which is
	// exactly why the warning below is the operator's only signal.
	if _, err := store.ValidateClientSecret(ctx, clientID, "whatever"); !errors.Is(err, iam.ErrSecretRotationRequired) {
		t.Fatalf("validating against a pre-KDF digest returned %v, want ErrSecretRotationRequired: the record was not treated as legacy, so this test is not exercising the path it claims", err)
	}

	got := captured.String()
	if !strings.Contains(got, clientID) {
		t.Fatalf("no warning naming %s was emitted: the server built its IAM storage without a logger, so a pre-KDF record refuses silently and the operator is never told rotation is the remedy.\ncaptured: %q", clientID, got)
	}
	if !strings.Contains(strings.ToLower(got), "rotation") {
		t.Errorf("the warning does not name the remedy, so it does not tell an operator what to do: %q", got)
	}
}

// warnCapture is a minimal iamStorageLogger that records what it is told.
type warnCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnCapture) Warnf(format string, args ...interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, fmt.Sprintf(format, args...))
}

func (w *warnCapture) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.msgs, "\n")
}
