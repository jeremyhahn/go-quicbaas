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
	claims, err := v.ValidatePlatformToken(ctx, token)
	if err != nil {
		return nil, err
	}

	// Sender-constrained tokens must present a valid DPoP proof. Reuse
	// ValidateWithDPoP so both entry points share one DPoP code path.
	if claims.Confirmation != nil && claims.Confirmation.Jkt != "" {
		if v.replayCache == nil {
			return nil, &ConfigError{Detail: "replay cache required for sender-constrained token"}
		}
		if _, err := v.ValidateWithDPoP(ctx, token, dpopProof, htm, htu, v.replayCache); err != nil {
			return nil, err
		}
	}

	ac := Authorize(claims)
	if err := ac.RequireScope(requiredPerm); err != nil {
		return nil, err
	}

	mode := AuthzModeHybrid
	if v.modeResolver != nil {
		if resolved := v.modeResolver(claims.TenantID); resolved != "" {
			mode = resolved
		}
	}

	switch mode {
	case AuthzModeOffline:
		return ac, nil
	case AuthzModeOnline:
		if err := v.enforceLiveness(ctx, v.liveness, token, claims, "online"); err != nil {
			return nil, err
		}
		return ac, nil
	case AuthzModeHybrid:
		if err := v.enforceLiveness(ctx, v.cachedLiveness, token, claims, "hybrid"); err != nil {
			return nil, err
		}
		return ac, nil
	default:
		// An unknown mode is a misconfiguration; fail closed rather than
		// silently skipping the liveness check.
		return nil, &ConfigError{Detail: "unknown authz_mode: " + string(mode)}
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
