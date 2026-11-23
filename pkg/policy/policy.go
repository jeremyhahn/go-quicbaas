// Package policy is go-quicbaas's one policy engine. A policy is signed data:
// a Document names its kind, tenant and spec, is signed through go-xkms (the
// Signer and Verifier seam), stored in go-qrdb, and verified on load. The
// Engine dispatches a loaded document to the one Evaluator registered for its
// kind. IAM authorization is not a kind: it stays go-qrdb's PDP, and the
// engine refuses a document that claims it (SPEC row service-policy).
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Kind names what a policy document decides. Each kind has one evaluator.
type Kind string

// The kinds the Go-coded policies move into.
const (
	KindAdmission     Kind = "admission"
	KindSandbox       Kind = "sandbox"
	KindMount         Kind = "mount"
	KindNetwork       Kind = "sandbox-network"
	KindTrust         Kind = "trust"
	KindCommunication Kind = "communication"
	KindRole          Kind = "role"
	KindLifecycle     Kind = "lifecycle"
	KindSpend         Kind = "spend"
)

// maxDocumentNameLen bounds a document name (a DNS name's length).
const maxDocumentNameLen = 253

// iamKinds are the names an IAM decision would go by. go-qrdb's PDP owns IAM
// authorization; a second engine for the same decision is refused.
var iamKinds = map[Kind]struct{}{
	"iam":           {},
	"authorization": {},
	"authz":         {},
	"access":        {},
}

// Errors the engine returns. Each is wrapped with the document's identity.
var (
	ErrUnsigned       = errors.New("policy: document is not signed")
	ErrBadSignature   = errors.New("policy: signature does not verify")
	ErrWrongTenant    = errors.New("policy: document belongs to another tenant")
	ErrUnknownKind    = errors.New("policy: no evaluator for kind")
	ErrIAMKind        = errors.New("policy: IAM authorization is go-qrdb's, not a policy kind")
	ErrInvalidSpec    = errors.New("policy: invalid spec")
	ErrInvalidDoc     = errors.New("policy: invalid document")
	ErrDuplicateKind  = errors.New("policy: kind already registered")
	ErrNotLoaded      = errors.New("policy: document was not loaded through the engine")
	ErrNoVerifier     = errors.New("policy: engine has no verifier")
	ErrNoSigner       = errors.New("policy: no signer")
	ErrNilEvaluator   = errors.New("policy: nil evaluator")
	ErrEmptyKind      = errors.New("policy: evaluator has no kind")
	ErrTenantRequired = errors.New("policy: tenant required")
)

// Document is a policy as data.
type Document struct {
	Kind    Kind            `json:"kind"`
	Name    string          `json:"name"`
	Tenant  string          `json:"tenant"`
	Version uint64          `json:"version"`
	Spec    json.RawMessage `json:"spec"`
}

// Payload is the byte string that is signed and verified: the document as
// JSON with its spec compacted, so whitespace in the spec does not change it.
func (d Document) Payload() ([]byte, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}
	var spec bytes.Buffer
	if err := json.Compact(&spec, d.Spec); err != nil {
		return nil, fmt.Errorf("%w: %s/%s: %w", ErrInvalidSpec, d.Kind, d.Name, err)
	}
	d.Spec = spec.Bytes()
	return json.Marshal(d)
}

func (d Document) validate() error {
	switch {
	case d.Kind == "":
		return fmt.Errorf("%w: kind required", ErrInvalidDoc)
	case d.Name == "" || len(d.Name) > maxDocumentNameLen || strings.TrimSpace(d.Name) != d.Name:
		return fmt.Errorf("%w: name %q", ErrInvalidDoc, d.Name)
	case d.Tenant == "":
		return fmt.Errorf("%w: %s/%s", ErrTenantRequired, d.Kind, d.Name)
	case len(d.Spec) == 0:
		return fmt.Errorf("%w: %s/%s: spec required", ErrInvalidSpec, d.Kind, d.Name)
	}
	return nil
}

// SignedDocument is a Document with the signature over its Payload and the
// id of the tenant key that made it.
type SignedDocument struct {
	Document  Document `json:"document"`
	KeyID     string   `json:"key_id"`
	Signature []byte   `json:"signature"`
}

// Signer signs a payload with the tenant's key. go-xkms implements it.
type Signer interface {
	Sign(ctx context.Context, tenant string, payload []byte) (keyID string, signature []byte, err error)
}

// Verifier checks a signature made with the tenant's key. go-xkms implements
// it. A nil error means the signature verifies.
type Verifier interface {
	Verify(ctx context.Context, tenant, keyID string, payload, signature []byte) error
}

// Sign signs doc through s.
func Sign(ctx context.Context, s Signer, doc Document) (SignedDocument, error) {
	if s == nil {
		return SignedDocument{}, ErrNoSigner
	}
	payload, err := doc.Payload()
	if err != nil {
		return SignedDocument{}, err
	}
	keyID, sig, err := s.Sign(ctx, doc.Tenant, payload)
	if err != nil {
		return SignedDocument{}, fmt.Errorf("policy: sign %s/%s: %w", doc.Kind, doc.Name, err)
	}
	return SignedDocument{Document: doc, KeyID: keyID, Signature: sig}, nil
}

// Decision is an evaluator's answer.
type Decision struct {
	Allow  bool
	Reason string
	Kind   Kind
	Policy string
}

// Evaluator decides one kind. Validate checks a spec when a document is
// loaded; Evaluate decides an input against a spec that Validate accepted.
type Evaluator interface {
	Kind() Kind
	Validate(spec json.RawMessage) error
	Evaluate(ctx context.Context, spec json.RawMessage, input any) (Decision, error)
}

// Loaded is a document the engine verified and validated. Only Load makes one.
type Loaded struct {
	doc    Document
	engine *Engine
}

// Document returns the loaded document.
// Document returns a copy of the loaded document; changing it does not
// change the policy the engine evaluates.
func (l *Loaded) Document() Document {
	d := l.doc
	d.Spec = append(json.RawMessage(nil), l.doc.Spec...)
	return d
}

// Engine dispatches documents to evaluators by kind.
type Engine struct {
	verifier Verifier
	mu       sync.RWMutex
	byKind   map[Kind]Evaluator
}

// NewEngine returns an engine that verifies documents through v, with evals
// registered.
func NewEngine(v Verifier, evals ...Evaluator) (*Engine, error) {
	e := &Engine{verifier: v, byKind: make(map[Kind]Evaluator)}
	for _, ev := range evals {
		if err := e.Register(ev); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Register adds the evaluator for its kind. A kind has one evaluator, and an
// IAM kind has none.
func (e *Engine) Register(ev Evaluator) error {
	if ev == nil {
		return ErrNilEvaluator
	}
	k := ev.Kind()
	if k == "" {
		return ErrEmptyKind
	}
	if isIAMKind(k) {
		return fmt.Errorf("%w: %q", ErrIAMKind, k)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.byKind[k]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateKind, k)
	}
	e.byKind[k] = ev
	return nil
}

// Has reports whether an evaluator is registered for k.
func (e *Engine) Has(k Kind) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.byKind[k]
	return ok
}

func (e *Engine) evaluator(k Kind) (Evaluator, error) {
	if isIAMKind(k) {
		return nil, fmt.Errorf("%w: %q", ErrIAMKind, k)
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	ev, ok := e.byKind[k]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, k)
	}
	return ev, nil
}

// Load verifies sd for tenant and validates its spec. Nothing in a document
// is used before both pass: an unsigned, tampered, other tenant's,
// unknown-kind, IAM-kind or invalid-spec document is refused.
func (e *Engine) Load(ctx context.Context, tenant string, sd SignedDocument) (*Loaded, error) {
	if tenant == "" {
		return nil, ErrTenantRequired
	}
	if e.verifier == nil {
		return nil, ErrNoVerifier
	}
	doc := sd.Document
	// The spec is copied before it is verified: the Loaded value must own the
	// bytes the signature covers, or a caller could change them afterwards.
	doc.Spec = append(json.RawMessage(nil), sd.Document.Spec...)
	if doc.Tenant != tenant {
		return nil, fmt.Errorf("%w: %s/%s is %q's, not %q", ErrWrongTenant, doc.Kind, doc.Name, doc.Tenant, tenant)
	}
	if len(sd.Signature) == 0 || sd.KeyID == "" {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnsigned, doc.Kind, doc.Name)
	}
	payload, err := doc.Payload()
	if err != nil {
		return nil, err
	}
	if err := e.verifier.Verify(ctx, tenant, sd.KeyID, payload, sd.Signature); err != nil {
		return nil, fmt.Errorf("%w: %s/%s: %w", ErrBadSignature, doc.Kind, doc.Name, err)
	}
	ev, err := e.evaluator(doc.Kind)
	if err != nil {
		return nil, err
	}
	if err := ev.Validate(doc.Spec); err != nil {
		return nil, fmt.Errorf("%w: %s/%s: %w", ErrInvalidSpec, doc.Kind, doc.Name, err)
	}
	return &Loaded{doc: doc, engine: e}, nil
}

// Evaluate decides input against a loaded document, dispatching on its kind.
func (e *Engine) Evaluate(ctx context.Context, l *Loaded, input any) (Decision, error) {
	if l == nil || l.engine != e {
		return Decision{}, ErrNotLoaded
	}
	ev, err := e.evaluator(l.doc.Kind)
	if err != nil {
		return Decision{}, err
	}
	d, err := ev.Evaluate(ctx, l.doc.Spec, input)
	if err != nil {
		return Decision{}, fmt.Errorf("policy: evaluate %s/%s: %w", l.doc.Kind, l.doc.Name, err)
	}
	d.Kind = l.doc.Kind
	d.Policy = l.doc.Name
	return d, nil
}

func isIAMKind(k Kind) bool {
	_, ok := iamKinds[Kind(strings.ToLower(string(k)))]
	return ok
}
