package xkmssign

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/policy"
	"github.com/jeremyhahn/go-quicbaas/pkg/policy/sandbox"
)

// fakeOps is an in-memory KeyOps: one P-256 key per key ID.
type fakeOps struct {
	keys      map[string]*ecdsa.PrivateKey
	verifyErr error
}

func newFakeOps(t *testing.T, keyIDs ...string) *fakeOps {
	t.Helper()
	f := &fakeOps{keys: map[string]*ecdsa.PrivateKey{}}
	for _, id := range keyIDs {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		f.keys[id] = k
	}
	return f
}

func (f *fakeOps) Sign(_ context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error) {
	if hashAlgorithm != HashAlgorithm || preHashed {
		return nil, errors.New("fake: unexpected hash mode")
	}
	k, ok := f.keys[keyID]
	if !ok {
		return nil, errors.New("fake: no key " + keyID)
	}
	d := sha256.Sum256(input)
	return ecdsa.SignASN1(rand.Reader, k, d[:])
}

func (f *fakeOps) Verify(_ context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error) {
	if f.verifyErr != nil {
		return false, f.verifyErr
	}
	if hashAlgorithm != HashAlgorithm {
		return false, errors.New("fake: unexpected hash")
	}
	k, ok := f.keys[keyID]
	if !ok {
		return false, errors.New("fake: no key " + keyID)
	}
	d := sha256.Sum256(input)
	return ecdsa.VerifyASN1(&k.PublicKey, d[:], signature), nil
}

func sandboxDoc(tenant string) policy.Document {
	return policy.Document{
		Kind: policy.KindSandbox, Name: "sandbox", Tenant: tenant, Version: 1,
		Spec: json.RawMessage(`{"policy":"opt-in"}`),
	}
}

func newAdapter(t *testing.T, ops KeyOps) *Adapter {
	t.Helper()
	a, err := New(ops, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func newEngine(t *testing.T, v policy.Verifier) *policy.Engine {
	t.Helper()
	eng, err := policy.NewEngine(v, sandbox.Evaluator{})
	if err != nil {
		t.Fatal(err)
	}
	return eng
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	a := newAdapter(t, newFakeOps(t, DefaultKeyFor("acme")))
	sd, err := policy.Sign(ctx, a, sandboxDoc("acme"))
	if err != nil {
		t.Fatal(err)
	}
	if sd.KeyID != "policy-signing-acme" {
		t.Fatalf("keyID = %q", sd.KeyID)
	}
	eng := newEngine(t, a)
	l, err := eng.Load(ctx, "acme", sd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if l.Document().Tenant != "acme" {
		t.Fatalf("loaded tenant %q", l.Document().Tenant)
	}
}

func TestTamperedPayloadRefused(t *testing.T) {
	ctx := context.Background()
	a := newAdapter(t, newFakeOps(t, DefaultKeyFor("acme")))
	sd, err := policy.Sign(ctx, a, sandboxDoc("acme"))
	if err != nil {
		t.Fatal(err)
	}
	sd.Document.Spec = json.RawMessage(`{"policy":"disabled"}`)
	_, err = newEngine(t, a).Load(ctx, "acme", sd)
	if !errors.Is(err, policy.ErrBadSignature) {
		t.Fatalf("err = %v, want ErrBadSignature", err)
	}
	// Directly, too.
	payload, _ := sd.Document.Payload()
	if err := a.Verify(ctx, "acme", sd.KeyID, payload, sd.Signature); !errors.Is(err, policy.ErrBadSignature) {
		t.Fatalf("verify err = %v, want ErrBadSignature", err)
	}
}

func TestOtherTenantsKeyRefused(t *testing.T) {
	ctx := context.Background()
	a := newAdapter(t, newFakeOps(t, DefaultKeyFor("acme"), DefaultKeyFor("beta")))
	sdB, err := policy.Sign(ctx, a, sandboxDoc("beta"))
	if err != nil {
		t.Fatal(err)
	}
	// Tenant A's document carrying tenant B's key and signature.
	doc := sandboxDoc("acme")
	payload, _ := doc.Payload()
	keyID, sigB, err := a.Sign(ctx, "beta", payload)
	if err != nil {
		t.Fatal(err)
	}
	if keyID != sdB.KeyID {
		t.Fatalf("keyID %q != %q", keyID, sdB.KeyID)
	}
	if err := a.Verify(ctx, "acme", keyID, payload, sigB); !errors.Is(err, policy.ErrWrongTenant) {
		t.Fatalf("verify err = %v, want ErrWrongTenant", err)
	}
	_, err = newEngine(t, a).Load(ctx, "acme",
		policy.SignedDocument{Document: doc, KeyID: keyID, Signature: sigB})
	if !errors.Is(err, policy.ErrWrongTenant) {
		t.Fatalf("load err = %v, want ErrWrongTenant", err)
	}
}

func TestKeyOpsErrorWrapped(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("backend down")
	ops := newFakeOps(t, DefaultKeyFor("acme"))
	ops.verifyErr = sentinel
	a := newAdapter(t, ops)
	err := a.Verify(ctx, "acme", DefaultKeyFor("acme"), []byte("x"), []byte("y"))
	if !errors.Is(err, sentinel) || errors.Is(err, policy.ErrBadSignature) {
		t.Fatalf("err = %v, want the KeyOps error only", err)
	}
	// Sign with a key that does not exist surfaces the KeyOps error.
	if _, _, err := a.Sign(ctx, "nokey", []byte("x")); err == nil {
		t.Fatal("sign with a missing key succeeded")
	}
}

func TestNilAndEmptyArguments(t *testing.T) {
	ctx := context.Background()
	if _, err := New(nil, nil); !errors.Is(err, ErrNilKeyOps) {
		t.Fatalf("New(nil) err = %v", err)
	}
	custom, err := New(newFakeOps(t, "k-acme"), func(tn string) string { return "k-" + tn })
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := custom.Sign(ctx, "acme", []byte("x")); err != nil || id != "k-acme" {
		t.Fatalf("custom keyFor: id=%q err=%v", id, err)
	}
	a := newAdapter(t, newFakeOps(t, DefaultKeyFor("acme")))
	if _, _, err := a.Sign(ctx, "", []byte("x")); !errors.Is(err, policy.ErrTenantRequired) {
		t.Fatalf("Sign empty tenant err = %v", err)
	}
	if err := a.Verify(ctx, "", DefaultKeyFor("acme"), []byte("x"), []byte("y")); !errors.Is(err, policy.ErrTenantRequired) {
		t.Fatalf("Verify empty tenant err = %v", err)
	}
	if err := a.Verify(ctx, "acme", "", []byte("x"), []byte("y")); !errors.Is(err, policy.ErrWrongTenant) {
		t.Fatalf("Verify empty keyID err = %v", err)
	}
}
