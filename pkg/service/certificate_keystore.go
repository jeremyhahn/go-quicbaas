package service

import (
	"context"
	"crypto/x509"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// KeyStoreWriteRefusedError is returned when a caller other than the system
// tenant writes a certificate straight into the go-xkms key store. The key
// store records no tenant: a tenant's certificates come from the certificate
// service's issuance path instead.
type KeyStoreWriteRefusedError struct {
	Op             string
	CallerTenantID string
}

// Error names the refused operation.
func (e *KeyStoreWriteRefusedError) Error() string {
	return "certificate: " + e.Op + ": raw key-store writes are reserved to the system tenant"
}

// Unwrap returns ErrForbidden, so every transport answers with its
// permission-denied status.
func (e *KeyStoreWriteRefusedError) Unwrap() error {
	return ErrForbidden
}

// RequireSystemKeyStoreWriter refuses a raw go-xkms key-store write from any
// caller but the system tenant.
func RequireSystemKeyStoreWriter(ctx context.Context, op string) error {
	callerTenant, err := tenantctx.FromContext(ctx, op)
	if err != nil {
		return &KeyStoreWriteRefusedError{Op: op}
	}
	if !tenantctx.IsSystem(callerTenant) {
		return &KeyStoreWriteRefusedError{Op: op, CallerTenantID: callerTenant}
	}
	return nil
}

// CertificateLookup reads the certificate stored under a go-xkms key ID.
type CertificateLookup func(ctx context.Context, keyID string) (*x509.Certificate, error)

// FilterCertificateIDsForCaller returns the go-xkms key IDs whose certificate
// the caller may reach under BindCertificateToCaller. The system tenant sees
// every ID; a tenant sees only the certificates the CA issued for it; a caller
// without a tenant is refused.
func FilterCertificateIDsForCaller(ctx context.Context, certs CertificateService, keyIDs []string, lookup CertificateLookup) ([]string, error) {
	callerTenant, err := tenantctx.FromContext(ctx, "certificate.filter_issuing_tenant")
	if err != nil {
		return nil, &CertificateTenantError{Cause: err}
	}
	if tenantctx.IsSystem(callerTenant) {
		return keyIDs, nil
	}

	visible := make([]string, 0, len(keyIDs))
	for _, keyID := range keyIDs {
		// An ID without a readable certificate, or with a certificate issued
		// for another tenant, is not the caller's to see.
		cert, lookupErr := lookup(ctx, keyID)
		if lookupErr != nil {
			continue
		}
		if BindCertificateToCaller(ctx, certs, cert) == nil {
			visible = append(visible, keyID)
		}
	}
	return visible, nil
}
