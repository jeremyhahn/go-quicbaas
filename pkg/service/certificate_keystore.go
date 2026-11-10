package service

import (
	"context"
	"crypto/x509"
	"errors"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

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

// CertificateLookupError is returned when the key store cannot say whether a
// listed key ID holds a certificate. Only a confirmed not-found is skipped
// while filtering; any other failure (an unreadable store, a cancelled
// context) would otherwise shrink the listing silently.
type CertificateLookupError struct {
	KeyID string
	Cause error
}

// Error names the key ID whose certificate could not be read.
func (e *CertificateLookupError) Error() string {
	return "certificate: look up " + e.KeyID + ": " + e.Cause.Error()
}

// Unwrap returns the lookup failure.
func (e *CertificateLookupError) Unwrap() error {
	return e.Cause
}

// CertificateListError is returned when a go-xkms backend cannot list its
// certificate key IDs. The listing fails as a whole rather than skipping the
// backend, so it never shrinks silently.
type CertificateListError struct {
	Backend string
	Cause   error
}

// Error names the backend whose certificates could not be listed.
func (e *CertificateListError) Error() string {
	return "certificate: list backend " + e.Backend + ": " + e.Cause.Error()
}

// Unwrap returns the listing failure.
func (e *CertificateListError) Unwrap() error {
	return e.Cause
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
		// An ID whose certificate is confirmed absent, or was issued for
		// another tenant, is not the caller's to see. Any other lookup
		// failure is returned rather than read as absence.
		cert, lookupErr := lookup(ctx, keyID)
		if lookupErr != nil {
			if isCertificateNotFound(lookupErr) {
				continue
			}
			return nil, &CertificateLookupError{KeyID: keyID, Cause: lookupErr}
		}
		if cert == nil {
			continue
		}
		issued, err := issuedForTenant(ctx, certs, callerTenant, cert.SerialNumber, "certificate.filter_issuing_tenant")
		if err != nil {
			return nil, err
		}
		if issued {
			visible = append(visible, keyID)
		}
	}
	return visible, nil
}

// BackendCertificateLister lists the key IDs that hold a certificate on one go-xkms backend.
type BackendCertificateLister func(ctx context.Context, backend string) ([]string, error)

// BackendCertificateLookup reads the certificate stored under keyID on one go-xkms backend.
type BackendCertificateLookup func(ctx context.Context, backend, keyID string) (*x509.Certificate, error)

// FilterBackendCertificateIDsForCaller lists each backend's certificate key
// IDs, in the order given, and keeps those the caller may reach under
// FilterCertificateIDsForCaller. A key ID is backend-local: each (backend, ID)
// pair is judged only by the certificate stored on that backend, so an ID
// held on two backends appears once for each backend that shows it to the
// caller.
func FilterBackendCertificateIDsForCaller(ctx context.Context, certs CertificateService, backends []string, list BackendCertificateLister, lookup BackendCertificateLookup) ([]string, error) {
	var visible []string
	for _, name := range backends {
		keyIDs, err := list(ctx, name)
		if err != nil {
			return nil, &CertificateListError{Backend: name, Cause: err}
		}
		onBackend := func(ctx context.Context, keyID string) (*x509.Certificate, error) {
			return lookup(ctx, name, keyID)
		}
		backendVisible, err := FilterCertificateIDsForCaller(ctx, certs, keyIDs, onBackend)
		if err != nil {
			return nil, err
		}
		visible = append(visible, backendVisible...)
	}
	return visible, nil
}

// isCertificateNotFound reports whether a CertificateLookup failed only
// because the key ID holds no certificate. The embedded go-xkms key store
// wraps its storage not-found sentinel; a remote go-xkms client answers with
// its own not-found sentinels and coded errors.
func isCertificateNotFound(err error) bool {
	return errors.Is(err, xkmssdk.ErrStorageNotFound) || xkmssdk.IsNotFound(err)
}
