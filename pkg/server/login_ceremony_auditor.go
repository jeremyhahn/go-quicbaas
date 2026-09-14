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

package server

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// auditLoggerResolver resolves the per-tenant IAM audit sink. It is satisfied
// by the composed *service.QuicBaaS facade (quicbaas_iam.go GetAuditLogger)
// and is declared here — narrow, at the point of use — so the auditor depends
// on the one method it calls rather than on the whole facade.
type auditLoggerResolver interface {
	// GetAuditLogger returns the IAM audit sink for the calling context.
	GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
}

// loginCeremonyAuditor is the server-side sink for refused login ceremonies.
//
// It writes TWO records for one rejection, and both are load-bearing:
//
//   - A structured WARN log, ALWAYS. This is the record an operator has when
//     the audit plane is not yet composed, when the tenant is unknown, or when
//     the audit store itself is the thing that is broken. go-quicbaas#484 is
//     the case for it: a WebAuthn login that could not succeed produced no
//     line at any level, so four materially different client payloads were
//     indistinguishable from the outside and from the inside.
//   - An AU-2/AU-3 dual-identity audit event, when a tenant is resolvable.
//     This gives the failure path the parity with the success path that the
//     controls require (the success path has audited since it was written).
//
// The tenant is the one field the ceremony cannot always supply: qrdb's
// FinishLoginRequest carries no tenant id (sdk/go/transport/login_types.go),
// and on a rejection there is often no resolved principal to take one from.
// Rather than stamp a guessed tenant onto an audit record — which would make
// the record wrong in exactly the situation it exists to explain — the audit
// event is skipped and the WARN log, which carries every field, stands alone.
type loginCeremonyAuditor struct {
	logger *logger.Logger
	audit  auditLoggerResolver
}

// Compile-time check that the auditor satisfies the facade's seam.
var _ service.LoginCeremonyAuditor = (*loginCeremonyAuditor)(nil)

// newLoginCeremonyAuditor builds the sink for refused login ceremonies. Either
// dependency may be nil; the auditor then writes whichever record it still
// can, and never panics on a path that is already reporting a failure.
func newLoginCeremonyAuditor(log *logger.Logger, audit auditLoggerResolver) *loginCeremonyAuditor {
	return &loginCeremonyAuditor{logger: log, audit: audit}
}

// newLoginCeremonyAuditorFor builds the sink for a composed facade.
//
// service.QuicBaaS does not declare GetAuditLogger — only the concrete
// implementation carries it, which is why every REST handler that audits
// discovers it by assertion (pkg/api/rest/iam_oidc_introspect.go
// auditLogger). The same discovery happens here rather than widening the
// facade interface for one caller. A facade without the capability still gets
// an auditor: it writes the WARN record, which is the half that matters for
// diagnosis.
func newLoginCeremonyAuditorFor(log *logger.Logger, svc service.QuicBaaS) *loginCeremonyAuditor {
	resolver, ok := svc.(auditLoggerResolver)
	if !ok {
		if log != nil {
			log.Warn("login rejections will not reach the audit plane: the composed facade exposes no audit logger")
		}
		return newLoginCeremonyAuditor(log, nil)
	}
	return newLoginCeremonyAuditor(log, resolver)
}

// RecordLoginRejection implements service.LoginCeremonyAuditor.
func (a *loginCeremonyAuditor) RecordLoginRejection(ctx context.Context, rejection service.LoginRejection) {
	if a == nil {
		return
	}
	a.log(rejection)
	a.record(ctx, rejection)
}

// log writes the structured WARN line. It is the unconditional half.
func (a *loginCeremonyAuditor) log(rejection service.LoginRejection) {
	if a.logger == nil {
		return
	}
	a.logger.Warn("login ceremony rejected",
		"stage", string(rejection.Stage),
		"factor_type", rejection.FactorType,
		"session_id", rejection.SessionID,
		"tenant_id", rejection.TenantID,
		"subject", rejection.Subject,
		"factor_id", rejection.FactorID,
		"lookup_key_sha256", rejection.LookupKeyDigest,
		"lookup_key_len", rejection.LookupKeyLen,
		"method_metadata_keys", rejection.MetadataKeys,
		"reason", rejection.Reason,
	)
}

// record writes the AU-2/AU-3 authentication-failure audit event.
func (a *loginCeremonyAuditor) record(ctx context.Context, rejection service.LoginRejection) {
	if a.audit == nil {
		return
	}
	tenantID := rejection.TenantID
	if tenantID == "" {
		tenantID = iam.TenantFromContext(ctx)
	}
	if tenantID == "" {
		// No honest tenant to file the event under; the WARN line above is
		// the record. See the type comment.
		return
	}
	auditLogger, err := a.audit.GetAuditLogger(ctx)
	if err != nil || auditLogger == nil {
		if a.logger != nil && err != nil {
			a.logger.Warn("login rejection audit skipped: audit logger unavailable",
				"tenant_id", tenantID, "error", err.Error())
		}
		return
	}

	identity := iam.DualIdentity{
		Subject:         rejection.Subject,
		TenantID:        tenantID,
		ServiceIdentity: iam.ServiceIdentityFromCert(service.VerifiedPeerCertFromContext(ctx)),
	}
	details := map[string]interface{}{
		"stage":                rejection.Stage,
		"factor_type":          rejection.FactorType,
		"session_id":           rejection.SessionID,
		"factor_id":            rejection.FactorID,
		"lookup_key_sha256":    rejection.LookupKeyDigest,
		"lookup_key_len":       rejection.LookupKeyLen,
		"method_metadata_keys": rejection.MetadataKeys,
		"reason":               rejection.Reason,
	}
	if logErr := iam.LogDualIdentity(ctx, auditLogger, identity,
		iam.AuditEventAuthentication, iam.AuditCategoryAuthentication,
		loginRejectionAction(rejection.Stage), iam.AuditOutcomeFailure, details); logErr != nil {
		if a.logger != nil {
			a.logger.Warn("login rejection audit failed", "error", logErr.Error())
		}
	}
}

// loginRejectionActions maps a ceremony stage onto the audit action name. The
// finish action is the SAME string the success path already records
// (pkg/api/rest/iam_login_ceremony.go), so an operator queries one action and
// sees both outcomes rather than having to know two names.
var loginRejectionActions = map[service.LoginStage]string{
	service.LoginStageBegin:  "iam.login.begin",
	service.LoginStageFinish: "iam.login.finish",
}

// loginRejectionAction returns the audit action name for a ceremony stage. An
// unknown stage still yields a filed event rather than a dropped one.
func loginRejectionAction(stage service.LoginStage) string {
	if action, ok := loginRejectionActions[stage]; ok {
		return action
	}
	return "iam.login"
}
