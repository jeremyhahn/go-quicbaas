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

package sdlc

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// =============================================================================
// Forge token custody
// =============================================================================
//
// Token custody is deliberately NOT part of ForgeBackend.
//
// ForgeBackend is the workflow plane: issues, branches, pull requests, reviews,
// checks, webhooks — the things the module does with a repository. Minting and
// revoking credentials is not workflow, and folding it in would make every
// future adapter carry a credential capability to be registrable at all. The
// completeness rule for THIS interface is scoped to this interface: a forge may
// implement it or not, and one that does must implement all of it.

// TokenLifecycleCapabilities reports what the PLATFORM permits around token
// custody. Like ForgeCapabilities, these are facts about the forge, never a
// record of which methods an adapter got around to writing.
type TokenLifecycleCapabilities struct {
	// SelfServiceMint reports whether a token can mint its own successor.
	//
	// Gitea's cannot. Its POST /users/{username}/tokens is reachable with a
	// token only for a user's OWN tokens and only with the user-level write
	// scope that also grants SSH-key management — the scope agent registration
	// deliberately withholds — and reaching another user's tokens requires
	// basic auth outright. So rotation there runs on a password-equivalent
	// credential held by the provisioning plane, not on the token being
	// replaced.
	//
	// A forge where this is true can rotate from the expiring credential
	// itself and needs no custody credential at all.
	SelfServiceMint bool `json:"self_service_mint"`

	// ScopedTokens reports whether minted tokens can carry a scope set.
	//
	// It matters to rotation specifically: the replacement must be able to
	// verify ITSELF before the old one is revoked, which requires at least an
	// identity-read scope. A forge without scoping hands back a token with
	// whatever authority the account has, and the verify step still works.
	ScopedTokens bool `json:"scoped_tokens"`

	// RevokeByName reports whether a token can be revoked by the name it was
	// minted under, rather than only by an id returned at mint time.
	//
	// This is what makes revocation recoverable after a crash between minting
	// and recording: the name is derivable, an id that was never persisted is
	// not.
	RevokeByName bool `json:"revoke_by_name"`
}

// TokenSpec describes a token to mint.
type TokenSpec struct {
	// Username is the account the token belongs to.
	Username string

	// Name identifies the token on the forge. Rotation derives it so that a
	// token orphaned by a crash can still be found and revoked.
	Name string

	// Scopes requested for the token. Ignored by forges whose
	// ScopedTokens capability is false.
	//
	// Rotation always includes an identity-read scope: without it the new
	// token cannot verify itself, and revoking the old one on an unverified
	// replacement is how an account locks itself out.
	Scopes []string
}

// MintedToken is a newly issued token.
//
// The secret is present only here, at the one moment the forge discloses it.
// It belongs in SecretsService before this value goes out of scope.
type MintedToken struct {
	// ID is the forge's handle for the token, where it issues one.
	ID string

	// Name is the name it was minted under.
	Name string

	// Secret is the token itself, held in the STACK-OWNED SECURE BUFFER
	// (go-quicbaas#97 rotation-surface slice, owner-ruled 2026-08-24):
	// mlocked, guard-paged, canary-checked, zero-wiped on Destroy — never a
	// Go string, never a plain heap slice. The adapter constructs it at its
	// decode boundary (NewBufferFromBytes moves AND wipes the extracted
	// bytes) and retains nothing; the ONE owner is the rotation that minted
	// it, which defers Destroy so the enclave dies with the rotation on
	// every path. Bytes() hands out borrowed views INTO the enclave — the
	// no-retain contracts on VerifyToken and the store write govern those.
	// The irreducible transport-decode residue inside the adapter stays
	// documented at that site as #97's remaining floor.
	Secret *secure.Buffer
}

// TokenIdentity is what a forge reports about the bearer of a token.
type TokenIdentity struct {
	// Username the token authenticates as.
	Username string

	// Scopes the token carries, where the forge reports them.
	Scopes []string
}

// ForgeTokenLifecycle mints, verifies and revokes forge access tokens.
//
// Implemented by a forge adapter only when the platform supports the whole of
// it. A nil implementation is a forge whose tokens are managed by hand, which
// is a legitimate configuration and not a defect.
type ForgeTokenLifecycle interface {
	// TokenCapabilities reports what the platform permits.
	TokenCapabilities() TokenLifecycleCapabilities

	// MintToken issues a new token and returns it, including its secret.
	//
	// Implementations authenticate with the custody credential rather than
	// with the token being replaced, unless SelfServiceMint is true.
	MintToken(ctx context.Context, spec TokenSpec) (*MintedToken, error)

	// VerifyToken reports who the given token authenticates as.
	//
	// This is the step that makes rotation safe: a replacement is proven to
	// work BEFORE the credential it replaces is destroyed. It authenticates
	// as the token under test, never as the custody credential — verifying
	// with a different credential would prove nothing about this one.
	//
	// token is a BORROWED byte view with a NO-RETAIN contract: the
	// implementation must not keep token, or any slice aliasing it, beyond
	// the call's return — the caller wipes the backing array immediately
	// after. Transient conversions an implementation cannot avoid (an HTTP
	// Authorization header is a string) are confined to the call and are
	// part of the go-quicbaas#97 secure-buffer custody debt, recorded there.
	VerifyToken(ctx context.Context, token []byte) (*TokenIdentity, error)

	// RevokeToken destroys a token, verified by read-back where the platform
	// allows a token to be listed.
	RevokeToken(ctx context.Context, username, nameOrID string) error
}

// TokenCustodyUnsupportedError reports that a forge adapter does not implement
// token custody at all.
//
// Distinct from a mint failure: this says rotation was asked of a forge that
// never claimed to do it, which is a configuration error rather than a runtime
// one.
type TokenCustodyUnsupportedError struct {
	Type ForgeType
}

// Error implements the error interface.
func (e *TokenCustodyUnsupportedError) Error() string {
	return "sdlc: forge " + string(e.Type) + " does not implement token custody"
}

// TokenVerificationError reports that a freshly minted token could not be
// verified, or verified as the wrong account.
//
// Rotation treats this as terminal for the attempt and MUST NOT revoke the
// credential being replaced: an unverified replacement plus a destroyed
// original is how an agent loses its access entirely.
type TokenVerificationError struct {
	Name     string
	Expected string
	Actual   string
	Cause    error
}

// Error implements the error interface.
func (e *TokenVerificationError) Error() string {
	if e.Cause != nil {
		return "sdlc: token " + e.Name + " failed verification: " + e.Cause.Error()
	}
	return "sdlc: token " + e.Name + " authenticates as " + e.Actual + ", expected " + e.Expected
}

// Unwrap returns the underlying cause.
func (e *TokenVerificationError) Unwrap() error { return e.Cause }
