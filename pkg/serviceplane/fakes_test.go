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
	// loseAfter, when positive, makes every Swap lose once that many
	// Swaps have succeeded; swaps counts the successful ones.
	loseAfter int
	// loseOnce, when positive, makes the one Swap that follows that many
	// successful ones lose; later Swaps succeed.
	loseOnce int
	swaps    int
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
	// A write on a cancelled request fails, as it does over the network.
	if err := ctx.Err(); err != nil {
		return false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.loseSwaps || (g.loseAfter > 0 && g.swaps >= g.loseAfter) {
		return false, nil
	}
	if g.loseOnce > 0 && g.swaps >= g.loseOnce {
		g.loseOnce = 0
		return false, nil
	}
	ok, err := g.kv.CompareAndSwap(ctx, key, held, next)
	if ok && err == nil {
		g.swaps++
	}
	return ok, err
}

// setLoseAfter makes Swaps lose once n more have succeeded; 0 turns it off.
func (g *memGuard) setLoseAfter(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.swaps, g.loseAfter = 0, n
}

// setLoseOnce makes the Swap after the next n successful ones lose.
func (g *memGuard) setLoseOnce(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.swaps, g.loseOnce = 0, n
}

type launcher struct {
	mu        sync.Mutex
	calls     []string
	launchErr error
	haltErr   error
	// onLaunch, when set, runs in Launch and its error is Launch's.
	onLaunch func(ctx context.Context) error
	// onHalt, when set, runs after a Halt that succeeds.
	onHalt func()
}

func (l *launcher) Launch(ctx context.Context, svc *Service) error {
	l.mu.Lock()
	l.calls = append(l.calls, "launch "+svc.Name)
	err, hook := l.launchErr, l.onLaunch
	l.mu.Unlock()
	if hook != nil {
		return hook(ctx)
	}
	return err
}

func (g *memGuard) setLoseSwaps(lose bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loseSwaps = lose
}

func (l *launcher) Halt(_ context.Context, svc *Service) error {
	l.mu.Lock()
	l.calls = append(l.calls, "halt "+svc.Name)
	err, hook := l.haltErr, l.onHalt
	l.mu.Unlock()
	if err == nil && hook != nil {
		hook()
	}
	return err
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

// reset clears the ledger.
func (l *ledger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = nil
}

type grant struct {
	kind    GrantKind
	ledger  *ledger
	fail    error
	revoked error
	// failRole, when set, fails a Grant for a manifest with that role.
	failRole string
}

func (g *grant) Kind() GrantKind { return g.kind }

func (g *grant) Grant(_ context.Context, m *Manifest, prior string) (string, error) {
	if g.fail != nil {
		return "", g.fail
	}
	if g.failRole != "" && m.Role == g.failRole {
		return "", errors.New("grant refused for role " + m.Role)
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
	clockMu  sync.Mutex
	clock    time.Time
}

// advance moves the plane's clock on by d.
func (f *fixture) advance(d time.Duration) {
	f.clockMu.Lock()
	defer f.clockMu.Unlock()
	f.clock = f.clock.Add(d)
}

func (f *fixture) now() time.Time {
	f.clockMu.Lock()
	defer f.clockMu.Unlock()
	return f.clock
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
		clock:    time.Unix(1_800_000_000, 0).UTC(),
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
		Now: f.now,
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
	svc, err := f.plane.Deploy(context.Background(), "acme", f.sign(t, m))
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
