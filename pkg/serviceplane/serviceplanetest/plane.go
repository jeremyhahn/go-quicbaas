// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

// Package serviceplanetest gives the transports' tests and the integration
// suite a real service plane over an in-memory store: a stub launcher that
// records what it ran, one stub grant per grant kind, and an Ed25519 key the
// plane's keyring trusts.
package serviceplanetest

import (
	"context"
	"crypto/ed25519"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// KeyID names the harness key in the plane's keyring.
const KeyID = "serviceplanetest"

// Launcher records the services it launched and halted. Fail makes the next
// launches fail with the error it holds.
type Launcher struct {
	mu       sync.Mutex
	fail     error
	launched []string
	halted   []string
}

// Fail makes every launch fail with err until Fail(nil).
func (l *Launcher) Fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fail = err
}

// Launch records the service as launched, or fails.
func (l *Launcher) Launch(_ context.Context, svc *serviceplane.Service) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return l.fail
	}
	l.launched = append(l.launched, svc.Tenant+"/"+svc.Name)
	return nil
}

// Halt records the service as halted.
func (l *Launcher) Halt(_ context.Context, svc *serviceplane.Service) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.halted = append(l.halted, svc.Tenant+"/"+svc.Name)
	return nil
}

// Launched answers the tenant/name of every launch, in order.
func (l *Launcher) Launched() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.launched...)
}

// Grant is a stub grant of one kind. Its reference is kind:tenant/name.
type Grant struct{ GrantKind serviceplane.GrantKind }

// Kind answers the grant's kind.
func (g Grant) Kind() serviceplane.GrantKind { return g.GrantKind }

// Grant answers kind:tenant/name.
func (g Grant) Grant(_ context.Context, m *serviceplane.Manifest, _ string) (string, error) {
	return string(g.GrantKind) + ":" + m.Tenant + "/" + m.Name, nil
}

// Revoke revokes nothing.
func (g Grant) Revoke(context.Context, *serviceplane.Manifest, string) error { return nil }

// Grants answers one stub grant per grant kind.
func Grants() []serviceplane.Grant {
	out := make([]serviceplane.Grant, 0, len(serviceplane.GrantKinds))
	for _, k := range serviceplane.GrantKinds {
		out = append(out, Grant{GrantKind: k})
	}
	return out
}

// Runtime is what a node needs to serve the plane: the stub launcher, the
// stub grants and the keyring that trusts Key.
type Runtime struct {
	Launcher *Launcher
	Grants   []serviceplane.Grant
	Verifier serviceplane.Keyring
	Key      ed25519.PrivateKey
	// Now is the clock of the planes Plane builds; nil is time.Now.
	Now func() time.Time
	// writes is how many more store swaps land before every later one is
	// lost, as a crashed node's would be; negative is no limit.
	writes atomic.Int64
	once   sync.Once
}

// LoseWritesAfter lets n more store swaps land and loses every later one,
// as if the node crashed; a negative n lands every swap again.
func (r *Runtime) LoseWritesAfter(n int64) {
	r.once.Do(func() {})
	r.writes.Store(n)
}

// landWrite reports whether one more swap lands.
func (r *Runtime) landWrite() bool {
	r.once.Do(func() { r.writes.Store(-1) })
	for {
		n := r.writes.Load()
		if n < 0 {
			return true
		}
		if n == 0 {
			return false
		}
		if r.writes.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

// NewRuntime makes a runtime with a fresh key.
func NewRuntime(t testing.TB) *Runtime {
	t.Helper()
	signer, _, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmEd25519)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := signer.(ed25519.PrivateKey)
	if !ok {
		t.Fatalf("requester key is %T, want ed25519.PrivateKey", signer)
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatalf("requester public key is %T, want ed25519.PublicKey", key.Public())
	}
	return &Runtime{
		Launcher: &Launcher{},
		Grants:   Grants(),
		Verifier: serviceplane.Keyring{KeyID: pub},
		Key:      key,
	}
}

// Manifest is a valid manifest for tenant/name.
func Manifest(tenant, name string) *serviceplane.Manifest {
	return &serviceplane.Manifest{
		Tenant: tenant,
		Name:   name,
		Image: serviceplane.Image{
			Reference:  "registry.example.internal/" + name,
			Digest:     "sha256:" + strings.Repeat("ab", 32),
			Provenance: "slsa://registry.example.internal/" + name,
		},
		Policy:    serviceplane.PolicyRefs{Sandbox: "default", Mount: "ro-root", Network: "egress-none"},
		Resources: serviceplane.Resources{MilliCPU: 250, MemoryBytes: 64 << 20},
		Role:      "operator",
	}
}

// Sign signs Manifest(tenant, name) with the runtime's key.
func (r *Runtime) Sign(t testing.TB, tenant, name string) *serviceplane.SignedManifest {
	t.Helper()
	sm, err := serviceplane.Sign(Manifest(tenant, name), KeyID, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	return sm
}

// guard arbitrates writes over an in-memory store, as cas.Writer does over
// go-qrdb.
type guard struct {
	mu   sync.Mutex
	kv   *kvstore.Store
	land func() bool
}

func (g *guard) Create(ctx context.Context, key string, value []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok, err := g.kv.Exists(ctx, key); err != nil || ok {
		return false, err
	}
	return true, g.kv.Put(ctx, key, value)
}

func (g *guard) Swap(ctx context.Context, key string, held, next []byte) (bool, error) {
	if g.land != nil && !g.land() {
		return false, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.kv.CompareAndSwap(ctx, key, held, next)
}

// Plane builds a plane over a fresh in-memory store and the runtime.
func (r *Runtime) Plane(t testing.TB) *serviceplane.Plane {
	t.Helper()
	kv := kvstore.NewMemory()
	store, err := serviceplane.NewStore(&guard{kv: kv, land: r.landWrite}, kv)
	if err != nil {
		t.Fatal(err)
	}
	plane, err := serviceplane.NewPlane(serviceplane.Config{
		Store: store, Verifier: r.Verifier, Launcher: r.Launcher, Grants: r.Grants,
		Now: r.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plane
}

// Resolver answers a resolver over a fresh plane, assignable to
// service.ServicePlaneResolver.
func (r *Runtime) Resolver(t testing.TB) func(context.Context) (*serviceplane.Plane, error) {
	plane := r.Plane(t)
	return func(context.Context) (*serviceplane.Plane, error) { return plane, nil }
}
