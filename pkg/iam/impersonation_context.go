// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"context"
	"strconv"
)

// impersonationContextKey is the context key for impersonation context.
type impersonationContextKey struct{}

// ImpersonationContext contains information about an active impersonation session.
// This is used to propagate impersonation context through request handling.
type ImpersonationContext struct {
	// SessionID is the impersonation session identifier.
	SessionID string

	// AdminUserID is the admin performing the impersonation.
	AdminUserID uint64

	// AdminUsername is the admin's username.
	AdminUsername string

	// TargetUserID is the user being impersonated.
	TargetUserID uint64

	// TargetUsername is the target user's username.
	TargetUsername string

	// TenantID is the tenant context.
	TenantID string

	// Reason is the justification for impersonation.
	Reason string
}

// WithImpersonationContext returns a new context with the impersonation context attached.
func WithImpersonationContext(ctx context.Context, impCtx *ImpersonationContext) context.Context {
	return context.WithValue(ctx, impersonationContextKey{}, impCtx)
}

// GetImpersonationContext retrieves the impersonation context from the given context.
// Returns nil if no impersonation context is present.
func GetImpersonationContext(ctx context.Context) *ImpersonationContext {
	impCtx, ok := ctx.Value(impersonationContextKey{}).(*ImpersonationContext)
	if !ok {
		return nil
	}
	return impCtx
}

// IsImpersonating returns true if the context contains an impersonation session.
func IsImpersonating(ctx context.Context) bool {
	return GetImpersonationContext(ctx) != nil
}

// ImpersonationContextFromSession creates an ImpersonationContext from an ImpersonationSession.
func ImpersonationContextFromSession(session *ImpersonationSession) *ImpersonationContext {
	if session == nil {
		return nil
	}
	return &ImpersonationContext{
		SessionID:      session.ID,
		AdminUserID:    session.AdminUserID,
		AdminUsername:  session.AdminUsername,
		TargetUserID:   session.TargetUserID,
		TargetUsername: session.TargetUsername,
		TenantID:       session.TenantID,
		Reason:         session.Reason,
	}
}

// BuildImpersonatedActor creates an AuditActor that properly reflects impersonation.
// The actor appears as the impersonated user but includes the impersonating admin's info.
func BuildImpersonatedActor(impCtx *ImpersonationContext) *AuditActor {
	if impCtx == nil {
		return nil
	}
	return &AuditActor{
		UserID:                strconv.FormatUint(impCtx.TargetUserID, 10),
		Username:              impCtx.TargetUsername,
		ImpersonatingUserID:   strconv.FormatUint(impCtx.AdminUserID, 10),
		ImpersonatingUsername: impCtx.AdminUsername,
	}
}

// BuildAuditEventForImpersonatedOperation creates an AuditEvent with proper impersonation context.
// This is a convenience function to ensure consistent audit event creation during impersonation.
// It returns nil, nil for a nil context, and *AuditLogIDGenerationError when
// no event ID can be minted, in which case no event exists to record.
func BuildAuditEventForImpersonatedOperation(
	impCtx *ImpersonationContext,
	action string,
	eventType AuditEventType,
	category AuditCategory,
	outcome AuditOutcome,
	resource *AuditResource,
	details map[string]interface{},
) (*AuditEvent, error) {
	if impCtx == nil {
		return nil, nil
	}

	auditID, err := generateAuditLogID()
	if err != nil {
		return nil, err
	}

	// Add impersonation details to the event
	eventDetails := map[string]interface{}{
		"impersonated_user_id":  strconv.FormatUint(impCtx.TargetUserID, 10),
		"impersonated_username": impCtx.TargetUsername,
		"impersonation_reason":  impCtx.Reason,
	}
	for k, v := range details {
		eventDetails[k] = v
	}

	return &AuditEvent{
		ID:                     auditID,
		TenantID:               impCtx.TenantID,
		Action:                 action,
		EventType:              eventType,
		Category:               category,
		Outcome:                outcome,
		ImpersonationSessionID: impCtx.SessionID,
		CorrelationID:          impCtx.SessionID,
		Actor:                  BuildImpersonatedActor(impCtx),
		Resource:               resource,
		Details:                eventDetails,
	}, nil
}
