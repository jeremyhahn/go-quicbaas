package serviceplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// mapKV is a go-qrdb KV client over a map. Scan answers in map order, which
// Go randomises, so the reader's ordering is what a test observes.
type mapKV map[string][]byte

func (m mapKV) Put(_ context.Context, key string, value []byte, _ ...transport.WriteOption) error {
	m[key] = value
	return nil
}

func (m mapKV) Get(_ context.Context, key string, _ ...transport.ReadOption) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, &transport.KeyNotFoundError{Key: key}
	}
	return v, nil
}

func (m mapKV) Delete(_ context.Context, key string, _ ...transport.WriteOption) error {
	delete(m, key)
	return nil
}

func (m mapKV) List(_ context.Context, prefix string, _ ...transport.ReadOption) ([]string, error) {
	return nil, errors.New("unused")
}

func (m mapKV) Scan(_ context.Context, prefix string, _ ...transport.ReadOption) (map[string][]byte, error) {
	out := map[string][]byte{}
	for k, v := range m {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, nil
}

func (m mapKV) Exists(_ context.Context, key string, _ ...transport.ReadOption) (bool, error) {
	_, ok := m[key]
	return ok, nil
}

// mapGuard arbitrates over the same map, so the store reads what it wrote.
type mapGuard struct{ kv mapKV }

func (g mapGuard) Create(_ context.Context, key string, value []byte) (bool, error) {
	if _, ok := g.kv[key]; ok {
		return false, nil
	}
	g.kv[key] = value
	return true, nil
}

func (g mapGuard) Swap(_ context.Context, key string, held, next []byte) (bool, error) {
	if string(g.kv[key]) != string(held) {
		return false, nil
	}
	g.kv[key] = next
	return true, nil
}

func TestQRDBReader_NotFoundIsAbsentAndScanIsOrdered(t *testing.T) {
	kv := mapKV{}
	if QRDBReader(nil) != nil {
		t.Fatal("a nil client made a reader")
	}
	store, err := NewStore(mapGuard{kv}, QRDBReader(kv))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "acme", "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing key = %v, want ErrNotFound", err)
	}
	f := newFixture(t)
	for _, name := range []string{"zeta", "alpha", "mid", "beta", "omega"} {
		svc := f.deploy(t, name)
		raw, _ := f.guard.kv.Get(ctx, serviceKey("acme", name))
		kv[serviceKey("acme", name)] = raw
		_ = svc
	}
	got, err := store.List(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "alpha,beta,mid,omega,zeta" {
		t.Fatalf("list order = %v", names)
	}
}
