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

package embedded

import (
	"context"
	"strconv"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Session handlers (quicbaas.IAM.Session/*).
//
// The gRPC and REST transports have carried the Session service since it
// shipped; the embedded transport did not, which left an in-process host —
// the deployment shape a platform product runs — unable to list or revoke a
// user's sessions at all. These handlers close that gap against the same
// service.SessionManager the REST handlers use.
//
// The session surface is SELF-SCOPED: over REST every operation is bound to
// the authenticated Subject, and naming another user's session answers 403
// (see pkg/api/rest/iam_sessions.go). These handlers apply the same rule
// whenever the request context carries a Subject, so putting the SDK behind an
// authenticated surface does not turn the embedded transport into a way to
// read or revoke a third party's sessions. A context with no Subject is the
// host process calling its own service before any transport has run — the
// pre-existing contract for every embedded IAM handler — and is unrestricted.

type sessionManagerProvider interface {
	GetSessionManager(ctx context.Context) (service.SessionManager, error)
}

// sessionManagerFor resolves the session manager through the same structural
// type assertion the rest of the embedded IAM dispatch uses, so callers never
// depend on the concrete service implementation.
func sessionManagerFor(ctx context.Context, svc service.QuicBaaS) (service.SessionManager, error) {
	p, ok := svc.(sessionManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError("IAM.Session")
	}
	mgr, err := p.GetSessionManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError("IAM.Session")
	}
	return mgr, nil
}

// requireSessionOwner enforces that the session belongs to the authenticated
// subject. It mirrors the REST handlers' ownership gate exactly: a mismatch is
// denied rather than served, so a caller cannot read or revoke a session by
// guessing its id.
func requireSessionOwner(subject interfaces.Subject, s *iam.SessionExt, method string) error {
	if !s.OwnedBy(subject.ID, subject.TenantID) {
		return NewPermissionDeniedError("quicbaas.IAM.Session", method, "access denied")
	}
	return nil
}

// requestedIdentityMatches enforces that an explicit user/tenant on the
// request agrees with the authenticated subject. Silently substituting the
// subject's own identity would answer a different question than the one asked,
// which is the same reason the REST surface rejects the mismatch.
func requestedIdentityMatches(subject interfaces.Subject, userID, tenantID string) bool {
	if userID != "" && userID != subject.ID {
		return false
	}
	if tenantID != "" && tenantID != subject.TenantID {
		return false
	}
	return true
}

func handleIAMSessionGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "SessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Session", "Get",
			"struct with SessionID field", typeNameOf(req))
	}
	s, err := mgr.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		if err := requireSessionOwner(subject, s, "Get"); err != nil {
			return nil, err
		}
	}
	return sessionToSDK(s), nil
}

func handleIAMSessionList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	r, ok := iamRequestPayload[iamsdk.ListSessionsRequest](req, "Request")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Session", "List",
			"*iam.ListSessionsRequest", typeNameOf(req))
	}

	userID, tenantID := r.UserID, r.TenantID
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		if !requestedIdentityMatches(subject, userID, tenantID) {
			return nil, NewPermissionDeniedError("quicbaas.IAM.Session", "List",
				"requested identity does not match the authenticated subject")
		}
		userID, tenantID = subject.ID, subject.TenantID
	}

	// SessionManager.List returns active sessions only — expired rows never
	// leave storage through this seam — so IncludeExpired cannot widen the
	// result here any more than it can over REST or gRPC. Honoring it would
	// require a manager operation that does not exist, and pretending to
	// honor it would be worse than leaving the shared behavior identical.
	var id uint64
	if userID != "" {
		if id, err = parseIAMUserID("quicbaas.IAM.Session", "List", userID); err != nil {
			return nil, err
		}
	}
	sessions, err := mgr.List(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	out := make([]iamsdk.Session, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		out = append(out, *sessionToSDK(s))
	}

	total := len(out)
	out = applySessionWindow(out, r.Offset, r.Limit)

	return &iamsdk.ListSessionsResponse{
		Sessions:   out,
		TotalCount: total,
		HasMore:    r.Offset+len(out) < total,
	}, nil
}

// applySessionWindow applies the request's offset and limit to an already
// filtered session slice. An offset past the end yields an empty page rather
// than an error: paging off the end of a shrinking list is normal, not a
// client mistake.
func applySessionWindow(sessions []iamsdk.Session, offset, limit int) []iamsdk.Session {
	if offset > 0 {
		if offset >= len(sessions) {
			return []iamsdk.Session{}
		}
		sessions = sessions[offset:]
	}
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions
}

func handleIAMSessionRevoke(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	id, ok := iamRequestField(req, "SessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Session", "Revoke",
			"struct with SessionID field", typeNameOf(req))
	}
	// Ownership is checked before the revoke, not after: a denied caller must
	// not be able to destroy a session it was never allowed to see.
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		s, getErr := mgr.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		if err := requireSessionOwner(subject, s, "Revoke"); err != nil {
			return nil, err
		}
	}
	if err := mgr.Revoke(ctx, id); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleIAMSessionTouch records activity on a session through
// SessionManager.UpdateActivity, which stamps server time: the client's
// Timestamp field is not carried. Touch is a void operation, so success
// returns nil.
func handleIAMSessionTouch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	id, ok := iamRequestField(req, "SessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Session", "Touch",
			"struct with SessionID field", typeNameOf(req))
	}
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		s, getErr := mgr.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		if err := requireSessionOwner(subject, s, "Touch"); err != nil {
			return nil, err
		}
	}
	if err := mgr.UpdateActivity(ctx, id); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMSessionRevokeAll(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, hasTenant := iamRequestField(req, "TenantID")
	userID, hasUser := iamRequestField(req, "UserID")
	if !hasTenant || !hasUser {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Session", "RevokeAll",
			"struct with TenantID and UserID fields", typeNameOf(req))
	}
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		if !requestedIdentityMatches(subject, userID, tenantID) {
			return nil, NewPermissionDeniedError("quicbaas.IAM.Session", "RevokeAll",
				"requested identity does not match the authenticated subject")
		}
		userID, tenantID = subject.ID, subject.TenantID
	}
	var id uint64
	if userID != "" {
		if id, err = parseIAMUserID("quicbaas.IAM.Session", "RevokeAll", userID); err != nil {
			return nil, err
		}
	}
	if err := mgr.RevokeAllForUser(ctx, id, tenantID); err != nil {
		return nil, err
	}
	return nil, nil
}

func handleIAMSessionRevokeOthers(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	keepID, ok := iamRequestField(req, "KeepSessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.IAM.Session", "RevokeOthers",
			"struct with KeepSessionID field", typeNameOf(req))
	}

	// The caller only names the session to keep, so the owning user and
	// tenant are read back from that session. Deriving them here rather than
	// trusting caller-supplied ids also means a caller cannot revoke a third
	// party's sessions by naming their own session alongside someone else's
	// user id.
	keep, err := mgr.Get(ctx, keepID)
	if err != nil {
		return nil, err
	}
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		if err := requireSessionOwner(subject, keep, "RevokeOthers"); err != nil {
			return nil, err
		}
	}
	if err := mgr.RevokeAllExcept(ctx, keep.UserID, keep.TenantID, keepID); err != nil {
		return nil, err
	}
	return nil, nil
}

// sessionToSDK converts the domain session record to the SDK's session type.
// DeviceInfo collapses to its human-readable name because the SDK models it
// as a single display string.
func sessionToSDK(s *iam.SessionExt) *iamsdk.Session {
	if s == nil {
		return nil
	}
	out := &iamsdk.Session{
		ID:          s.ID,
		UserID:      strconv.FormatUint(s.UserID, 10),
		TenantID:    s.TenantID,
		CreatedAt:   s.Created,
		Created:     s.Created,
		LastActive:  s.LastActive,
		ExpiresAt:   s.ExpiresAt,
		MFAVerified: s.MFAVerified,
		IPAddress:   s.IPAddress,
		UserAgent:   s.UserAgent,
	}
	if s.DeviceInfo != nil {
		out.DeviceInfo = s.DeviceInfo.Name
	}
	if s.Metadata != nil {
		out.AuthMethod = s.Metadata["auth_method"]
	}
	return out
}
