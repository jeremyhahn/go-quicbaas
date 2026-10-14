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

import (
	"strings"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// The user identity is go-qrdb's Snowflake, and User.ID carries it as the same
// uint64 the store assigns. There is no second representation to convert to or
// from: the JSON encoding renders it as a decimal string, the form qrdb's own
// transport uses, because a JSON number above 2^53 loses precision in a
// JavaScript client. YAML has no string option and carries the plain integer.
//
// This replaces an IDMap — a persisted, tenant-scoped correlation between a
// broker-minted UUID and qrdb's uint64 — and, after it, a decimal-string id that
// every store call parsed and every write formatted. Both existed because the
// broker held the id in a different form from the store; holding it in the
// store's form removes the conversion and the failures it could have.

// ErrInvalidUserID reports a user id that is not a qrdb Snowflake: zero, which
// is the "not created yet" value, or text at a string-typed edge that is not the
// decimal rendering of a non-zero uint64. Neither named a stored user, so
// resolving it would be a guess rather than a lookup.
var ErrInvalidUserID = &dberrors.QBError{
	Code: dberrors.ErrInvalidInput,
	Op:   "iam.user.id",
	Err:  &IAMMessageError{Message: "user id must be a qrdb snowflake in decimal"},
}

// LoginName returns the tenant-unique name this user is known by in the store's
// username index.
//
// It is the USERNAME, not the email and not the display name. qrdb models all
// three as separate fields on purpose — "human users carry both; the email is
// the one anything above qrdb keys on" — and the username is what an
// administrator types to assign a role. Collapsing it onto the email would make
// every such lookup miss; collapsing it onto the display name, which is what
// this used to do, made the lookup miss for anyone whose display name was not
// also their handle, and pointed a uniqueness constraint at a field that has
// none.
//
// The collision that used to make this dangerous is closed elsewhere and by
// construction: resolveSubject consults the username index ONLY for a subject
// that is not email-shaped, so an email-shaped subject can never be resolved
// through a username, whatever a display name happens to contain.
//
// Both UserStorage implementations resolve GetUserByUsername through this, so
// the in-memory store cannot answer the question differently from the
// qrdb-backed one and let a test pass over a production failure.
func (u *User) LoginName() string {
	if u == nil {
		return ""
	}
	return u.Username
}

// RequireLoginKey refuses a user record that nothing could authenticate.
//
// qrdb's email is the canonical login key, and a record may omit it only under
// WithoutEmailLoginKey, for an identity that authenticates by certificate. The
// one such record this build creates is a fleet node (ensureFleetNodeCertUser),
// which carries its certificate binding at creation; the first-boot admin is
// not one, since it is created with a synthetic email and bound afterwards. A
// record with neither an email nor a certificate binding names a principal no
// ceremony can resolve, so it is refused when it is written rather than at its
// first login.
//
// Both UserStorage implementations call this from CreateUser, so the in-memory
// store refuses exactly what the qrdb-backed one does.
func RequireLoginKey(u *User) error {
	if strings.TrimSpace(u.Email) != "" {
		return nil
	}
	for i := range u.CertBindings {
		if u.CertBindings[i].Thumbprint != "" {
			return nil
		}
	}
	return NewPrincipalWithoutLoginKeyError(u.TenantID, u.Username)
}
