package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// hmacKeys stands in for go-xkms in these unit tests: one key per tenant.
type hmacKeys map[string][]byte

func (k hmacKeys) Sign(_ context.Context, tenant string, payload []byte) (string, []byte, error) {
	key, ok := k[tenant]
	if !ok {
		return "", nil, fmt.Errorf("no key for %q", tenant)
	}
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return tenant + "/policy", m.Sum(nil), nil
}

func (k hmacKeys) Verify(ctx context.Context, tenant, keyID string, payload, sig []byte) error {
	if keyID != tenant+"/policy" {
		return fmt.Errorf("key %q is not %q's", keyID, tenant)
	}
	_, want, err := k.Sign(ctx, tenant, payload)
	if err != nil {
		return err
	}
	if !hmac.Equal(want, sig) {
		return errors.New("mismatch")
	}
	return nil
}

// limitEval is a test kind: allow while input <= spec.max.
type limitEval struct{ kind Kind }

type limitSpec struct {
	Max *int `json:"max"`
}

func (l limitEval) Kind() Kind { return l.kind }

func (limitEval) Validate(raw json.RawMessage) error {
	var s limitSpec
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s.Max == nil {
		return errors.New("max required")
	}
	return nil
}

func (limitEval) Evaluate(_ context.Context, raw json.RawMessage, input any) (Decision, error) {
	var s limitSpec
	if err := json.Unmarshal(raw, &s); err != nil {
		return Decision{}, err
	}
	n, ok := input.(int)
	if !ok {
		return Decision{}, fmt.Errorf("input %T, want int", input)
	}
	if n > *s.Max {
		return Decision{Reason: fmt.Sprintf("%d over %d", n, *s.Max)}, nil
	}
	return Decision{Allow: true}, nil
}

var keys = hmacKeys{"acme": []byte("acme-key"), "other": []byte("other-key")}

func doc(kind Kind, spec string) Document {
	return Document{Kind: kind, Name: "cap", Tenant: "acme", Version: 1, Spec: json.RawMessage(spec)}
}

func mustSign(t *testing.T, d Document) SignedDocument {
	t.Helper()
	sd, err := Sign(context.Background(), keys, d)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return sd
}

func newEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine(keys, limitEval{KindSpend}, limitEval{KindMount})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func TestEngineDispatchesByKind(t *testing.T) {
	ctx := context.Background()
	e := newEngine(t)
	spend, err := e.Load(ctx, "acme", mustSign(t, doc(KindSpend, `{"max": 10}`)))
	if err != nil {
		t.Fatalf("Load spend: %v", err)
	}
	mount, err := e.Load(ctx, "acme", mustSign(t, doc(KindMount, `{"max": 2}`)))
	if err != nil {
		t.Fatalf("Load mount: %v", err)
	}
	for _, tc := range []struct {
		l     *Loaded
		in    int
		allow bool
		kind  Kind
	}{
		{spend, 5, true, KindSpend},
		{spend, 11, false, KindSpend},
		{mount, 2, true, KindMount},
		{mount, 3, false, KindMount},
	} {
		d, err := e.Evaluate(ctx, tc.l, tc.in)
		if err != nil {
			t.Fatalf("Evaluate %s(%d): %v", tc.kind, tc.in, err)
		}
		if d.Allow != tc.allow || d.Kind != tc.kind || d.Policy != "cap" {
			t.Errorf("Evaluate %s(%d) = %+v, want allow=%v", tc.kind, tc.in, d, tc.allow)
		}
	}
}

func TestSpecWhitespaceDoesNotBreakSignature(t *testing.T) {
	sd := mustSign(t, doc(KindSpend, `{"max": 10}`))
	sd.Document.Spec = json.RawMessage("{\n  \"max\":10\n}")
	if _, err := newEngine(t).Load(context.Background(), "acme", sd); err != nil {
		t.Fatalf("Load reformatted spec: %v", err)
	}
}

func TestLoadRefuses(t *testing.T) {
	ctx := context.Background()
	good := mustSign(t, doc(KindSpend, `{"max": 10}`))
	cases := map[string]struct {
		tenant string
		sd     func() SignedDocument
		want   error
	}{
		"unsigned": {"acme", func() SignedDocument {
			s := good
			s.Signature = nil
			return s
		}, ErrUnsigned},
		"no key id": {"acme", func() SignedDocument {
			s := good
			s.KeyID = ""
			return s
		}, ErrUnsigned},
		"tampered spec": {"acme", func() SignedDocument {
			s := good
			s.Document.Spec = json.RawMessage(`{"max": 1000}`)
			return s
		}, ErrBadSignature},
		"tampered kind": {"acme", func() SignedDocument {
			s := good
			s.Document.Kind = KindMount
			return s
		}, ErrBadSignature},
		"other tenant's key": {"acme", func() SignedDocument {
			d := doc(KindSpend, `{"max": 10}`)
			d.Tenant = "other"
			s := mustSign(t, d)
			s.Document.Tenant = "acme"
			return s
		}, ErrBadSignature},
		"wrong tenant": {"other", func() SignedDocument { return good }, ErrWrongTenant},
		"unknown kind": {"acme", func() SignedDocument {
			return mustSign(t, doc(KindLifecycle, `{"max": 1}`))
		}, ErrUnknownKind},
		"iam kind": {"acme", func() SignedDocument {
			return mustSign(t, doc("IAM", `{"max": 1}`))
		}, ErrIAMKind},
		"invalid spec": {"acme", func() SignedDocument {
			return mustSign(t, doc(KindSpend, `{"min": 1}`))
		}, ErrInvalidSpec},
		"no tenant": {"", func() SignedDocument { return good }, ErrTenantRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			l, err := newEngine(t).Load(ctx, tc.tenant, tc.sd())
			if !errors.Is(err, tc.want) {
				t.Fatalf("Load err = %v, want %v", err, tc.want)
			}
			if l != nil {
				t.Fatal("Load returned a document with an error")
			}
		})
	}
}

func TestLoadWithoutVerifierRefuses(t *testing.T) {
	e, err := NewEngine(nil, limitEval{KindSpend})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Load(context.Background(), "acme", mustSign(t, doc(KindSpend, `{"max": 1}`)))
	if !errors.Is(err, ErrNoVerifier) {
		t.Fatalf("err = %v, want ErrNoVerifier", err)
	}
}

func TestRegisterRefuses(t *testing.T) {
	e := newEngine(t)
	for name, tc := range map[string]struct {
		ev   Evaluator
		want error
	}{
		"duplicate":     {limitEval{KindSpend}, ErrDuplicateKind},
		"iam":           {limitEval{"authorization"}, ErrIAMKind},
		"iam any case":  {limitEval{"AuthZ"}, ErrIAMKind},
		"empty kind":    {limitEval{""}, ErrEmptyKind},
		"nil evaluator": {nil, ErrNilEvaluator},
	} {
		if err := e.Register(tc.ev); !errors.Is(err, tc.want) {
			t.Errorf("%s: Register err = %v, want %v", name, err, tc.want)
		}
	}
	if _, err := NewEngine(keys, limitEval{KindSpend}, limitEval{KindSpend}); !errors.Is(err, ErrDuplicateKind) {
		t.Errorf("NewEngine duplicate err = %v, want ErrDuplicateKind", err)
	}
	if !e.Has(KindSpend) || e.Has(KindLifecycle) {
		t.Error("Has does not match the registered kinds")
	}
}

func TestEvaluateNeedsALoadedDocument(t *testing.T) {
	ctx := context.Background()
	e := newEngine(t)
	if _, err := e.Evaluate(ctx, &Loaded{doc: doc(KindSpend, `{"max": 1}`)}, 0); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("forged Loaded err = %v, want ErrNotLoaded", err)
	}
	l, err := newEngine(t).Load(ctx, "acme", mustSign(t, doc(KindSpend, `{"max": 1}`)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Evaluate(ctx, l, 0); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("other engine's Loaded err = %v, want ErrNotLoaded", err)
	}
	if _, err := e.Evaluate(ctx, nil, 0); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("nil Loaded err = %v, want ErrNotLoaded", err)
	}
}

func TestSignRefusesInvalidDocument(t *testing.T) {
	ctx := context.Background()
	for name, d := range map[string]Document{
		"no kind":   {Name: "x", Tenant: "acme", Spec: json.RawMessage(`{}`)},
		"no name":   {Kind: KindSpend, Tenant: "acme", Spec: json.RawMessage(`{}`)},
		"no tenant": {Kind: KindSpend, Name: "x", Spec: json.RawMessage(`{}`)},
		"no spec":   {Kind: KindSpend, Name: "x", Tenant: "acme"},
		"bad json":  {Kind: KindSpend, Name: "x", Tenant: "acme", Spec: json.RawMessage(`{`)},
	} {
		if _, err := Sign(ctx, keys, d); err == nil {
			t.Errorf("%s: Sign accepted an invalid document", name)
		}
	}
	if _, err := Sign(ctx, nil, doc(KindSpend, `{}`)); !errors.Is(err, ErrNoSigner) {
		t.Errorf("nil signer err = %v, want ErrNoSigner", err)
	}
}
