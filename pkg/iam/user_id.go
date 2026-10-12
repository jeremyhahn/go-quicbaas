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
	"strconv"
	"strings"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// The user identity is go-qrdb's Snowflake, and User.ID carries it in decimal —
// the rendering qrdb's own transport type uses. These two functions are the ONLY
// conversion between the two forms.
//
// They replace an IDMap: a persisted, tenant-scoped, bidirectional correlation
// between a broker-minted UUID and qrdb's uint64. That table existed because the
// two layers minted different identifiers for one person, and it could fail — a
// verified login whose record had no correlation entry was refused with "id
// correlation not found" while the record it named was present all along. A
// conversion has no such failure: it is total in one direction and refuses only
// input that never named a user in the first place.

// ErrInvalidUserID reports a user id that is not a qrdb Snowflake. Anything but
// the decimal rendering of a non-zero uint64 never named a stored user, so
// resolving it would be a guess rather than a lookup.
var ErrInvalidUserID = &dberrors.QBError{
	Code: dberrors.ErrInvalidInput,
	Op:   "iam.user.id",
	Err:  &IAMMessageError{Message: "user id must be a qrdb snowflake in decimal"},
}

// ParseUserID converts an external user id into qrdb's numeric id — the id an
// auth factor binds to and a login resolver resolves a principal from.
//
// Only the CANONICAL rendering is accepted. "00042" parses to the same integer
// as "42", and accepting both would give one user two spellings — which matters
// because callers verify a user exists by id and then persist the string they
// were handed, so the two spellings become two keys for one account and slip
// past any per-user duplicate check.
func ParseUserID(userID string) (uint64, error) {
	if userID == "" {
		return 0, ErrInvalidUserID
	}
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil || id == 0 {
		return 0, ErrInvalidUserID
	}
	if FormatUserID(id) != userID {
		return 0, ErrInvalidUserID
	}
	return id, nil
}

// FormatUserID renders a qrdb numeric id as an external user id. A zero id is
// the store's invalid sentinel and has no external form, so it yields "".
func FormatUserID(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
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

// QRDBID returns the user's qrdb numeric id. It is the same value ID carries,
// in the form the store and the auth-factor surfaces address it by.
func (u *User) QRDBID() (uint64, error) {
	if u == nil {
		return 0, ErrInvalidUserID
	}
	return ParseUserID(u.ID)
}
