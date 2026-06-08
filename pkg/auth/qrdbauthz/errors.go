//go:build iam

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

package qrdbauthz

// NilRBACResolverError is returned by New when the RBAC client resolver is nil.
// The resolver late-binds the qrdb node client and must always be supplied.
type NilRBACResolverError struct{}

func (e *NilRBACResolverError) Error() string {
	return "qrdbauthz: rbac client resolver is nil"
}

// MissingTenantError is returned when a Subject has an empty TenantID. A
// well-formed qrdb identity always carries a tenant; an empty tenant is a
// programming error in the calling transport, surfaced as a typed denial
// rather than a silent allow.
type MissingTenantError struct {
	Subject string
}

func (e *MissingTenantError) Error() string {
	if e.Subject != "" {
		return "qrdbauthz: subject " + e.Subject + " has no tenant"
	}
	return "qrdbauthz: subject has no tenant"
}

// PDPUnavailableError is returned when the qrdb policy decision point is not
// reachable (the node client has not been constructed yet). The adapter fails
// closed: it never silently allows when it cannot reach the PDP. Transports
// gate calling the adapter on their enforcement posture (RequireAuthz), so this
// error surfaces only when enforcement is active but the PDP is genuinely
// absent.
type PDPUnavailableError struct{}

func (e *PDPUnavailableError) Error() string {
	return "qrdbauthz: qrdb policy decision point is unavailable"
}

// ABACEvaluationError wraps a failure inside the optional ABAC overlay. The
// overlay runs only after a qrdb allow; an evaluation failure fails closed.
type ABACEvaluationError struct {
	Cause error
}

func (e *ABACEvaluationError) Error() string {
	if e.Cause != nil {
		return "qrdbauthz: abac evaluation failed: " + e.Cause.Error()
	}
	return "qrdbauthz: abac evaluation failed"
}

func (e *ABACEvaluationError) Unwrap() error { return e.Cause }

// AuthorizationDeniedError is the typed denial the adapter returns when the
// ABAC overlay explicitly denies a request that RBAC allowed. qrdb's own RBAC
// denials are surfaced unchanged (as qrdb's typed errors).
type AuthorizationDeniedError struct {
	Subject  string
	Resource string
	Action   string
	Reason   string
}

func (e *AuthorizationDeniedError) Error() string {
	msg := "qrdbauthz: authorization denied for " + e.Resource + ":" + e.Action
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}
