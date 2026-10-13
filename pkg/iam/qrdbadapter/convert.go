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

package qrdbadapter

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// This file projects between iam.User and qrdb's canonical sdk.User. It is a
// PROJECTION, not a split: every iam.User field is stored by qrdb, so there is
// no side-record to write and no second identifier space to reconcile.
//
// What used to live here — a BrokerProfile carrying fourteen fields qrdb could
// not model, and an IDMapEntry correlating a broker UUID with qrdb's uint64 —
// is gone. qrdb owns Email, Name, AuthMethods, Lifecycle and LastAuth
// natively now, and the identity is qrdb's Snowflake rendered in decimal, so the
// correlation is strconv rather than a table that can fail to resolve.
//
// The two field groups qrdb still does not model are NOT carried here either,
// because each already has an owner in this repository: MFA state belongs to
// MFAStorage (which DefaultMFAManager.GetMFAStatus reads and the user record
// never did), and invitations belong to iam.InviteStore, which delegates to
// qrdb's TenantInviteStore. Mirroring either onto the identity record is what
// this change removes.

// ParseUserID and FormatUserID are the canonical id conversions, owned by
// pkg/iam because the rendering is a property of iam.User rather than of this
// adapter. They are aliased here so the projection reads in one place.
var (
	// ParseUserID converts the external decimal user id into qrdb's uint64.
	ParseUserID = iam.ParseUserID

	// FormatUserID renders a qrdb Snowflake as the external user id.
	FormatUserID = iam.FormatUserID
)

// ErrInvalidUserID reports a user id that is not a qrdb Snowflake.
var ErrInvalidUserID = iam.ErrInvalidUserID

// ToIAMUser projects qrdb's canonical user onto iam.User. It is total: every
// field comes from the record, so there is no partially-composed user and no
// "the side-record was missing" case.
func ToIAMUser(core qrdbsdk.User) *iam.User {
	return &iam.User{
		ID:    FormatUserID(core.ID),
		Email: core.Email,
		// qrdb's Username has a home on iam.User now, so the projection is
		// round-trip faithful: it used to be read only as a display-name
		// fallback and was otherwise dropped, while the write side fabricated
		// a Username out of the display name.
		Username:     core.Username,
		Name:         displayName(core),
		TenantID:     core.TenantID,
		Roles:        cloneStrings(core.RoleIDs),
		AuthMethods:  cloneStrings(core.AuthMethods),
		Lifecycle:    core.Lifecycle,
		LastAuth:     core.LastAuth,
		Attributes:   cloneAttributes(core.Attributes),
		CertBindings: cloneCertBindings(core.CertBindings),
		Created:      core.CreatedAt,
		Updated:      core.UpdatedAt,
	}
}

// FromIAMUser projects an iam.User onto qrdb's canonical user. The returned
// core carries the parsed Snowflake, or zero for a user that has not been
// created yet (qrdb assigns the id server-side), which is why an empty ID is
// accepted here while a malformed one is refused.
func FromIAMUser(u *iam.User) (qrdbsdk.User, error) {
	if u == nil {
		return qrdbsdk.User{}, ErrInvalidUserID
	}
	var id uint64
	if u.ID != "" {
		parsed, err := ParseUserID(u.ID)
		if err != nil {
			return qrdbsdk.User{}, err
		}
		id = parsed
	}
	// Normalization is qrdb's, not a local copy of it: a consumer that
	// de-duplicates or case-folds the set differently would disagree with the
	// store about what the user may present.
	authMethods, err := qrdbsdk.NormalizeAuthMethods(u.AuthMethods)
	if err != nil {
		return qrdbsdk.User{}, err
	}
	return qrdbsdk.User{
		ID:           id,
		Username:     CoreUsername(u),
		Email:        qrdbsdk.NormalizeEmail(u.Email),
		Name:         u.Name,
		TenantID:     u.TenantID,
		RoleIDs:      cloneStrings(u.Roles),
		AuthMethods:  authMethods,
		Lifecycle:    u.Lifecycle,
		LastAuth:     u.LastAuth,
		Attributes:   cloneAttributes(u.Attributes),
		CertBindings: cloneCertBindings(u.CertBindings),
		CreatedAt:    u.Created,
		UpdatedAt:    u.Updated,
	}, nil
}

// CoreUsername returns the qrdb Username for a broker user — the account's
// tenant-unique handle, which is what an administrator addresses it by. The
// rule belongs to iam.User.LoginName so both UserStorage implementations share
// it; this is the name the projection writes into the qrdb record.
func CoreUsername(u *iam.User) string {
	return u.LoginName()
}

// displayName returns the presentation name for a qrdb record. qrdb's Name is
// authoritative when set; a record written before Name existed (or a system
// user, which carries a username and no display name) falls back to Username so
// the projection never surfaces a nameless principal.
func displayName(core qrdbsdk.User) string {
	if core.Name != "" {
		return core.Name
	}
	return core.Username
}

// ToIAMRole composes a broker iam.Role from qrdb's canonical role core. qrdb has
// no Description field, so the broker Description is left empty (a later phase
// may carry it in a side-record if needed); permissions are stringified via the
// shared convertPermissions helper.
func ToIAMRole(core qrdbsdk.Role) *iam.Role {
	return &iam.Role{
		ID:          core.ID,
		Name:        core.Name,
		TenantID:    core.TenantID,
		Permissions: convertPermissions(core.Permissions),
		// Derived by qrdb on read from the live permission registry, never
		// stored. Carrying it keeps this converter faithful to the one in
		// pkg/iam (transportRoleToIAM): both project the same drift signal.
		RetiredPermissions: convertPermissions(core.RetiredPermissions),
		Created:            core.CreatedAt,
		Updated:            core.UpdatedAt,
	}
}

// FromIAMRole splits a broker iam.Role into qrdb's canonical role core. The
// broker Description has no qrdb home and is intentionally dropped (see
// ToIAMRole); permission strings are parsed back into typed qrdb permissions.
// RetiredPermissions is deliberately NOT carried back: qrdb derives it on read
// and clears it on every write, so sending one would be sending a value the
// store is guaranteed to discard.
// Any string that does not parse as "resource:action" is skipped so a single
// malformed entry cannot corrupt the whole role.
func FromIAMRole(r *iam.Role) qrdbsdk.Role {
	return qrdbsdk.Role{
		ID:          r.ID,
		Name:        r.Name,
		TenantID:    r.TenantID,
		Permissions: parsePermissions(r.Permissions),
		CreatedAt:   r.Created,
		UpdatedAt:   r.Updated,
	}
}

// parsePermissions maps the broker's permission strings to qrdb's typed
// permission slice, skipping any malformed entry.
func parsePermissions(in []string) []qrdbsdk.Permission {
	if len(in) == 0 {
		return nil
	}
	out := make([]qrdbsdk.Permission, 0, len(in))
	for _, s := range in {
		p, err := qrdbsdk.ParsePermission(s)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cloneAttributes copies the ABAC attribute map so the projection and the store
// record never alias one backing map. A nil/empty input yields nil to keep an
// empty round-trip nil-equal.
func cloneAttributes(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneStrings returns a copy of in so the two models never share a backing
// slice. A nil/empty input yields nil to keep empty round-trips nil-equal.
func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// cloneCertBindings returns a copy of in so the projection and the store record
// never share a backing slice. CertBinding is a value type, so a shallow element
// copy is a deep copy. A nil/empty input yields nil to keep empty round-trips
// nil-equal.
func cloneCertBindings(in []qrdbsdk.CertBinding) []qrdbsdk.CertBinding {
	if len(in) == 0 {
		return nil
	}
	out := make([]qrdbsdk.CertBinding, len(in))
	copy(out, in)
	return out
}
