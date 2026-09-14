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

package iam

import "errors"

// This file holds the narrow CONDITION predicates for the IAM sentinels whose
// callers need the specific condition rather than the error class.
//
// WHY THEY EXIST. Every Err* value in this package is a *dberrors.QBError,
// which is a type alias for go-qrdb's *errors.QRDBError, and QRDBError.Is
// compares the error CODE and nothing else:
//
//	func (e *QRDBError) Is(target error) bool {
//		var de *QRDBError
//		if errors.As(target, &de) {
//			return e.Code == de.Code   // <- code, not identity
//		}
//		...
//	}
//
// So errors.Is(err, ErrInviteExpired) is true for EVERY error carrying the
// Forbidden code -- ErrInviteUsed, ErrPIVCertRejected, ErrTenantSuspended, a
// forbidden bubbling up from the store. The call site reads like idiomatic
// sentinel matching and is not: it over-matches, so it takes a branch written
// for a narrower case (go-quicbaas#434).
//
// The fix follows the shape #411 established for MFA: a dedicated typed error
// carried as the sentinel's inner Err, matched with errors.As. That makes the
// predicate POSITIVE about one condition, so a failure mode added later --
// upstream, in a store, in a transport -- lands in the caller's default bucket
// instead of silently joining the matched set.
//
// Matching by code is not wrong everywhere: a REST bridge that answers 404 for
// any not-found is asking about the class on purpose, and should keep using
// dberrors.IsNotFound. These predicates are for the callers that mean the one
// condition. scripts/codedsentinel enforces the distinction.

// UserNotFoundError reports that a user record is absent. It is the inner
// error of ErrUserNotFound, so it identifies that sentinel specifically rather
// than every not-found the identity path can raise.
type UserNotFoundError struct{}

// Error implements the error interface.
func (e *UserNotFoundError) Error() string { return "user not found" }

// IsUserNotFound reports whether err is (or wraps) ErrUserNotFound -- the user
// record is absent -- as opposed to any other not-found the lookup can raise.
func IsUserNotFound(err error) bool {
	var target *UserNotFoundError
	return errors.As(err, &target)
}

// RoleNotFoundError reports that a role record is absent. It is the inner
// error of ErrRoleNotFound.
type RoleNotFoundError struct{}

// Error implements the error interface.
func (e *RoleNotFoundError) Error() string { return "role not found" }

// IsRoleNotFound reports whether err is (or wraps) ErrRoleNotFound.
func IsRoleNotFound(err error) bool {
	var target *RoleNotFoundError
	return errors.As(err, &target)
}

// MFANotEnrolledError reports that a user has no MFA enrollment. It is the
// inner error of ErrMFANotEnrolled.
type MFANotEnrolledError struct{}

// Error implements the error interface.
func (e *MFANotEnrolledError) Error() string { return "MFA not enrolled" }

// IsMFANotEnrolled reports whether err is (or wraps) ErrMFANotEnrolled -- this
// user has no enrollment -- as opposed to any other not-found the MFA store
// can raise, which is a failure to READ the enrollment and must not be
// reported as its absence.
func IsMFANotEnrolled(err error) bool {
	var target *MFANotEnrolledError
	return errors.As(err, &target)
}

// InviteUnusableReason names why an invitation cannot be redeemed.
type InviteUnusableReason string

const (
	// InviteAbsent means no invitation matches the token.
	InviteAbsent InviteUnusableReason = "invite not found"

	// InviteExpired means the invitation passed its expiry time.
	InviteExpired InviteUnusableReason = "invite has expired"

	// InviteRedeemed means the invitation has already been consumed.
	InviteRedeemed InviteUnusableReason = "invite has already been used"
)

// InviteUnusableError reports that an invitation cannot be redeemed because of
// its OWN state -- absent, expired, or already consumed -- as opposed to a
// failure of the store that holds it. It is the inner error of
// ErrInviteNotFound, ErrInviteExpired and ErrInviteUsed.
//
// The three reasons share one type because the public activation routes
// deliberately answer all three identically (see respondInviteUnusable): an
// unauthenticated caller must not learn that a token exists but is spent. The
// Reason field keeps the distinction for logs and for callers inside the trust
// boundary.
type InviteUnusableError struct {
	Reason InviteUnusableReason
}

// Error implements the error interface.
func (e *InviteUnusableError) Error() string { return string(e.Reason) }

// IsInviteUnusable reports whether err is (or wraps) one of the three invite
// sentinels -- the invitation itself is absent, expired or spent -- as opposed
// to a storage or sealed-tenant failure, which must keep its own status rather
// than being reported to the invitee as a bad link.
func IsInviteUnusable(err error) bool {
	var target *InviteUnusableError
	return errors.As(err, &target)
}

// UserNotPendingError reports that an activation was attempted on a user who
// is not awaiting activation. It is the inner error of ErrUserNotPending.
type UserNotPendingError struct{}

// Error implements the error interface.
func (e *UserNotPendingError) Error() string { return "user is not in pending state" }

// IsUserNotPending reports whether err is (or wraps) ErrUserNotPending.
func IsUserNotPending(err error) bool {
	var target *UserNotPendingError
	return errors.As(err, &target)
}

// PIVCertRejectedError reports that a presented PIV certificate failed chain
// verification or did not satisfy the required key usage. It is the inner
// error of ErrPIVCertRejected.
type PIVCertRejectedError struct{}

// Error implements the error interface.
func (e *PIVCertRejectedError) Error() string { return "PIV certificate rejected" }

// IsPIVCertRejected reports whether err is (or wraps) ErrPIVCertRejected.
//
// This one is why the audit found the invite routes: ErrPIVCertRejected and
// ErrInviteExpired both carry the Forbidden code, so the code-comparing
// errors.Is matched a rejected certificate against the earlier invite case and
// the 422 branch below it could never run.
func IsPIVCertRejected(err error) bool {
	var target *PIVCertRejectedError
	return errors.As(err, &target)
}
