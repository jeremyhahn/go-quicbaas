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

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials/credstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// materialLog records every message, arg and field any logger derived from
// it writes.
type materialLog struct {
	mu      sync.Mutex
	entries []string
}

func (m *materialLog) add(parts ...any) {
	m.mu.Lock()
	m.entries = append(m.entries, fmt.Sprint(parts...))
	m.mu.Unlock()
}

func (m *materialLog) contains(s string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if strings.Contains(e, s) {
			return true
		}
	}
	return false
}

func (m *materialLog) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

type materialLogger struct {
	log    *materialLog
	fields string
}

var _ interfaces.Logger = (*materialLogger)(nil)

func (l *materialLogger) rec(msg string, args ...any) {
	l.log.add(l.fields, " ", msg, " ", fmt.Sprint(args...))
}
func (l *materialLogger) Debug(msg string, args ...any)        { l.rec(msg, args...) }
func (l *materialLogger) Debugf(f string, args ...interface{}) { l.rec(fmt.Sprintf(f, args...)) }
func (l *materialLogger) Info(msg string, args ...any)         { l.rec(msg, args...) }
func (l *materialLogger) Infof(f string, args ...interface{})  { l.rec(fmt.Sprintf(f, args...)) }
func (l *materialLogger) Warn(msg string, args ...any)         { l.rec(msg, args...) }
func (l *materialLogger) Warnf(f string, args ...interface{})  { l.rec(fmt.Sprintf(f, args...)) }
func (l *materialLogger) Error(msg string, args ...any)        { l.rec(msg, args...) }
func (l *materialLogger) Errorf(f string, args ...interface{}) { l.rec(fmt.Sprintf(f, args...)) }
func (l *materialLogger) Fatal(msg string)                     { l.rec(msg) }
func (l *materialLogger) Fatalf(f string, args ...interface{}) { l.rec(fmt.Sprintf(f, args...)) }
func (l *materialLogger) WithField(key string, value interface{}) interfaces.Logger {
	return &materialLogger{log: l.log, fields: l.fields + fmt.Sprint(" ", key, "=", value)}
}
func (l *materialLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return &materialLogger{log: l.log, fields: l.fields + fmt.Sprint(" ", fields)}
}
func (l *materialLogger) WithError(err error) interfaces.Logger {
	return &materialLogger{log: l.log, fields: l.fields + fmt.Sprint(" error=", err)}
}

// materialStores is an in-memory credential store per tenant.
type materialStores struct {
	mu        sync.Mutex
	data      map[string]map[string][]byte
	failNextD int
	deletes   map[string]int
}

func newMaterialStores() *materialStores {
	return &materialStores{data: map[string]map[string][]byte{}, deletes: map[string]int{}}
}

func (f *materialStores) storeFor(ctx context.Context) (credstore.Store, error) {
	tid, _ := tenantctx.Lookup(ctx)
	return &materialStore{f: f, tenant: tid}, nil
}

func (f *materialStores) has(tenant, name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[tenant][name]
	return ok
}

type materialStore struct {
	f      *materialStores
	tenant string
}

func (s *materialStore) Put(_ context.Context, name string, value []byte) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if s.f.data[s.tenant] == nil {
		s.f.data[s.tenant] = map[string][]byte{}
	}
	s.f.data[s.tenant][name] = append([]byte(nil), value...)
	return nil
}

func (s *materialStore) Delete(_ context.Context, name string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.deletes[s.tenant]++
	if s.f.failNextD > 0 {
		s.f.failNextD--
		return errors.New("store unavailable")
	}
	if _, ok := s.f.data[s.tenant][name]; !ok {
		return qrdbsdk.ErrCredentialNotFound
	}
	delete(s.f.data[s.tenant], name)
	return nil
}

type materialFixture struct {
	svc    *dynamicSecretsHandler
	kv     interfaces.KVStore
	leases *lease.Manager
	stores *materialStores
	log    *materialLog
}

func newMaterialFixture(t *testing.T) *materialFixture {
	t.Helper()
	log := &materialLog{}
	logger := &materialLogger{log: log}
	kv := newDSStore()
	leaseMgr, err := lease.NewManager(&lease.ManagerConfig{Store: kv, Logger: logger})
	require.NoError(t, err)
	t.Cleanup(func() { _ = leaseMgr.Close() })

	stores := newMaterialStores()
	gen, err := credstore.NewGenerator(stores.storeFor)
	require.NoError(t, err)
	registry := credentials.NewGeneratorRegistry()
	require.NoError(t, registry.Register(gen))

	svc, err := NewDynamicSecretsHandler(context.Background(), &DynamicSecretsConfig{
		Store:             kv,
		LeaseManager:      leaseMgr,
		GeneratorRegistry: registry,
		Logger:            logger,
	})
	require.NoError(t, err)

	_, err = svc.CreateRole(context.Background(), &CreateCredentialRoleRequest{
		Name:          "app-token",
		GeneratorType: credstore.GeneratorType,
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.NoError(t, err)
	return &materialFixture{svc: svc, kv: kv, leases: leaseMgr, stores: stores, log: log}
}

func (f *materialFixture) generate(t *testing.T, ctx context.Context, ttl time.Duration) *GenerateCredentialResponse {
	t.Helper()
	resp, err := f.svc.GenerateCredential(ctx, &GenerateCredentialRequest{RoleName: "app-token", TTL: ttl})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Credential.Data["value"], "credential %s has no value in the response", resp.Credential.CredentialID)
	return resp
}

// waitExpired waits until the lease's own ExpiresAt has passed.
func waitExpired(t *testing.T, l *lease.Lease) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !time.Now().After(l.ExpiresAt) {
		require.True(t, time.Now().Before(deadline), "lease %s never expired", l.LeaseID)
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDynamicSecrets_MaterialNeverPersisted(t *testing.T) {
	ctx := context.Background()
	f := newMaterialFixture(t)
	resp := f.generate(t, ctx, 0)
	value := resp.Credential.Data["value"]
	id := resp.Credential.CredentialID
	require.Equal(t, "credential://dynamic-"+id, resp.Credential.Data["reference"])
	require.True(t, f.stores.has("", credstore.ReferenceName(id)))

	found := false
	require.NoError(t, f.kv.Scan(ctx, "", func(key string, v []byte) error {
		if strings.Contains(key, value) || strings.Contains(string(v), value) {
			found = true
		}
		return nil
	}))
	require.False(t, found, "the KV store carries credential %s's value", id)

	record, err := f.svc.loadCredential(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.Data, "credential %s's record carries Data", id)
	require.Equal(t, resp.LeaseID, record.LeaseID)

	held, err := f.leases.GetLease(ctx, resp.LeaseID)
	require.NoError(t, err)
	for k, v := range held.Metadata {
		require.False(t, strings.Contains(v, value), "lease metadata %q carries credential %s's value", k, id)
	}
}

func TestDynamicSecrets_MaterialNeverLogged(t *testing.T) {
	ctx := context.Background()
	f := newMaterialFixture(t)

	revoked := f.generate(t, ctx, 0)
	_, err := f.svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: revoked.LeaseID})
	require.NoError(t, err)

	expired := f.generate(t, ctx, time.Second)
	waitExpired(t, expired.Lease)
	n, _, err := f.leases.ReapExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.Positive(t, f.log.count())
	for _, resp := range []*GenerateCredentialResponse{revoked, expired} {
		require.False(t, f.log.contains(resp.Credential.Data["value"]),
			"a log entry carries credential %s's value", resp.Credential.CredentialID)
	}
}

func TestDynamicSecrets_MaterialRevokeDeletesFromStore(t *testing.T) {
	ctx := context.Background()
	f := newMaterialFixture(t)
	resp := f.generate(t, ctx, 0)
	name := credstore.ReferenceName(resp.Credential.CredentialID)
	require.True(t, f.stores.has("", name))

	_, err := f.svc.RevokeLease(ctx, &RevokeLeaseRequest{LeaseID: resp.LeaseID})
	require.NoError(t, err)
	require.False(t, f.stores.has("", name))
	record, err := f.svc.loadCredential(ctx, resp.Credential.CredentialID)
	require.NoError(t, err)
	require.Nil(t, record)
}

func TestDynamicSecrets_MaterialExpiryDeletesFromStore(t *testing.T) {
	ctx := context.Background()
	f := newMaterialFixture(t)
	resp := f.generate(t, ctx, time.Second)
	name := credstore.ReferenceName(resp.Credential.CredentialID)
	require.True(t, f.stores.has("", name))

	waitExpired(t, resp.Lease)
	n, _, err := f.leases.ReapExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.False(t, f.stores.has("", name))
	_, err = f.leases.Validate(ctx, resp.LeaseID)
	require.True(t, lease.IsLeaseRevoked(err), "lease %s not revoked: %v", resp.LeaseID, err)
}

func TestDynamicSecrets_MaterialExpiryUsesTheLeaseTenant(t *testing.T) {
	f := newMaterialFixture(t)
	resp := f.generate(t, tenantctx.WithTenant(context.Background(), "tenant-a"), time.Second)
	name := credstore.ReferenceName(resp.Credential.CredentialID)
	require.True(t, f.stores.has("tenant-a", name))
	require.Equal(t, "tenant-a", resp.Lease.Metadata["tenant_id"])

	waitExpired(t, resp.Lease)
	n, _, err := f.leases.ReapExpired(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.False(t, f.stores.has("tenant-a", name))
	f.stores.mu.Lock()
	defer f.stores.mu.Unlock()
	require.Equal(t, 1, f.stores.deletes["tenant-a"])
	require.Zero(t, f.stores.deletes[""], "the expiry teardown reached the system store")
}

func TestDynamicSecrets_MaterialExpiryFailureRetries(t *testing.T) {
	ctx := context.Background()
	f := newMaterialFixture(t)
	resp := f.generate(t, ctx, time.Second)
	name := credstore.ReferenceName(resp.Credential.CredentialID)
	f.stores.mu.Lock()
	f.stores.failNextD = 1
	f.stores.mu.Unlock()

	waitExpired(t, resp.Lease)
	n, _, err := f.leases.ReapExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.True(t, f.stores.has("", name))
	held, err := f.leases.GetLease(ctx, resp.LeaseID)
	require.NoError(t, err)
	require.Nil(t, held.RevokedAt)

	n, _, err = f.leases.ReapExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.False(t, f.stores.has("", name))
	_, err = f.leases.Validate(ctx, resp.LeaseID)
	require.True(t, lease.IsLeaseRevoked(err), "lease %s not revoked: %v", resp.LeaseID, err)
}
