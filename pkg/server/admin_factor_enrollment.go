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

// admin_factor_enrollment.go enrolls the administrator's HARDWARE login
// factors during first boot, from the operator's --admin-factor selection.
//
// The layering here is the whole point. go-qrdb owns WHICH factors are
// enrolled and in what order (pkg/ceremony/factors.go, which deliberately
// implements no factors: "go-xkms already implements every factor against
// them"). go-xkms owns the CTAP2 and PIV ceremonies and publishes them through
// its SDK. quicbaas composes the two and adds the one thing neither has — a
// durable store to persist the result.
//
// quicbaas therefore reaches the ceremony THROUGH go-xkms, never straight to
// go-qrdb: a ceremony wired directly to qrdb can only ever do software crypto,
// which would silently downgrade an operator who asked for a security key.

package server

import (
	"context"
	"log/slog"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// AdminFactorPlanError reports an invalid --admin-factor selection.
type AdminFactorPlanError struct {
	Cause error
}

// Error implements the error interface.
func (e *AdminFactorPlanError) Error() string {
	return "admin login factors: invalid --admin-factor selection: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AdminFactorPlanError) Unwrap() error { return e.Cause }

// AdminFactorPreflightError reports that a planned factor cannot succeed. It is
// raised BEFORE the ceremony changes state.
type AdminFactorPreflightError struct {
	Cause error
}

// Error implements the error interface.
func (e *AdminFactorPreflightError) Error() string {
	return "admin login factors: preflight failed (no state was changed): " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AdminFactorPreflightError) Unwrap() error { return e.Cause }

// AdminFactorEnrollError reports a failure enrolling a planned factor.
type AdminFactorEnrollError struct {
	Cause error
}

// Error implements the error interface.
func (e *AdminFactorEnrollError) Error() string {
	return "admin login factors: enrollment failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AdminFactorEnrollError) Unwrap() error { return e.Cause }

// adminFactorPlan parses the configured --admin-factor selection. A nil plan
// (no selection) means the mTLS baseline, which first-boot already enrolls.
func (s *Server) adminFactorPlan() (*qrdbsdk.AdminEnrollmentPlan, error) {
	if s == nil || s.config == nil || len(s.config.Auth.AdminFactors) == 0 {
		return nil, nil
	}
	plan, err := qrdbsdk.ParseAdminFactors(s.config.Auth.AdminFactors)
	if err != nil {
		return nil, &AdminFactorPlanError{Cause: err}
	}
	return plan, nil
}

// adminEnrollmentConfig projects the server's configuration onto the go-xkms
// admin-enrollment surface. certPEM is the administrator certificate the
// ceremony just issued; the mtls factor binds to it.
func (s *Server) adminEnrollmentConfig(
	plan *qrdbsdk.AdminEnrollmentPlan,
	userID uint64,
	certPEM []byte,
) xkmssdk.AdminEnrollmentConfig {
	rpID, rpName := s.adminRelyingParty()
	return xkmssdk.AdminEnrollmentConfig{
		Plan:             plan,
		Username:         s.config.Auth.BootstrapAdminUsername,
		TenantID:         qrdbsdk.SystemTenantID,
		RelyingPartyID:   rpID,
		RelyingPartyName: rpName,
		PasskeyDevice:    s.config.Auth.PasskeyDevice,
		AdminCertPEM:     certPEM,
		Persister: newAdminFactorPersister(
			s.GetQRDBUserAuthFactorServicer(), userID, slog.Default(),
		),
		// Hardware enrollment is only available in the console-owning process
		// that attaches CTAP2 / PIV handlers. Preflight refuses nil handlers before
		// first boot mutates state.
	}
}

// adminRelyingParty resolves the WebAuthn relying party a passkey is scoped
// to, reusing the same derivation the FIDO2 factor verifier and the OIDC
// issuer agree on so an enrolled passkey is usable at login.
func (s *Server) adminRelyingParty() (string, string) {
	rpID := s.config.IAM.WebAuthn.RPID
	if rpID == "" {
		if derived, _, err := webauthnDefaultsFromIssuer(issuerURLFromConfig(s.config)); err == nil {
			rpID = derived
		}
	}
	rpName := s.config.IAM.WebAuthn.RPDisplayName
	if rpName == "" {
		rpName = rpID
	}
	return rpID, rpName
}

// PreflightAdminLoginFactors refuses the boot before anything is created when a
// planned factor cannot succeed.
//
// This is the difference between an operator retrying once and an operator
// holding an initialized barrier guarding an administrator who cannot log in.
// It is exported so the daemon can call it before init starts a server.
func (s *Server) PreflightAdminLoginFactors(ctx context.Context) error {
	plan, err := s.adminFactorPlan()
	if err != nil {
		return err
	}
	if plan == nil {
		return nil
	}
	cfg := s.adminEnrollmentConfig(plan, 0, nil)
	if plan.Includes(qrdbsdk.FactorFIDO2) && cfg.FIDO2Handler == nil {
		// TODO(go-xkms): delete this guard once go-xkms refuses the same case in
		// preflight. In v0.49, NewAdminFactorRegistry keeps the browser FIDO2
		// factor when FIDO2Handler is nil, so PreflightAdminFactors reports the
		// planned local hardware enrollment available and enrollment then panics in
		// pkg/fido2.(*FIDO2Handler).ListDevices.
		return &AdminFactorPreflightError{Cause: &qrdbsdk.AdminFactorUnavailableError{
			Factor: qrdbsdk.FactorFIDO2,
			Reason: "no CTAP2 handler in this process; enroll from a console",
		}}
	}
	if err := xkmssdk.PreflightAdminFactors(ctx, cfg); err != nil {
		return &AdminFactorPreflightError{Cause: err}
	}
	return nil
}

// enrollAdminLoginFactors runs the planned factors for the administrator
// first-boot just created and persists each one.
//
// Enrolled factors are logged even on failure so an operator is never left
// guessing which credentials are live.
func (s *Server) enrollAdminLoginFactors(
	ctx context.Context,
	c *iamComponents,
	tenantID, userID string,
	result *qrdbsdk.FirstBootResult,
) error {
	plan, err := s.adminFactorPlan()
	if err != nil {
		return err
	}
	if plan == nil {
		return nil
	}

	// The user id IS qrdb's numeric id, rendered in decimal, so both factor
	// kinds land on one identity by construction rather than by both consulting
	// the same correlation table.
	qrdbUserID, err := iam.ParseUserID(userID)
	if err != nil {
		return &AdminFactorEnrollError{Cause: err}
	}

	var certPEM []byte
	if result != nil {
		certPEM = result.AdminCertPEM
	}

	enrolled, enrollErr := xkmssdk.EnrollAdminFactors(
		ctx, s.adminEnrollmentConfig(plan, qrdbUserID, certPEM),
	)
	for _, e := range enrolled {
		s.logger.Infof("Admin login factor enrolled: %s", e.Factor.String())
	}
	if enrollErr != nil {
		return &AdminFactorEnrollError{Cause: enrollErr}
	}

	names := make([]string, 0, len(enrolled))
	for _, e := range enrolled {
		names = append(names, e.Factor.String())
	}
	if len(names) > 0 {
		s.logger.Infof("Bootstrap admin login factors: %s", strings.Join(names, ", "))
	}
	return nil
}
