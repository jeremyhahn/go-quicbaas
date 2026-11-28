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

package credstore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeStores is an in-memory credential store per tenant ("" is the system
// store).
type fakeStores struct {
	mu        sync.Mutex
	data      map[string]map[string][]byte
	putErr    error
	deleteErr error
	lastPut   []byte
	failFor   error
}

func newFakeStores() *fakeStores {
	return &fakeStores{data: map[string]map[string][]byte{}}
}

func (f *fakeStores) storeFor(ctx context.Context) (Store, error) {
	if f.failFor != nil {
		return nil, f.failFor
	}
	tid, _ := tenantctx.Lookup(ctx)
	return &fakeStore{f: f, tenant: tid}, nil
}

func (f *fakeStores) get(tenant, name string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[tenant][name]
	return v, ok
}

func (f *fakeStores) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.data {
		n += len(m)
	}
	return n
}

type fakeStore struct {
	f      *fakeStores
	tenant string
}

func (s *fakeStore) Put(_ context.Context, name string, value []byte) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.lastPut = append([]byte(nil), value...)
	if s.f.putErr != nil {
		return s.f.putErr
	}
	if s.f.data[s.tenant] == nil {
		s.f.data[s.tenant] = map[string][]byte{}
	}
	s.f.data[s.tenant][name] = append([]byte(nil), value...)
	return nil
}

func (s *fakeStore) Delete(_ context.Context, name string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if s.f.deleteErr != nil {
		return s.f.deleteErr
	}
	if _, ok := s.f.data[s.tenant][name]; !ok {
		return fmt.Errorf("fake delete: %w", qrdbsdk.ErrCredentialNotFound)
	}
	delete(s.f.data[s.tenant], name)
	return nil
}

func newTestGenerator(t *testing.T, f *fakeStores) *Generator {
	t.Helper()
	g, err := NewGenerator(f.storeFor)
	require.NoError(t, err)
	return g
}

func testRole(config map[string]string) *credentials.Role {
	return &credentials.Role{Name: "r", GeneratorType: GeneratorType, Config: config, DefaultTTL: time.Hour}
}

func TestCredstore_NewGeneratorRejectsNil(t *testing.T) {
	_, err := NewGenerator(nil)
	require.Error(t, err)
}

func TestCredstore_GenerateStoresOnlyInTheStore(t *testing.T) {
	ctx := context.Background()
	f := newFakeStores()
	g := newTestGenerator(t, f)
	require.True(t, g.KeepsMaterial())
	require.Equal(t, GeneratorType, g.Type())

	cred, err := g.Generate(ctx, testRole(nil))
	require.NoError(t, err)
	require.Equal(t, GeneratorType, cred.Type)
	require.Equal(t, "credential://dynamic-"+cred.CredentialID, cred.Data["reference"])
	require.Equal(t, "dynamic-"+cred.CredentialID, ReferenceName(cred.CredentialID))
	require.False(t, cred.ExpiresAt.Before(cred.CreatedAt.Add(time.Hour)))

	stored, ok := f.get("", ReferenceName(cred.CredentialID))
	require.True(t, ok, "credential %s not in the store", cred.CredentialID)
	require.True(t, string(stored) == cred.Data["value"], "stored value of credential %s differs", cred.CredentialID)
	decoded, err := base64.RawURLEncoding.DecodeString(cred.Data["value"])
	require.NoError(t, err)
	require.Len(t, decoded, 32)

	cred2, err := g.Generate(ctx, testRole(map[string]string{"length": "48", "encoding": "hex"}))
	require.NoError(t, err)
	require.Len(t, cred2.Data["value"], 96)
	_, err = hex.DecodeString(cred2.Data["value"])
	require.NoError(t, err)
}

func TestCredstore_TwoGenerationsDiffer(t *testing.T) {
	f := newFakeStores()
	g := newTestGenerator(t, f)
	a, err := g.Generate(context.Background(), testRole(nil))
	require.NoError(t, err)
	b, err := g.Generate(context.Background(), testRole(nil))
	require.NoError(t, err)
	require.NotEqual(t, a.CredentialID, b.CredentialID)
	require.True(t, a.Data["value"] != b.Data["value"], "credentials %s and %s share a value", a.CredentialID, b.CredentialID)
	require.Equal(t, 2, f.count())
}

func TestCredstore_RevokeDeletesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFakeStores()
	g := newTestGenerator(t, f)
	cred, err := g.Generate(ctx, testRole(nil))
	require.NoError(t, err)

	require.NoError(t, g.Revoke(ctx, cred))
	_, ok := f.get("", ReferenceName(cred.CredentialID))
	require.False(t, ok)
	require.NoError(t, g.Revoke(ctx, cred), "second revoke must succeed")

	f.deleteErr = fmt.Errorf("wrapped: %w", qrdbsdk.ErrCredentialNotFound)
	require.NoError(t, g.Revoke(ctx, cred))

	require.Error(t, g.Revoke(ctx, &credentials.Credential{}))
	require.Error(t, g.Revoke(ctx, nil))
}

func TestCredstore_TenantIsolation(t *testing.T) {
	f := newFakeStores()
	g := newTestGenerator(t, f)
	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")

	cred, err := g.Generate(ctxA, testRole(nil))
	require.NoError(t, err)
	name := ReferenceName(cred.CredentialID)
	_, ok := f.get("tenant-a", name)
	require.True(t, ok)
	_, ok = f.get("tenant-b", name)
	require.False(t, ok)
	_, ok = f.get("", name)
	require.False(t, ok)

	require.NoError(t, g.Revoke(ctxB, cred))
	_, ok = f.get("tenant-a", name)
	require.True(t, ok, "revoke under tenant-b removed tenant-a's credential %s", cred.CredentialID)
}

func TestCredstore_StoreForFailsClosed(t *testing.T) {
	f := newFakeStores()
	f.failFor = errors.New("tenant has no unsealed barrier")
	g := newTestGenerator(t, f)
	_, err := g.Generate(tenantctx.WithTenant(context.Background(), "tenant-x"), testRole(nil))
	require.Error(t, err)
	require.Equal(t, 0, f.count())
}

func TestCredstore_ValidateConfig(t *testing.T) {
	g := newTestGenerator(t, newFakeStores())
	cases := []struct {
		name   string
		config map[string]string
		ok     bool
		key    string
	}{
		{"defaults", nil, true, ""},
		{"min", map[string]string{"length": "16"}, true, ""},
		{"max hex", map[string]string{"length": "1024", "encoding": "hex"}, true, ""},
		{"base64", map[string]string{"encoding": "base64"}, true, ""},
		{"too short", map[string]string{"length": "15"}, false, "length"},
		{"too long", map[string]string{"length": "1025"}, false, "length"},
		{"not a number", map[string]string{"length": "abc"}, false, "length"},
		{"bad encoding", map[string]string{"encoding": "rot13"}, false, "encoding"},
		{"unknown key", map[string]string{"prefix": "x"}, false, "prefix"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := g.ValidateConfig(tc.config)
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.key)
			_, genErr := g.Generate(context.Background(), testRole(tc.config))
			require.Error(t, genErr)
		})
	}
}

func TestCredstore_ErrorsNeverCarryTheValue(t *testing.T) {
	ctx := context.Background()
	f := newFakeStores()
	f.putErr = errors.New("put refused")
	g := newTestGenerator(t, f)

	_, err := g.Generate(ctx, testRole(nil))
	require.Error(t, err)
	require.NotEmpty(t, f.lastPut)
	require.False(t, strings.Contains(err.Error(), string(f.lastPut)), "generate error carries the secret value")
	require.Equal(t, 0, f.count())

	f.putErr = nil
	cred, err := g.Generate(ctx, testRole(nil))
	require.NoError(t, err)
	f.deleteErr = errors.New("delete refused")
	err = g.Revoke(ctx, cred)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), cred.Data["value"]), "revoke error for credential %s carries the secret value", cred.CredentialID)
	require.Contains(t, err.Error(), cred.CredentialID)
}

func TestCredstore_RotateKeepsTheNewCredentialOnRevokeFailure(t *testing.T) {
	ctx := context.Background()
	f := newFakeStores()
	g := newTestGenerator(t, f)
	old, err := g.Generate(ctx, testRole(nil))
	require.NoError(t, err)

	f.deleteErr = errors.New("delete refused")
	next, err := g.Rotate(ctx, old, testRole(nil))
	require.Error(t, err)
	require.NotNil(t, next)
	require.NotEqual(t, old.CredentialID, next.CredentialID)
	_, ok := f.get("", ReferenceName(next.CredentialID))
	require.True(t, ok)

	f.deleteErr = nil
	rotated, err := g.Rotate(ctx, next, testRole(nil))
	require.NoError(t, err)
	_, ok = f.get("", ReferenceName(next.CredentialID))
	require.False(t, ok)
	_, ok = f.get("", ReferenceName(rotated.CredentialID))
	require.True(t, ok)
}
