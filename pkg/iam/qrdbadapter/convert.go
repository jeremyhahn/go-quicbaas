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

// This file implements the bidirectional, lossless conversion between the
// broker's rich iam.User / iam.Role model and qrdb's canonical sdk.User /
// sdk.Role core, splitting the broker-only fields into a BrokerProfile
// side-record and recording the string-UUID <-> uint64 correlation as an
// IDMapEntry. It is the seam that the later identity-unification sub-phases wire
// into the live IdentityStore; nothing here is on the production path yet.
//
// Field-gap policy (locked architecture):
//   - qrdb owns the user CORE: Username, TenantID, RoleIDs, ABAC Attributes,
//     Enabled, System, timestamps.
//   - The broker owns everything qrdb's sdk.User cannot model: Email, Name,
//     Groups (qrdb has no Group type), AuthMethods, the State/Suspended/Locked
//     lifecycle, all MFA bookkeeping, the invite flow, LastAuth, and the PIV
//     thumbprint. Those live in the BrokerProfile, NEVER in qrdb Attributes.
//   - The external string UUID (iam.User.ID) stays the API identity forever; the
//     qrdb uint64 is the internal id. Their correlation is the IDMapEntry.

// IDMapEntry is the string-UUID <-> uint64 correlation produced by FromIAMUser.
// At conversion time of a NOT-yet-persisted broker user the qrdb id is unknown
// (qrdb assigns it server-side), so QRDBUserID is zero until the caller persists
// the qrdb user and then records the binding via IDMap.Link. For an existing
// user the caller pre-resolves the uint64 and the entry carries both ids.
type IDMapEntry struct {
	// TenantID scopes the correlation.
	TenantID string

	// UUID is the broker's external string identifier (iam.User.ID).
	UUID string

	// QRDBUserID is the qrdb Snowflake id, or zero when not yet assigned.
	QRDBUserID uint64
}

// ToIAMUser composes a broker iam.User from qrdb's canonical core, the broker
// side-record, and the external UUID resolved from the IDMap. A nil profile
// yields a user carrying only the qrdb-core fields (the broker-only fields stay
// zero), which keeps the conversion total for users that have no side-record
// yet. The Attributes map is copied (qrdb's string values are boxed into the
// broker's interface{} map) so the two models never alias the same backing map.
func ToIAMUser(core qrdbsdk.User, profile *iam.BrokerProfile, uuid string) *iam.User {
	u := &iam.User{
		ID:         uuid,
		TenantID:   core.TenantID,
		Name:       core.Username, // sensible default; profile.Name overrides below
		Roles:      cloneStrings(core.RoleIDs),
		Attributes: attributesToBroker(core.Attributes),
		Created:    core.CreatedAt,
		Updated:    core.UpdatedAt,
	}
	if profile != nil {
		u.Email = profile.Email
		if profile.Name != "" {
			u.Name = profile.Name
		}
		u.Groups = cloneStrings(profile.Groups)
		u.AuthMethods = cloneStrings(profile.AuthMethods)
		u.State = profile.State
		u.Suspended = profile.Suspended
		u.Locked = profile.Locked
		u.MFAEnabled = profile.MFAEnabled
		u.MFAMethod = profile.MFAMethod
		u.MFAEnrolledAt = profile.MFAEnrolledAt
		u.MFAVerified = profile.MFAVerified //nolint:staticcheck // SA1019: the legacy field is still mirrored across the wire and the storage adapter for older clients.
		u.MFATimestamp = profile.MFATimestamp
		u.LastAuth = profile.LastAuth
		u.CertBindings = cloneCertBindings(profile.CertBindings)
		u.InviteToken = profile.InviteToken
		u.InviteExpiresAt = profile.InviteExpiresAt
	}
	return u
}

// FromIAMUser splits a broker iam.User into qrdb's canonical core, the broker
// side-record, and the id correlation entry. The returned core carries no ID
// (qrdb assigns the uint64 server-side); the BrokerProfile carries every
// broker-only field keyed by the qrdb uint64 once known. qrdbID is the
// already-resolved Snowflake id, or zero for a not-yet-persisted user — it is
// stamped onto both the profile and the entry so the caller can persist the
// profile and Link the correlation after qrdb returns the id.
func FromIAMUser(u *iam.User, qrdbID uint64) (qrdbsdk.User, *iam.BrokerProfile, IDMapEntry) {
	core := qrdbsdk.User{
		ID:         qrdbID,
		Username:   userCoreName(u),
		TenantID:   u.TenantID,
		RoleIDs:    cloneStrings(u.Roles),
		Attributes: attributesToQRDB(u.Attributes),
		Enabled:    !u.Suspended && !u.Locked && u.State != iam.UserStateDisabled,
		CreatedAt:  u.Created,
		UpdatedAt:  u.Updated,
	}
	profile := &iam.BrokerProfile{
		QRDBUserID:      qrdbID,
		TenantID:        u.TenantID,
		Email:           u.Email,
		Name:            u.Name,
		Groups:          cloneStrings(u.Groups),
		AuthMethods:     cloneStrings(u.AuthMethods),
		State:           u.State,
		Suspended:       u.Suspended,
		Locked:          u.Locked,
		MFAEnabled:      u.MFAEnabled,
		MFAMethod:       u.MFAMethod,
		MFAEnrolledAt:   u.MFAEnrolledAt,
		MFAVerified:     u.MFAVerified, //nolint:staticcheck // SA1019: the legacy field is still mirrored across the wire and the storage adapter for older clients.
		MFATimestamp:    u.MFATimestamp,
		LastAuth:        u.LastAuth,
		CertBindings:    cloneCertBindings(u.CertBindings),
		InviteToken:     u.InviteToken,
		InviteExpiresAt: u.InviteExpiresAt,
		Created:         u.Created,
		Updated:         u.Updated,
	}
	entry := IDMapEntry{TenantID: u.TenantID, UUID: u.ID, QRDBUserID: qrdbID}
	return core, profile, entry
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
		Created:     core.CreatedAt,
		Updated:     core.UpdatedAt,
	}
}

// FromIAMRole splits a broker iam.Role into qrdb's canonical role core. The
// broker Description has no qrdb home and is intentionally dropped (see
// ToIAMRole); permission strings are parsed back into typed qrdb permissions.
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

// userCoreName returns the qrdb Username for a broker user. iam.User has no
// dedicated Username field; the broker uses Name as the human login name, so it
// is the canonical source for qrdb's Username.
func userCoreName(u *iam.User) string {
	return u.Name
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

// attributesToBroker boxes qrdb's map[string]string ABAC attributes into the
// broker's map[string]interface{}. A nil/empty input yields nil so an empty
// round-trip stays nil-equal.
func attributesToBroker(in map[string]string) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// attributesToQRDB unboxes the broker's map[string]interface{} into qrdb's
// map[string]string. Non-string values are stringified via a best-effort cast;
// values that are not strings are dropped so qrdb's typed map stays clean.
func attributesToQRDB(in map[string]interface{}) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
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

// legacyPIVThumbprintsFromBindings returns the legacy (hex SHA-256 of the cert
// DER) PIV-attested thumbprints from a binding slice — the subset the broker
// keeps in its own legacy-PIV index because qrdb's SPKI index cannot resolve
// them. It mirrors iam.User.LegacyPIVThumbprints for use on a BrokerProfile's
// detached binding slice (e.g. reconciling indexes on update/delete).
func legacyPIVThumbprintsFromBindings(in []qrdbsdk.CertBinding) []string {
	out := make([]string, 0, len(in))
	for i := range in {
		b := in[i]
		if b.Kind == qrdbsdk.CertBindingPIVAttested && b.Thumbprint != "" && isLegacyHexThumbprint(b.Thumbprint) {
			out = append(out, b.Thumbprint)
		}
	}
	return out
}

// isLegacyHexThumbprint reports whether s is a legacy PIV thumbprint: the hex
// encoding of a SHA-256 digest (64 lowercase hex characters). A canonical SPKI
// thumbprint is base64url of 32 bytes (43 characters) and never matches.
func isLegacyHexThumbprint(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// cloneCertBindings returns a copy of in so the broker iam.User and the
// BrokerProfile never share a backing slice. CertBinding is a value type, so a
// shallow element copy is a deep copy. A nil/empty input yields nil to keep
// empty round-trips nil-equal.
func cloneCertBindings(in []qrdbsdk.CertBinding) []qrdbsdk.CertBinding {
	if len(in) == 0 {
		return nil
	}
	out := make([]qrdbsdk.CertBinding, len(in))
	copy(out, in)
	return out
}
