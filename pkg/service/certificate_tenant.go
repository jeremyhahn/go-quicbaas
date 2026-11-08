package service

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"math/big"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// CertificateTenantResolver is the go-xkms CA capability that reads back the
// tenant recorded on a certificate at issuance (go-xkms ca.CA.CertificateTenant).
type CertificateTenantResolver interface {
	CertificateTenant(ctx context.Context, serial *big.Int) (string, error)
}

// CertificateTenantError reports that a caller asked to act on a certificate
// that was not issued for its tenant, or that the caller's own tenant is
// unknown. Like go-xkms's TenantBindingError it looks exactly like a missing
// tenant: it unwraps to qrdbsdk.ErrTenantMissing and renders the same message,
// so every transport answers not-found and a probe cannot tell a foreign
// serial from an unknown one. The fields are for server-side diagnostics
// (errors.As) only.
type CertificateTenantError struct {
	CallerTenantID  string
	IssuingTenantID string
	Cause           error
}

// Error renders the message of qrdbsdk.ErrTenantMissing and nothing else.
func (e *CertificateTenantError) Error() string {
	return qrdbsdk.ErrTenantMissing.Error()
}

// Unwrap returns qrdbsdk.ErrTenantMissing. The cause is deliberately not
// exposed through the error chain, so it cannot change the transport status.
func (e *CertificateTenantError) Unwrap() error {
	return qrdbsdk.ErrTenantMissing
}

// BindToIssuingTenant applies go-xkms XKMSService.RevokeCertificate's tenant
// binding before a CA acts on the certificate with the given serial. The
// caller's tenant is resolved as every service resolves it
// (tenantctx.FromContext: the tenant key, then the qrdb Identity). The system
// tenant may act on any certificate. A tenant may act only on a certificate
// issued for it. Everything else is refused with CertificateTenantError: a
// caller with no tenant, a CA that cannot report the issuing tenant, and a
// serial the CA cannot attribute.
func BindToIssuingTenant(ctx context.Context, ca any, serial *big.Int) error {
	callerTenant, err := tenantctx.FromContext(ctx, "certificate.bind_issuing_tenant")
	if err != nil {
		return &CertificateTenantError{Cause: err}
	}
	if tenantctx.IsSystem(callerTenant) {
		return nil
	}

	refusal := &CertificateTenantError{CallerTenantID: callerTenant}
	resolver, ok := ca.(CertificateTenantResolver)
	if !ok {
		return refusal
	}
	issuingTenant, err := resolver.CertificateTenant(ctx, serial)
	if err != nil {
		refusal.Cause = err
		return refusal
	}
	if issuingTenant != callerTenant {
		refusal.IssuingTenantID = issuingTenant
		return refusal
	}
	return nil
}

// ErrCertificateTenantNotSupported is returned when the CA behind the
// certificate service does not record the tenant it issued a certificate for.
var ErrCertificateTenantNotSupported = &errors.QBError{
	Code: errors.ErrUnimplemented,
	Op:   "CertificateService.CertificateTenant",
	Err:  errCertificateTenantNotSupported,
}

var errCertificateTenantNotSupported = stderrors.New("CA service does not record issuing tenants")

// CertificateTenant makes the certificate service a CertificateTenantResolver
// over its CA, so a transport serving certificates from another store can bind
// the caller to the tenant the CA issued a certificate for.
func (s *certificateService) CertificateTenant(ctx context.Context, serial *big.Int) (string, error) {
	resolver, ok := s.caService.(CertificateTenantResolver)
	if !ok {
		return "", ErrCertificateTenantNotSupported
	}
	return resolver.CertificateTenant(ctx, serial)
}

// BindCertificateToCaller applies BindToIssuingTenant to a certificate a
// transport read from a store that does not record tenants (the go-xkms key
// store, addressed by key ID). The issuing tenant is resolved through certs,
// the CertificateService the transport is wired with; a nil service, or one
// whose CA does not record tenants, leaves only the system tenant through.
func BindCertificateToCaller(ctx context.Context, certs CertificateService, cert *x509.Certificate) error {
	if cert == nil {
		return BindToIssuingTenant(ctx, nil, nil)
	}
	return BindToIssuingTenant(ctx, certs, cert.SerialNumber)
}

// issuedForTenant reports whether the CA attributes serial to tenantID, for
// filtering a listing to the caller's own certificates. A serial the CA
// attributes to no tenant (go-xkms returns an empty tenant and a nil error for
// a missing ownership record), or a CA that records no issuing tenants, is not
// the caller's. A failure to read the CA's ownership records is returned, so a
// listing stops instead of reporting an incomplete inventory as complete.
func issuedForTenant(ctx context.Context, ca any, tenantID string, serial *big.Int, op string) (bool, error) {
	resolver, ok := ca.(CertificateTenantResolver)
	if !ok {
		return false, nil
	}
	issuingTenant, err := resolver.CertificateTenant(ctx, serial)
	if stderrors.Is(err, errCertificateTenantNotSupported) {
		return false, nil
	}
	if err != nil {
		return false, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}
	return issuingTenant != "" && issuingTenant == tenantID, nil
}
