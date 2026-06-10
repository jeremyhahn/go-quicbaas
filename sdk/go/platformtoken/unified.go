// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package platformtoken

import (
	"context"
	"errors"
)

// Authorize is the unified authorization entry point every policy-enforcement
// point calls. It verifies the token (including DPoP when the token is
// sender-constrained), enforces the required scope offline, and then applies the
// tenant's authz mode for liveness/revocation:
//
//   - AuthzModeOffline: trust the signed scopes and expiry; no liveness call.
//   - AuthzModeOnline:  enforce an uncached online liveness check every request.
//   - AuthzModeHybrid:  enforce an online liveness check whose positive results
//     are cached for a short TTL (the default mode).
//
// The scope decision is always offline and runs before any liveness call, so an
// already-denied request never incurs a network round trip. Liveness failures
// fail closed: a transport error denies, and an inactive token denies even when
// its scopes and signature are valid.
//
// requiredPerm is the qrdb-canonical "resource:action" permission the operation
// demands. dpopProof/htm/htu are the presented DPoP proof and the bound HTTP
// method and URI; they are consulted only when the token carries a cnf.jkt
// binding (a plain bearer service token is authorized without a proof).
func (v *Validator) Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*AuthorizedContext, error) {
	ac, claims, err := v.validateAndProject(ctx, token, dpopProof, htm, htu)
	if err != nil {
		return nil, err
	}
	// The scope decision is offline and runs BEFORE any liveness network call,
	// so an already-denied request never incurs a round trip.
	if err := ac.RequireScope(requiredPerm); err != nil {
		return nil, err
	}
	if err := v.enforceLivenessByMode(ctx, token, claims); err != nil {
		return nil, err
	}
	return ac, nil
}

// Validate verifies a token exactly like Authorize but WITHOUT a scope check. It
// is the liveness-bearing entry point for authenticated operations that carry no
// scope requirement: signature/iss/aud/exp, DPoP when the token is
// sender-constrained, and per-tenant liveness/revocation are all enforced; only
// RequireScope is skipped. Prefer Authorize for any operation that maps to a
// required permission; use Validate only for genuinely scope-free endpoints so a
// missing mapping never silently degrades into an unauthorized allow.
func (v *Validator) Validate(ctx context.Context, token, dpopProof, htm, htu string) (*AuthorizedContext, error) {
	ac, claims, err := v.validateAndProject(ctx, token, dpopProof, htm, htu)
	if err != nil {
		return nil, err
	}
	if err := v.enforceLivenessByMode(ctx, token, claims); err != nil {
		return nil, err
	}
	return ac, nil
}

// validateAndProject performs the shared offline verification used by both
// Authorize and Validate: token signature/iss/aud/exp, DPoP when the token is
// sender-constrained (reusing ValidateWithDPoP for one DPoP code path), and
// projection of the verified claims into an AuthorizedContext.
func (v *Validator) validateAndProject(ctx context.Context, token, dpopProof, htm, htu string) (*AuthorizedContext, *Claims, error) {
	claims, err := v.ValidatePlatformToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if claims.Confirmation != nil && claims.Confirmation.Jkt != "" {
		if v.replayCache == nil {
			return nil, nil, &ConfigError{Detail: "replay cache required for sender-constrained token"}
		}
		if _, err := v.ValidateWithDPoP(ctx, token, dpopProof, htm, htu, v.replayCache); err != nil {
			return nil, nil, err
		}
	}
	return Authorize(claims), claims, nil
}

// enforceLivenessByMode resolves the tenant's authz mode and runs the liveness
// check it requires: offline=none, online=uncached, hybrid=cached. An unknown
// mode is a fail-closed misconfiguration rather than a silent skip.
func (v *Validator) enforceLivenessByMode(ctx context.Context, token string, claims *Claims) error {
	mode := AuthzModeHybrid
	if v.modeResolver != nil {
		if resolved := v.modeResolver(claims.TenantID); resolved != "" {
			mode = resolved
		}
	}
	switch mode {
	case AuthzModeOffline:
		return nil
	case AuthzModeOnline:
		return v.enforceLiveness(ctx, v.liveness, token, claims, "online")
	case AuthzModeHybrid:
		return v.enforceLiveness(ctx, v.cachedLiveness, token, claims, "hybrid")
	default:
		return &ConfigError{Detail: "unknown authz_mode: " + string(mode)}
	}
}

// enforceLiveness runs the liveness check for a mode that requires it,
// translating its outcomes into typed authorization errors. A nil checker is a
// fail-closed configuration error; a transport error denies; an inactive token
// denies.
func (v *Validator) enforceLiveness(ctx context.Context, checker LivenessChecker, token string, claims *Claims, mode string) error {
	if checker == nil {
		return &ConfigError{Detail: "liveness checker required for " + mode + " authz_mode"}
	}
	active, err := checker.CheckLiveness(ctx, token, claims)
	if err != nil {
		// Surface an already-typed liveness error verbatim; wrap any other
		// backend error so the caller always sees a *LivenessCheckError.
		var lce *LivenessCheckError
		if errors.As(err, &lce) {
			return lce
		}
		return &LivenessCheckError{Cause: err}
	}
	if !active {
		return &TokenInactiveError{Subject: claims.Subject, JTI: claims.ID}
	}
	return nil
}
