package serviceplane

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// memGuard arbitrates over an in-memory go-qrdb store the way cas.Writer
// does over a node: Create writes an absent key only, Swap the held value only.
type memGuard struct {
	mu sync.Mutex
	kv *kvstore.Store
	// loseSwaps makes every Swap lose, as a racing node's write would.
	loseSwaps bool
}

func (g *memGuard) Create(ctx context.Context, key string, value []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok, err := g.kv.Exists(ctx, key); err != nil || ok {
		return false, err
	}
	return true, g.kv.Put(ctx, key, value)
}

func (g *memGuard) Swap(ctx context.Context, key string, held, next []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.loseSwaps {
		return false, nil
	}
	return g.kv.CompareAndSwap(ctx, key, held, next)
}

type launcher struct {
	mu        sync.Mutex
	calls     []string
	launchErr error
	haltErr   error
}

func (l *launcher) Launch(_ context.Context, svc *Service) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, "launch "+svc.Name)
	return l.launchErr
}

func (l *launcher) Halt(_ context.Context, svc *Service) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, "halt "+svc.Name)
	return l.haltErr
}

// ledger records every grant and revoke of every kind, in order.
type ledger struct {
	mu     sync.Mutex
	events []string
}

func (l *ledger) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *ledger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.events, ",")
}

type grant struct {
	kind    GrantKind
	ledger  *ledger
	fail    error
	revoked error
}

func (g *grant) Kind() GrantKind { return g.kind }

func (g *grant) Grant(_ context.Context, m *Manifest, prior string) (string, error) {
	if g.fail != nil {
		return "", g.fail
	}
	ref := string(g.kind) + ":" + m.Name + ":" + m.Role
	g.ledger.add("grant " + string(g.kind) + " prior=" + prior)
	return ref, nil
}

func (g *grant) Revoke(_ context.Context, _ *Manifest, ref string) error {
	g.ledger.add("revoke " + ref)
	return g.revoked
}

type fixture struct {
	plane    *Plane
	guard    *memGuard
	launcher *launcher
	ledger   *ledger
	grants   map[GrantKind]*grant
	key      ed25519.PrivateKey
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		guard:    &memGuard{kv: kvstore.NewMemory()},
		launcher: &launcher{},
		ledger:   &ledger{},
		grants:   map[GrantKind]*grant{},
		key:      key,
	}
	store, err := NewStore(f.guard, f.guard.kv)
	if err != nil {
		t.Fatal(err)
	}
	var gs []Grant
	for _, k := range GrantKinds {
		f.grants[k] = &grant{kind: k, ledger: f.ledger}
		gs = append(gs, f.grants[k])
	}
	f.plane, err = NewPlane(Config{
		Store: store, Verifier: Keyring{"k1": pub}, Launcher: f.launcher, Grants: gs,
		Now: func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) sign(t *testing.T, m *Manifest) *SignedManifest {
	t.Helper()
	sm, err := Sign(m, "k1", f.key)
	if err != nil {
		t.Fatal(err)
	}
	return sm
}

func (f *fixture) deploy(t *testing.T, name string) *Service {
	t.Helper()
	m := validManifest()
	m.Name = name
	svc, err := f.plane.Deploy(context.Background(), f.sign(t, m))
	if err != nil {
		t.Fatalf("deploy %s: %v", name, err)
	}
	return svc
}

func validManifest() *Manifest {
	return &Manifest{
		Tenant: "acme",
		Name:   "web",
		Image: Image{
			Reference:  "registry.acme.internal/web",
			Digest:     "sha256:" + strings.Repeat("ab", 32),
			Provenance: "slsa://registry.acme.internal/web/provenance",
		},
		Policy:    PolicyRefs{Sandbox: "default", Mount: "ro-root", Network: "egress-none"},
		Resources: Resources{MilliCPU: 500, MemoryBytes: 256 << 20},
		Volumes:   []Volume{{Name: "data", MountPath: "/data", SizeBytes: 1 << 30}},
		Ports:     []Port{{Name: "https", Port: 8443, Protocol: "tcp"}},
		Role:      "operator",
		Placement: Placement{NodeSelector: map[string]string{"zone": "a"}},
	}
}

func wantErr(t *testing.T, err, sentinel error) {
	t.Helper()
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}
