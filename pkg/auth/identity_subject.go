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

package auth

import (
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// A verified qrdbsdk.Identity names its caller in Subject, and that string is
// whatever the caller logged in with:
//
//   - mTLS: qrdb resolves the client certificate's SPKI binding to the user and
//     sets Subject to that user's USERNAME (an administrator is "admin").
//   - a bearer token quicbaas issued (REST and MCP bearer authenticators, the
//     OIDC session issuer, the platform bearer): Subject is the token's sub
//     claim, the email or username the login ceremony authenticated.
//
// Neither is the account's immutable key, and a username may be all digits,
// so Subject is never read as a user ID. A token also carries the user's qrdb
// ID in its signed uid claim; each quicbaas bearer authenticator marks the
// identity it builds with MarkTokenSubject, recording that uid, and
// SubjectFromIdentity surfaces it as Subject.UserID. An unmarked identity is
// qrdb's own, whose Subject is a username.

// identityUserIDKey is the Identity.Metadata key of the mark. Its presence
// says a quicbaas token authenticated the caller; its value is the token's
// verified uid claim, empty when the token carried none. It is namespaced so
// a metadata entry qrdb sets cannot collide with it.
const identityUserIDKey = "quicbaas.user_id"

// MarkTokenSubject records on identity that its Subject is the sub of a token
// quicbaas issued, and that userID (the token's verified uid claim, possibly
// empty) names the caller's account. It returns identity for use in a return
// statement; a nil identity is returned unchanged.
func MarkTokenSubject(identity *qrdbsdk.Identity, userID string) *qrdbsdk.Identity {
	if identity == nil {
		return nil
	}
	if identity.Metadata == nil {
		identity.Metadata = make(map[string]string, 1)
	}
	identity.Metadata[identityUserIDKey] = userID
	return identity
}

// SubjectIDKindOf classifies a token's sub the way the login ceremony keyed
// it: an email when it contains "@", which iam's username rule does not
// admit, and a username otherwise. It never answers SubjectIDUserID, because
// a login name is not proof of an account ID.
func SubjectIDKindOf(sub string) interfaces.SubjectIDKind {
	if strings.Contains(sub, "@") {
		return interfaces.SubjectIDEmail
	}
	return interfaces.SubjectIDUsername
}

// SubjectFromIdentity projects a verified qrdb identity onto the authenticated
// Subject the service layer reads. The identity came from
// qrdbsdk.IdentityFromContext, so qrdb has already verified it and the subject
// is authenticated. For an identity marked by MarkTokenSubject, IDKind
// classifies the sub by SubjectIDKindOf and UserID is the recorded uid; every
// other identity is qrdb's own and its Subject is a username.
func SubjectFromIdentity(identity *qrdbsdk.Identity) interfaces.Subject {
	subject := interfaces.Subject{
		ID:            identity.Subject,
		Username:      identity.Subject,
		TenantID:      identity.TenantID,
		Roles:         identity.Roles,
		Authenticated: true,
		IDKind:        interfaces.SubjectIDUsername,
	}
	if userID, fromToken := identity.Metadata[identityUserIDKey]; fromToken {
		subject.IDKind = SubjectIDKindOf(identity.Subject)
		subject.UserID = userID
	}
	return subject
}

// IsSystemSubject reports whether an authenticated subject acts as the system
// tenant. Only a subject bound to qrdbsdk.SystemTenantID itself is system. An
// empty TenantID is a caller with no tenant binding, never the platform scope,
// even though qrdbsdk.IsSystemTenant("") is true for storage routing: an
// authenticated principal with an empty tenant never acts as system (#348).
func IsSystemSubject(subject interfaces.Subject) bool {
	return subject.TenantID == qrdbsdk.SystemTenantID
}
