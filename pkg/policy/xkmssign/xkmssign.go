// Package xkmssign signs and verifies policy documents with the tenant's key
// in go-xkms. It adapts pkg/crypto's MemoryCryptoManager (which runs over
// pkg/kms and go-xkms) to policy.Signer and policy.Verifier.
//
// sdk/go/xkms has no general Sign/Verify (only a PKCS#11 sign server), so the
// tenant-key operations come from pkg/crypto, the same path the service's
// sign and verify verbs use.
package xkmssign

import (
	"context"
	"errors"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/policy"
)

// HashAlgorithm is the digest the adapter asks KeyOps to compute over the
// document payload.
const HashAlgorithm = "SHA256"

// KeyOps is the tenant-key sign and verify surface, exactly the signatures of
// pkg/crypto.MemoryCryptoManager. It is an interface so this package imports
// neither pkg/service nor a transport.
type KeyOps interface {
	Sign(ctx context.Context, keyID string, input []byte, hashAlgorithm string, preHashed bool) ([]byte, error)
	Verify(ctx context.Context, keyID string, input, signature []byte, hashAlgorithm string) (bool, error)
}

// ErrNilKeyOps is returned by New when ops is nil.
var ErrNilKeyOps = errors.New("xkmssign: key operations are required")

// DefaultKeyFor names the tenant's policy-signing key.
func DefaultKeyFor(tenant string) string { return "policy-signing-" + tenant }

// Adapter is a policy.Signer and policy.Verifier over a tenant's key.
type Adapter struct {
	ops    KeyOps
	keyFor func(tenant string) string
}

var (
	_ policy.Signer   = (*Adapter)(nil)
	_ policy.Verifier = (*Adapter)(nil)
)

// New returns an Adapter over ops. keyFor maps a tenant to its signing key
// ID; nil means DefaultKeyFor.
func New(ops KeyOps, keyFor func(tenant string) string) (*Adapter, error) {
	if ops == nil {
		return nil, ErrNilKeyOps
	}
	if keyFor == nil {
		keyFor = DefaultKeyFor
	}
	return &Adapter{ops: ops, keyFor: keyFor}, nil
}

// Sign signs payload with the tenant's key and returns that key's ID.
func (a *Adapter) Sign(ctx context.Context, tenant string, payload []byte) (string, []byte, error) {
	if tenant == "" {
		return "", nil, policy.ErrTenantRequired
	}
	keyID := a.keyFor(tenant)
	sig, err := a.ops.Sign(ctx, keyID, payload, HashAlgorithm, false)
	if err != nil {
		return "", nil, fmt.Errorf("xkmssign: sign with %s: %w", keyID, err)
	}
	return keyID, sig, nil
}

// Verify checks signature over payload with the tenant's key. A keyID other
// than the tenant's own is refused: a document signed with another tenant's
// key never verifies.
func (a *Adapter) Verify(ctx context.Context, tenant, keyID string, payload, signature []byte) error {
	if tenant == "" {
		return policy.ErrTenantRequired
	}
	want := a.keyFor(tenant)
	if keyID != want {
		return fmt.Errorf("%w: key %q is not tenant %q's key", policy.ErrWrongTenant, keyID, tenant)
	}
	ok, err := a.ops.Verify(ctx, keyID, payload, signature, HashAlgorithm)
	if err != nil {
		return fmt.Errorf("xkmssign: verify with %s: %w", keyID, err)
	}
	if !ok {
		return fmt.Errorf("%w: key %s", policy.ErrBadSignature, keyID)
	}
	return nil
}
