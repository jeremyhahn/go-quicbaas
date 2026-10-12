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

// auth_admin_factors.go publishes administrator FIDO2 / PIV factor enrollment
// through Security().Auth().AdminFactors().
//
// The orchestrator runs in the CALLER's process. Opening a CTAP2 authenticator
// or a smartcard belongs to the process that owns the operator's console, so
// go-xkms's EnrollAdminFactors executes here, against the handlers the caller
// attaches. The server side crosses the transport on verbs every transport
// carries:
//
//   - piv: the server CA issues the card key's certificate through
//     Security().Certificate().SignCSR, and the factor is saved through
//     Security().Auth().Factors().Add.
//   - fido2: the passkey is created in a server-driven WebAuthn ceremony,
//     Security().Auth().FIDO2().BeginAddCredential then FinishRegistration.
//     The server verifies the attestation, stores the credential the login
//     verifier checks, and writes the factor row itself, so nothing is added
//     through Factors().Add. The ceremony adds the passkey to the
//     AUTHENTICATED CALLER's account, so Subject must name the caller.
//
// A plan that names a factor the caller cannot drive is refused before any
// device is touched or any state changes, instead of silently falling back to
// software crypto.

package quicbaas

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/jeremyhahn/go-xkms/sdk/go/secure"

	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/backend"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/fido2"
)

// AdminEnrollmentResult pairs an enrolled admin factor with the enrollment it
// produced. It is go-qrdb's type, re-exported.
type AdminEnrollmentResult = qrdbsdk.AdminEnrollmentResult

// AdminFactorEnrollRequest selects the factors to enroll for one existing
// administrator and carries the local hardware used to enroll them.
//
// The hardware fields are process-local and never serialised: attach them in
// the process that owns the operator's console.
type AdminFactorEnrollRequest struct {
	// Factors is the --admin-factor selection ("fido2", "piv", "mtls"). It must
	// name at least one factor.
	Factors []string

	// Subject is the administrator's tenant-scoped username. The user must
	// already exist.
	Subject string

	// TenantID scopes Subject. Empty uses the transport's default tenant.
	TenantID string

	// DisplayName is shown by an authenticator's own UI. Falls back to Subject.
	DisplayName string

	// RelyingPartyID and RelyingPartyName scope an enrolled passkey. The ID is
	// required when Factors includes fido2.
	RelyingPartyID   string
	RelyingPartyName string

	// PasskeyDevice is the device path of the authenticator to enroll. It is
	// required when several authenticators are attached.
	PasskeyDevice string

	// PasskeyPIN is the CTAP2 PIN of the authenticator being enrolled.
	PasskeyPIN *secure.Secret

	// FIDO2Handler drives the attached CTAP2 authenticator. Required when
	// Factors includes fido2: a remote transport cannot open the device.
	// A *fido2.FIDO2Handler satisfies it.
	FIDO2Handler AdminPasskeyAuthenticator

	// PIVGenerator produces keys in the attached smartcard. Required when
	// Factors includes piv. The caller owns it so the enrolled key stays
	// reachable for login; Enroll never creates a generator of its own.
	PIVGenerator backend.PIVKeyGenerator

	// PIVCertStore holds the certificates of the card's slots. Required when
	// Factors includes piv: the certificate the server CA issues is installed
	// in the enrolled slot here, because login presents the slot certificate.
	PIVCertStore backend.PIVCertificateStorage

	// AdminCertPEM is the administrator's issued certificate, which the mtls
	// factor binds to.
	AdminCertPEM []byte
}

// AdminPasskeyAuthenticator is the local CTAP2 authenticator a fido2 admin
// factor is created on.
type AdminPasskeyAuthenticator interface {
	// ListDevices enumerates the attached authenticators.
	ListDevices() ([]fido2.Device, error)

	// BindDevice selects the authenticator RegisterWebAuthn runs against.
	BindDevice(path string)

	// RegisterWebAuthn creates a credential for the server's registration
	// options and returns the attestation the server verifies.
	RegisterWebAuthn(opts fido2.WebAuthnRegistrationOptions) (*fido2.WebAuthnAttestation, error)
}

var _ AdminPasskeyAuthenticator = (*fido2.FIDO2Handler)(nil)

// AdminFactorEnroller enrolls an administrator's hardware login factors.
//
// Preflight and Enroll run the device ceremony in THIS process; persistence
// goes to the server over the client's transport.
type AdminFactorEnroller interface {
	// Preflight refuses the selection, with no state changed, when a planned
	// factor cannot succeed: an unparsable plan, an unknown administrator, a
	// fido2 plan with no FIDO2Handler, or a piv plan with no PIVGenerator.
	Preflight(ctx context.Context, req *AdminFactorEnrollRequest) error

	// Enroll runs Preflight, then enrolls each planned factor on the local
	// hardware and persists it. Factors enrolled before a failure are returned
	// alongside the error so the caller can report what is live.
	Enroll(ctx context.Context, req *AdminFactorEnrollRequest) ([]AdminEnrollmentResult, error)
}

// adminFactorEnroller is the transport-backed AdminFactorEnroller.
type adminFactorEnroller struct {
	c       *client
	factors AuthFactorsClient
	fido2   FIDO2Client
}

func newAdminFactorEnroller(c *client, factors AuthFactorsClient, passkeys FIDO2Client) AdminFactorEnroller {
	return &adminFactorEnroller{c: c, factors: factors, fido2: passkeys}
}

// adminEnrollment is what preflight resolved before any device is touched.
type adminEnrollment struct {
	// local is the plan go-xkms enrolls in this process: every planned
	// factor except fido2. It is nil when fido2 is the only factor.
	local *qrdbsdk.AdminEnrollmentPlan

	// passkey reports that fido2 is planned; passkeyPath is the device
	// the passkey is created on.
	passkey     bool
	passkeyPath string

	// userID is the administrator's qrdb user ID.
	userID uint64
}

// plan validates the request and returns the parsed factor plan.
func (e *adminFactorEnroller) plan(op string, req *AdminFactorEnrollRequest) (*qrdbsdk.AdminEnrollmentPlan, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput(op, "request is required")
	}
	if strings.TrimSpace(req.Subject) == "" {
		return nil, sdkerrors.NewInvalidInput(op, "subject is required")
	}
	if len(req.Factors) == 0 {
		return nil, sdkerrors.NewInvalidInput(op, "at least one factor is required")
	}
	plan, err := qrdbsdk.ParseAdminFactors(req.Factors)
	if err != nil {
		return nil, sdkerrors.Wrap(err, sdkerrors.CodeInvalidInput, op, "invalid admin factor selection")
	}
	return plan, nil
}

// config projects the request onto go-xkms's admin-enrollment surface.
// pivKeys is the generator the piv factor runs against; Enroll resolves it so
// the persister can reach the same key afterwards.
func (e *adminFactorEnroller) config(
	plan *qrdbsdk.AdminEnrollmentPlan,
	req *AdminFactorEnrollRequest,
	persister qrdbsdk.AdminFactorPersister,
) xkmssdk.AdminEnrollmentConfig {
	return xkmssdk.AdminEnrollmentConfig{
		Plan:             plan,
		Username:         req.Subject,
		DisplayName:      req.DisplayName,
		TenantID:         req.TenantID,
		RelyingPartyID:   req.RelyingPartyID,
		RelyingPartyName: req.RelyingPartyName,
		PIVGenerator:     req.PIVGenerator,
		AdminCertPEM:     req.AdminCertPEM,
		Persister:        persister,
	}
}

// preflight is the shared refusal gate for Preflight and Enroll.
func (e *adminFactorEnroller) preflight(
	ctx context.Context,
	op string,
	req *AdminFactorEnrollRequest,
) (context.Context, *adminEnrollment, error) {
	if ctx == nil {
		return nil, nil, sdkerrors.NewInvalidInput(op, "context is required")
	}
	plan, err := e.plan(op, req)
	if err != nil {
		return nil, nil, err
	}
	if ctx, err = adminTenantContext(ctx, op, req.TenantID); err != nil {
		return nil, nil, err
	}
	out := &adminEnrollment{passkey: plan.Includes(qrdbsdk.FactorFIDO2)}
	if out.local, err = adminLocalPlan(op, plan); err != nil {
		return nil, nil, err
	}
	if out.passkey {
		if out.passkeyPath, err = adminPasskeyDevice(op, req); err != nil {
			return nil, nil, err
		}
	}
	if out.local != nil && out.local.Includes(qrdbsdk.FactorPIV) {
		if req.PIVGenerator == nil {
			return nil, nil, adminFactorUnavailable(op, qrdbsdk.FactorPIV, "no PIV generator is attached")
		}
		if req.PIVCertStore == nil {
			return nil, nil, adminFactorUnavailable(op, qrdbsdk.FactorPIV, "no PIV certificate store is attached")
		}
	}
	if err := xkmssdk.PreflightAdminFactors(ctx, e.config(out.local, req, nil)); err != nil {
		return nil, nil, sdkerrors.Wrap(err, sdkerrors.CodePreconditionFailed, op,
			"preflight failed (no state was changed)")
	}
	// Resolve the administrator before a device is touched, so an unknown
	// subject costs no ceremony.
	qrdb, err := e.c.qrdbClient()
	if err != nil {
		return nil, nil, err
	}
	if out.userID, err = resolveSubjectUserID(ctx, qrdb, req.TenantID, req.Subject); err != nil {
		return nil, nil, err
	}
	return ctx, out, nil
}

// adminTenantContext binds the requested tenant to ctx, so every call the
// enrollment makes (PIV certificate issuance, factor storage, the passkey
// ceremony) lands in that tenant. A ctx already bound to a different tenant
// is refused rather than overridden.
func adminTenantContext(ctx context.Context, op, tenantID string) (context.Context, error) {
	if tenantID == "" {
		return ctx, nil
	}
	switch bound := transport.TenantFromContext(ctx); bound {
	case "":
		return transport.ContextWithTenant(ctx, tenantID), nil
	case tenantID:
		return ctx, nil
	default:
		return nil, sdkerrors.NewConflict(op,
			"the context is bound to tenant "+bound+", not the requested tenant "+tenantID)
	}
}

// adminLocalPlan is plan without fido2, which the server-driven passkey
// ceremony enrolls instead of go-xkms. It is nil when nothing else remains.
func adminLocalPlan(op string, plan *qrdbsdk.AdminEnrollmentPlan) (*qrdbsdk.AdminEnrollmentPlan, error) {
	var rest []qrdbsdk.AdminFactor
	for _, f := range plan.Factors() {
		if f != qrdbsdk.FactorFIDO2 {
			rest = append(rest, f)
		}
	}
	if len(rest) == 0 {
		return nil, nil
	}
	local, err := qrdbsdk.NewAdminEnrollmentPlan(rest)
	if err != nil {
		return nil, sdkerrors.Wrap(err, sdkerrors.CodeInvalidInput, op, "invalid admin factor selection")
	}
	return local, nil
}

// adminPasskeyDevice picks the authenticator the fido2 factor is created on:
// the attached device whose path is PasskeyDevice, or the only attached
// device when none is named. Anything else is refused rather than guessed.
func adminPasskeyDevice(op string, req *AdminFactorEnrollRequest) (string, error) {
	if h, ok := req.FIDO2Handler.(*fido2.FIDO2Handler); req.FIDO2Handler == nil || (ok && h == nil) {
		return "", adminPasskeyUnavailable(op,
			"no CTAP2 handler in this process; enroll from a console that can open the authenticator")
	}
	if strings.TrimSpace(req.RelyingPartyID) == "" {
		return "", sdkerrors.NewInvalidInput(op, "fido2: relying party ID is required")
	}
	devices, err := req.FIDO2Handler.ListDevices()
	if err != nil {
		return "", sdkerrors.Wrap(err, sdkerrors.CodePreconditionFailed, op,
			"fido2: cannot enumerate authenticators (no state was changed)")
	}
	if len(devices) == 0 {
		return "", adminPasskeyUnavailable(op, "no FIDO2 authenticator is attached")
	}
	if req.PasskeyDevice == "" {
		if len(devices) > 1 {
			return "", adminPasskeyUnavailable(op,
				"several FIDO2 authenticators are attached; name one by device path")
		}
		return devices[0].Path, nil
	}
	for _, d := range devices {
		if d.Path == req.PasskeyDevice {
			return d.Path, nil
		}
	}
	return "", adminPasskeyUnavailable(op, "no FIDO2 authenticator is attached at "+req.PasskeyDevice)
}

func adminPasskeyUnavailable(op, reason string) error {
	return adminFactorUnavailable(op, qrdbsdk.FactorFIDO2, reason)
}

// adminFactorUnavailable refuses a planned factor before any state changes.
func adminFactorUnavailable(op string, factor qrdbsdk.AdminFactor, reason string) error {
	return sdkerrors.Wrap(&qrdbsdk.AdminFactorUnavailableError{
		Factor: factor,
		Reason: reason,
	}, sdkerrors.CodePreconditionFailed, op, "preflight failed (no state was changed)")
}

func (e *adminFactorEnroller) Preflight(ctx context.Context, req *AdminFactorEnrollRequest) error {
	_, _, err := e.preflight(ctx, "adminfactors.Preflight", req)
	return err
}

// Enroll enrolls the go-xkms factors first, then the fido2 passkey. Results
// are in that order.
func (e *adminFactorEnroller) Enroll(ctx context.Context, req *AdminFactorEnrollRequest) ([]AdminEnrollmentResult, error) {
	const op = "adminfactors.Enroll"
	ctx, pre, err := e.preflight(ctx, op, req)
	if err != nil {
		return nil, err
	}
	var enrolled []AdminEnrollmentResult
	if pre.local != nil {
		enrolled, err = xkmssdk.EnrollAdminFactors(ctx, e.config(pre.local, req, &adminFactorPersister{
			factors:  e.factors,
			issuer:   e.c.certificate(),
			pivKeys:  req.PIVGenerator,
			pivCerts: req.PIVCertStore,
			subject:  req.Subject,
			tenantID: req.TenantID,
		}))
		if err != nil {
			return enrolled, sdkerrors.Wrap(err, sdkerrors.CodeInternal, op, "admin factor enrollment failed")
		}
	}
	if pre.passkey {
		passkey, err := e.enrollPasskey(ctx, op, req, pre)
		if err != nil {
			return enrolled, err
		}
		enrolled = append(enrolled, passkey)
	}
	return enrolled, nil
}

// enrollPasskey creates the fido2 factor in a server-driven WebAuthn
// add-credential ceremony and reports it only once the server has stored it.
//
// The server verifies the attestation and records both the WebAuthn
// credential the login verifier checks and the qrdb factor row; nothing is
// persisted from this process.
func (e *adminFactorEnroller) enrollPasskey(
	ctx context.Context,
	op string,
	req *AdminFactorEnrollRequest,
	pre *adminEnrollment,
) (AdminEnrollmentResult, error) {
	if e.fido2 == nil {
		return AdminEnrollmentResult{}, sdkerrors.NewUnavailable(op, "fido2: no FIDO2 client on this transport")
	}
	begin, err := e.fido2.BeginAddCredential(ctx)
	if err != nil {
		return AdminEnrollmentResult{}, err
	}
	opts, err := fido2.ParseWebAuthnRegistrationOptions([]byte(begin.OptionsJSON), "https://"+req.RelyingPartyID)
	if err != nil {
		return AdminEnrollmentResult{}, sdkerrors.Wrap(err, sdkerrors.CodeInternal, op,
			"fido2: server registration options are unusable for relying party "+req.RelyingPartyID)
	}
	// The server binds the passkey to the authenticated caller. Refuse before
	// the authenticator is touched when that is not the requested Subject,
	// rather than enroll a passkey on an account nobody asked for.
	if !bytes.Equal(opts.UserID, adminWebAuthnUserHandle(pre.userID)) {
		return AdminEnrollmentResult{}, sdkerrors.NewForbidden(op,
			"fido2: the passkey would be added to the caller's account, not to "+req.Subject)
	}
	opts.PIN = req.PasskeyPIN
	req.FIDO2Handler.BindDevice(pre.passkeyPath)
	attestation, err := req.FIDO2Handler.RegisterWebAuthn(opts)
	if err != nil {
		return AdminEnrollmentResult{}, sdkerrors.Wrap(err, sdkerrors.CodeInternal, op,
			"fido2: the authenticator did not create the passkey")
	}
	finish, err := e.fido2.FinishRegistration(ctx, &FIDO2FinishRegistrationRequest{
		SessionID:       begin.SessionID,
		AttestationJSON: string(attestation.PublicKeyCredentialJSON),
	})
	if err != nil {
		return AdminEnrollmentResult{}, err
	}
	if finish == nil || !finish.Success || finish.CredentialID == "" {
		return AdminEnrollmentResult{}, sdkerrors.NewInternal(op, "fido2: the server did not store the passkey")
	}
	return AdminEnrollmentResult{
		Factor: qrdbsdk.FactorFIDO2,
		Enrollment: &qrdbsdk.AuthFactorEnrollment{
			UserID:     req.Subject,
			FactorName: qrdbsdk.FactorFIDO2.String(),
			TenantID:   req.TenantID,
			Metadata: map[string]string{
				"credential_id": finish.CredentialID,
				"rp_id":         opts.RPID,
			},
			EnrolledAt: time.Now().UTC(),
		},
	}, nil
}

// adminWebAuthnUserHandle is the WebAuthn user handle the server derives from
// a qrdb user ID: the ID as 8 big-endian bytes, as go-xkms's
// webauthn.HandleFromUserID encodes it. go-xkms does not re-export that
// function through its SDK.
func adminWebAuthnUserHandle(userID uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, userID)
}

// adminCertificateIssuer is the part of Security().Certificate() the piv
// factor needs: the server CA signs the card key's CSR.
type adminCertificateIssuer interface {
	SignCSR(ctx context.Context, req *SignCSRRequest) (*SignCSRResponse, error)
}

// adminFactorPersister saves ceremony results through Factors().Add, the same
// persist path every transport carries.
type adminFactorPersister struct {
	factors  AuthFactorsClient
	issuer   adminCertificateIssuer
	pivKeys  backend.PIVKeyGenerator
	pivCerts backend.PIVCertificateStorage
	subject  string
	tenantID string
}

var _ qrdbsdk.AdminFactorPersister = (*adminFactorPersister)(nil)

// PersistAdminFactor records one enrolled factor. mTLS is skipped: first boot
// already binds the administrator's certificate, including its renewal-stable
// SPKI credential.
func (p *adminFactorPersister) PersistAdminFactor(
	ctx context.Context,
	factor qrdbsdk.AdminFactor,
	enrollment *qrdbsdk.AuthFactorEnrollment,
) error {
	const op = "adminfactors.persist"
	if enrollment == nil {
		return sdkerrors.NewInvalidInput(op, factor.String()+": nil enrollment")
	}
	if factor == qrdbsdk.FactorMTLS {
		return nil
	}
	if factor == qrdbsdk.FactorPIV {
		if err := p.issuePIVCertificate(ctx, enrollment); err != nil {
			return err
		}
	}
	params, err := adminFactorMethodParams(factor, enrollment)
	if err != nil {
		return err
	}
	_, err = p.factors.Add(ctx, &AuthFactorEnrollRequest{
		FactorName:   factor.String(),
		Subject:      p.subject,
		TenantID:     p.tenantID,
		MethodParams: params,
	})
	return err
}

// issuePIVCertificate replaces the self-signed certificate go-xkms built at
// enrollment with one the server CA issues for the same card key, and installs
// it in the enrolled slot. The login verifier chains a PIV certificate to the
// server's CA roots and login presents the slot certificate, so a self-signed
// one, or an issued one left out of the slot, could be stored but never used
// to log in.
//
// The issued certificate is written back into enrollment.PublicKey, so both the
// stored factor and the returned result carry it.
func (p *adminFactorPersister) issuePIVCertificate(ctx context.Context, enrollment *qrdbsdk.AuthFactorEnrollment) error {
	const op = "adminfactors.persist"
	if p.pivKeys == nil || p.pivCerts == nil || p.issuer == nil {
		return sdkerrors.NewPreconditionFailed(op, "piv: no key generator, certificate store or certificate issuer attached")
	}
	slot := xkmssdk.PIVSlot(enrollment.Metadata["slot"])
	signer, err := p.pivKeys.GetPIVSigner(slot, enrollment.Metadata["cn"])
	if err != nil {
		return sdkerrors.Wrap(err, sdkerrors.CodePreconditionFailed, op,
			"piv: enrolled key not found in slot "+string(slot))
	}
	csrPEM, err := xkmssdk.CreateCSRWithKey(&xkmssdk.TrustedCACertificateRequest{
		Subject: xkmssdk.TrustedCASubject{CommonName: p.subject},
	}, signer)
	if err != nil {
		return sdkerrors.Wrap(err, sdkerrors.CodeInternal, op, "piv: build certificate request")
	}
	issued, err := p.issuer.SignCSR(ctx, &SignCSRRequest{CSRPEM: string(csrPEM), Usage: "client"})
	if err != nil {
		return sdkerrors.Wrap(err, sdkerrors.CodeInternal, op, "piv: server CA refused the certificate request")
	}
	cert, err := parseAdminFactorCertificate([]byte(issued.CertificatePEM))
	if err != nil {
		return sdkerrors.Wrap(err, sdkerrors.CodeInternal, op, "piv: server CA returned no certificate")
	}
	// The card signs login challenges with its own key, so a certificate for
	// any other key could never verify.
	pub, ok := cert.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(signer.Public()) {
		return sdkerrors.NewInternal(op, "piv: issued certificate does not certify the card key")
	}
	if err := p.pivCerts.Store(slot, cert); err != nil {
		return sdkerrors.Wrap(err, sdkerrors.CodeInternal, op,
			"piv: install the issued certificate in slot "+string(slot))
	}
	enrollment.PublicKey = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	return nil
}

// adminFactorMethodParams maps a piv enrollment onto the MethodParams that
// Factors().Add turns into the credential bytes login looks up. They must equal
// what the login proof presents, or the factor enrolls but can never
// authenticate: SHA-256 of the slot certificate, SHA-256 of its SPKI (the
// renewal-stable key), and the certificate itself for the verifier. piv is the
// only factor persisted this way; fido2 is stored by the server's ceremony.
//
// Attestation metadata is dropped: that namespace belongs to the server, which
// refuses a caller that writes into it.
func adminFactorMethodParams(
	factor qrdbsdk.AdminFactor,
	enrollment *qrdbsdk.AuthFactorEnrollment,
) (map[string]any, error) {
	const op = "adminfactors.persist"
	params := make(map[string]any, len(enrollment.Metadata)+4)
	for k, v := range enrollment.Metadata {
		if !strings.HasPrefix(strings.ToLower(k), qrdbsdk.ReservedMetadataPrefix) {
			params[k] = v
		}
	}
	params["label"] = "admin " + factor.String()

	if factor != qrdbsdk.FactorPIV {
		return nil, sdkerrors.NewInvalidInput(op, "unsupported admin factor: "+factor.String())
	}
	cert, err := parseAdminFactorCertificate(enrollment.PublicKey)
	if err != nil {
		return nil, sdkerrors.Wrap(err, sdkerrors.CodeInvalidInput, op,
			"piv: enrollment key material is not a certificate, so no login key can be derived")
	}
	params["credential_sha256"] = hex.EncodeToString(pkgiam.MTLSCredentialKey(cert))
	params["spki_sha256"] = hex.EncodeToString(pkgiam.MTLSSPKICredentialKey(cert))
	params["cert_pem"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	return params, nil
}

// parseAdminFactorCertificate reads enrollment key material as a certificate,
// accepting PEM or bare DER.
func parseAdminFactorCertificate(material []byte) (*x509.Certificate, error) {
	if block, _ := pem.Decode(material); block != nil {
		return x509.ParseCertificate(block.Bytes)
	}
	return x509.ParseCertificate(material)
}
