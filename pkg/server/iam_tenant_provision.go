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
	"log/slog"
	"slices"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const tenantProvisionOp = "iam.ProvisionTenant"

// provisionRollbackTimeout bounds the cleanup of a failed provision, which
// runs detached from the request so a cancellation cannot abandon it.
const provisionRollbackTimeout = 30 * time.Second

// tenantProvisioner is the control-plane path for onboarding a tenant when
// public registration is off: a system admin creates the tenant and its first
// admin through qrdb's bootstrap.ProvisionTenant. It composes the same seams
// first-boot uses and adds only the invite that hands the pending admin its
// activation token. It never initializes a tenant barrier and never holds a
// tenant admin's private key.
type tenantProvisioner struct {
	tenants  iam.TenantManager
	identity *iam.IdentityStore
	invites  iam.InviteStore

	// signer returns the CA that signs admin CSRs, or nil when none is up.
	signer func() qrdbsdk.CertSigner

	// factors returns qrdb's user auth-factor servicer, or nil without a node.
	factors func() qrdbsdk.UserAuthFactorServicer

	// certLoginFactors enrolls the issued admin certificate as an mtls login
	// factor; without it the certificate binds but cannot log in.
	certLoginFactors *iam.CertLoginFactorEnroller

	trustDomain string
	logger      *slog.Logger
}

// newTenantProvisioner wires a provisioner over the server's IAM components.
// The CA and the auth-factor servicer are read per call because both come up
// independently of IAM initialization.
func (s *Server) newTenantProvisioner(c *iamComponents) *tenantProvisioner {
	return &tenantProvisioner{
		tenants:  c.tenantManager,
		identity: c.identityStore,
		invites:  c.inviteStore,
		signer: func() qrdbsdk.CertSigner {
			// Return a true nil, not a nil *CAAdapter inside the interface.
			if ca := s.ceremonyCAAdapter(); ca != nil {
				return ca
			}
			return nil
		},
		factors:          s.GetQRDBUserAuthFactorServicer,
		certLoginFactors: c.certLoginFactors,
		trustDomain:      s.config.IAM.SPIFFETrustDomain,
		logger:           slog.Default(),
	}
}

// Provision creates the tenant, its built-in roles and its first admin. In
// cert mode the admin's CSR is signed and its SPKI thumbprint bound; in invite
// mode the admin is created pending and a one-time invite is minted for it.
//
// A failure after qrdb has provisioned is not rolled back: provisioning is
// idempotent, so the caller re-runs it and gets the same admin back.
func (p *tenantProvisioner) Provision(ctx context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	if req == nil {
		return nil, dberrors.New(dberrors.ErrInvalidInput, tenantProvisionOp, "request is required")
	}
	if err := req.Validate(); err != nil {
		return nil, dberrors.Wrap(err, dberrors.ErrInvalidInput, tenantProvisionOp)
	}

	mode := req.Enrollment()
	// users records the admin this call's create committed, the one account
	// a failed call may roll back.
	users := qrdbadapter.WrapUser(p.identity)
	deps, err := p.dependencies(mode, users)
	if err != nil {
		return nil, err
	}

	cfg := qrdbsdk.BootstrapTenantProvisionConfig{
		TenantID:      req.TenantID,
		DisplayName:   req.Name,
		Isolation:     req.IsolationType,
		AdminUsername: req.AdminUsername,
	}
	if mode == service.IAMTenantAdminEnrollInvite {
		cfg.AdminEnrollment = qrdbsdk.BootstrapAdminEnrollDeferred
	} else {
		cfg.AdminEnrollment = qrdbsdk.BootstrapAdminEnrollCert
		cfg.AdminCSRPEM = []byte(req.AdminCSRPEM)
	}

	res, err := qrdbsdk.ProvisionTenant(ctx, cfg, deps)
	if err != nil {
		if mode != service.IAMTenantAdminEnrollInvite {
			return nil, p.compensateFailedProvision(ctx, req.TenantID, users.CreatedUser(), err)
		}
		return nil, &TenantProvisionError{TenantID: req.TenantID, Cause: err}
	}

	// AdminKeyPEM is deliberately not copied. Only the signer is wired, so
	// qrdb cannot take the generate-and-distribute path, but the response
	// must not be able to carry a key even if that ever changes.
	resp := &service.IAMProvisionTenantResponse{
		TenantID:            res.TenantID,
		AdminUserID:         res.AdminUserID,
		AdminUsername:       res.AdminUsername,
		AdminRoleID:         res.AdminRoleID,
		AdminPending:        res.AdminPending,
		AdminCertPEM:        string(res.AdminCertPEM),
		AdminCertThumbprint: res.AdminCertThumbprint,
		Timestamp:           time.Now(),
	}
	if mode != service.IAMTenantAdminEnrollInvite {
		if err := p.finishCertEnrollment(ctx, res.AdminCertPEM, resp); err != nil {
			return nil, err
		}
		return resp, nil
	}

	// Clamped as CreateInvitedUser clamps: an omitted expiry gets the default
	// lifetime rather than whatever the store makes of zero.
	ttl := iam.ClampInviteTTL(time.Duration(req.InviteExpiryHours) * time.Hour)
	token, record, err := p.invites.Create(ctx, res.TenantID, resp.AdminUserID, ttl)
	if err != nil {
		return nil, &TenantProvisionInviteError{TenantID: res.TenantID, AdminUserID: resp.AdminUserID, Cause: err}
	}
	resp.InviteToken = token
	expiresAt := record.ExpiresAt
	resp.InviteExpiresAt = &expiresAt
	return resp, nil
}

// enrollAdminCert enrolls the certificate qrdb issued for the tenant admin as
// that admin's mtls login factor, the same enrollment first-boot performs, and
// permits mtls in the admin's method policy: the certificate login checks it,
// and the admin was created on the webauthn default (go-quicbaas#1727).
func (p *tenantProvisioner) enrollAdminCert(ctx context.Context, tenantID string, adminUserID uint64, certPEM []byte) error {
	cert, err := parseCertLoginPEM(certPEM)
	if err != nil {
		return err
	}
	if err := enrollCertLoginFactor(ctx, p.certLoginFactors, tenantID, adminUserID, cert, adminCertBindingSourceTenantProvision); err != nil {
		return err
	}
	_, err = p.identity.PermitCertificateLogin(ctx, tenantID, adminUserID)
	return err
}

// finishCertEnrollment makes the cert-mode admin able to log in. The binding
// alone does not: the login resolver looks the certificate up as an mtls auth
// factor. A freshly issued certificate is enrolled as that factor; if that
// fails, the certificate is withheld and the admin the call created is
// deleted, so the caller can simply retry. qrdb issues a certificate only for
// an admin its own run created, so resp names that admin. A re-run issues
// nothing and instead confirms the admin it found in place.
func (p *tenantProvisioner) finishCertEnrollment(ctx context.Context, certPEM []byte, resp *service.IAMProvisionTenantResponse) error {
	if len(certPEM) == 0 {
		return p.confirmExistingAdmin(ctx, resp)
	}
	err := p.enrollAdminCert(ctx, resp.TenantID, resp.AdminUserID, certPEM)
	if err == nil {
		return nil
	}
	if rollbackErr := p.rollback(ctx, resp.TenantID, resp.AdminUserID); rollbackErr != nil {
		return &TenantProvisionRollbackError{TenantID: resp.TenantID, AdminUserID: resp.AdminUserID, Cause: err, RollbackErr: rollbackErr}
	}
	return &TenantProvisionLoginFactorError{TenantID: resp.TenantID, AdminUserID: resp.AdminUserID, Cause: err}
}

// rollback deletes the admin a failed cert-mode call created, keyed by the id
// the call's own create returned; its certificate binding and any factor go
// with it. The tenant is never deleted. qrdb retires a deleted tenant id for
// good, so deleting it would refuse every retry, and the tenant may already
// hold what a concurrent provision of the same id put there.
//
// It runs detached from the request's cancellation, bounded by
// provisionRollbackTimeout: a request canceled mid-provision would otherwise
// fail its own cleanup and strand an admin no re-run can log in as.
func (p *tenantProvisioner) rollback(ctx context.Context, tenantID string, adminUserID uint64) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionRollbackTimeout)
	defer cancel()
	return p.identity.DeleteUser(cleanupCtx, tenantID, adminUserID)
}

// confirmExistingAdmin fills in the admin a re-run found in place, which qrdb
// resolves only in deferred mode, and refuses one holding no active login
// factor. Such an admin is what a failed rollback leaves behind, and
// reporting it as provisioned would hand back a tenant nobody can log in to.
func (p *tenantProvisioner) confirmExistingAdmin(ctx context.Context, resp *service.IAMProvisionTenantResponse) error {
	admin, err := p.identity.GetUserByUsername(ctx, resp.TenantID, resp.AdminUsername)
	if err != nil {
		return &TenantProvisionError{TenantID: resp.TenantID, Cause: err}
	}
	resp.AdminUserID = admin.ID
	// The username alone proves nothing: an ordinary or a disabled account
	// can hold it, and neither is the administrator this call promises.
	if !admin.CanAuthenticate() || !slices.Contains(admin.Roles, resp.AdminRoleID) {
		return &TenantAdminAccountMismatchError{
			TenantID:      resp.TenantID,
			AdminUserID:   admin.ID,
			AdminUsername: resp.AdminUsername,
			Lifecycle:     string(admin.Lifecycle),
			HasAdminRole:  slices.Contains(admin.Roles, resp.AdminRoleID),
		}
	}

	checker, ok := p.factors().(qrdbsdk.ActiveFactorChecker)
	if !ok {
		return provisionSeamUnavailable("fail-closed auth-factor checker")
	}
	active, err := checker.HasActiveFactor(qrdbsdk.ContextWithTenant(ctx, resp.TenantID), admin.ID)
	if err != nil {
		return &TenantProvisionError{TenantID: resp.TenantID, Cause: err}
	}
	if !active {
		return &TenantAdminLoginCredentialMissingError{TenantID: resp.TenantID, AdminUserID: admin.ID, AdminUsername: resp.AdminUsername}
	}
	return nil
}

// compensateFailedProvision deletes the admin a failed cert-mode call created.
// qrdb signs and binds the admin certificate only after it has created the
// admin, so a signer or binder failure leaves an admin with no certificate,
// and a retry would take that admin as already provisioned. created is the
// admin this call's create committed, nil when qrdb failed before creating
// one or found one in place; only that admin is deleted, never one a
// concurrent call created under the same username.
func (p *tenantProvisioner) compensateFailedProvision(ctx context.Context, tenantID string, created *iam.User, cause error) error {
	if created == nil {
		return &TenantProvisionError{TenantID: tenantID, Cause: cause}
	}
	if rollbackErr := p.rollback(ctx, tenantID, created.ID); rollbackErr != nil {
		return &TenantProvisionRollbackError{TenantID: tenantID, AdminUserID: created.ID, Cause: cause, RollbackErr: rollbackErr}
	}
	return &TenantProvisionError{TenantID: tenantID, Cause: cause}
}

// dependencies assembles qrdb's bootstrap seams for one provisioning run,
// refusing before any write when a seam the mode needs is missing.
func (p *tenantProvisioner) dependencies(mode service.IAMTenantAdminEnrollment, users *qrdbadapter.UserAdapter) (qrdbsdk.BootstrapDependencies, error) {
	if p.tenants == nil || p.identity == nil {
		return qrdbsdk.BootstrapDependencies{}, provisionSeamUnavailable("identity store")
	}

	roles := qrdbadapter.WrapRole(p.identity)
	deps := qrdbsdk.BootstrapDependencies{
		Tenants: qrdbadapter.WrapTenant(p.tenants),
		Roles:   roles,
		// Wired explicitly, as in first-boot: a re-run must bind the admin to
		// the identifiers its roles and user are actually persisted under.
		RoleResolver: roles,
		Users:        users,
		UserResolver: users,
		Binder:       users,
		Logger:       p.logger,
	}

	if mode == service.IAMTenantAdminEnrollInvite {
		if p.invites == nil {
			return deps, provisionSeamUnavailable("invite store")
		}
		// qrdb requires a fail-closed factor query before it hands back a
		// pending admin. A servicer that cannot answer it is refused rather
		// than passed as nil.
		checker, ok := p.factors().(qrdbsdk.ActiveFactorChecker)
		if !ok {
			return deps, provisionSeamUnavailable("fail-closed auth-factor checker")
		}
		deps.AdminFactors = checker
		return deps, nil
	}

	// Cert mode signs the admin's CSR. CA is never set, so qrdb cannot fall
	// back to generating a key on the admin's behalf.
	signer := p.signer()
	if signer == nil {
		return deps, provisionSeamUnavailable("certificate signer")
	}
	// Without a trust domain the certificate carries no tenant SPIFFE SAN and
	// login cannot resolve its tenant; see firstBootDependencies.
	if p.trustDomain == "" {
		return deps, dberrors.Wrap(ErrNoSPIFFETrustDomain, dberrors.ErrUnavailable, tenantProvisionOp)
	}
	// Checked before any write: a certificate that cannot be enrolled as a
	// login factor is not worth issuing.
	if p.certLoginFactors == nil {
		return deps, provisionSeamUnavailable("certificate login factor enroller")
	}
	deps.Signer = signer
	// qrdb names the admin's SPIFFE ID under it and reads a bring-your-own
	// admin certificate exactly as login will.
	deps.TrustDomain = p.trustDomain
	return deps, nil
}

// unavailable classifies a missing seam as ErrUnavailable.
func provisionSeamUnavailable(seam string) error {
	return dberrors.Wrap(&TenantProvisionUnavailableError{Seam: seam}, dberrors.ErrUnavailable, tenantProvisionOp)
}
