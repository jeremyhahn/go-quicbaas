package server

import (
	"context"
	"crypto/x509"

	xkmscaprovider "github.com/jeremyhahn/go-xkms/pkg/ca/provider"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantBoundXKMSCA is the node CA as the composed XKMS service sees it.
// go-xkms's canonical SignCSR and IssueCertificate (gRPC, REST and MCP) carry
// no tenant in their requests, so they sign or issue with tenantID "" and the
// certificate would be recorded as system-owned. The wrapper supplies the
// authenticated caller's tenant from the request context to both, the same
// source the quicbaas CertificateService uses, so the tenant can revoke what
// it had signed or issued (a signed CSR also gets the tenant SPIFFE SAN).
// Every other method is the node CA's own.
type tenantBoundXKMSCA struct {
	xkmscaprovider.CA
	trustDomain string
}

// tenantBoundXKMSTCGCA keeps go-xkms's TCG assertion on the CA true when the
// node CA also implements the TCG operations.
type tenantBoundXKMSTCGCA struct {
	xkmscaprovider.TCGCA
	bound tenantBoundXKMSCA
}

// newTenantBoundXKMSCA wraps pc so a CSR signed or a certificate issued with
// no tenant is recorded for the authenticated caller's tenant. trustDomain is the operator's SPIFFE
// trust domain, used when the request names none.
func newTenantBoundXKMSCA(pc xkmscaprovider.CA, trustDomain string) xkmscaprovider.CA {
	bound := tenantBoundXKMSCA{CA: pc, trustDomain: trustDomain}
	if tcg, ok := pc.(xkmscaprovider.TCGCA); ok {
		return &tenantBoundXKMSTCGCA{TCGCA: tcg, bound: bound}
	}
	return &bound
}

// Unwrap returns the node CA the wrapper was built around.
func (c *tenantBoundXKMSCA) Unwrap() xkmscaprovider.CA { return c.CA }

// SignCSRWithSPIFFE signs for the authenticated caller's tenant when the
// request names no tenant. A request that names a tenant (already checked by
// go-xkms against the caller), a system caller and a context with no tenant
// bound are passed through unchanged.
func (c *tenantBoundXKMSCA) SignCSRWithSPIFFE(
	ctx context.Context,
	csrPEM []byte,
	profile string,
	validityDays int,
	trustDomain, tenantID, kind, name string,
) (*x509.Certificate, error) {
	if tenantID == "" {
		if caller, ok := callerTenant(ctx, "xkms.sign_csr"); ok {
			tenantID = caller
			if trustDomain == "" {
				trustDomain = c.trustDomain
			}
			if kind == "" {
				kind = service.SPIFFEKindForProfile(profile)
			}
		}
	}
	return c.CA.SignCSRWithSPIFFE(ctx, csrPEM, profile, validityDays, trustDomain, tenantID, kind, name)
}

// IssueCertificateRaw issues for the authenticated caller's tenant when the
// request names no tenant, under the same rule as SignCSRWithSPIFFE. A request
// that names a tenant (already checked by go-xkms against the caller), a
// system caller and a context with no tenant bound are passed through
// unchanged. No SPIFFE SAN is added: this path takes no trust domain, and the
// quicbaas CertificateService's IssueCertificate adds none either.
func (c *tenantBoundXKMSCA) IssueCertificateRaw(
	ctx context.Context,
	commonName, organization string,
	sans []string,
	validityDays int,
	profile, algorithm, tenantID string,
) (certPEM, chainPEM, keyPEM []byte, serialHex string, err error) {
	if tenantID == "" {
		if caller, ok := callerTenant(ctx, "xkms.issue_certificate"); ok {
			tenantID = caller
		}
	}
	return c.CA.IssueCertificateRaw(ctx, commonName, organization, sans, validityDays, profile, algorithm, tenantID)
}

// callerTenant returns the authenticated caller's tenant, and true only when
// ctx binds a tenant that is not the system scope. op names the operation for
// tenantctx's error.
func callerTenant(ctx context.Context, op string) (string, bool) {
	caller, err := tenantctx.FromContext(ctx, op)
	if err != nil || tenantctx.IsSystem(caller) {
		return "", false
	}
	return caller, true
}

// Unwrap returns the node CA the wrapper was built around.
func (c *tenantBoundXKMSTCGCA) Unwrap() xkmscaprovider.CA { return c.TCGCA }

// SignCSRWithSPIFFE is tenantBoundXKMSCA.SignCSRWithSPIFFE.
func (c *tenantBoundXKMSTCGCA) SignCSRWithSPIFFE(
	ctx context.Context,
	csrPEM []byte,
	profile string,
	validityDays int,
	trustDomain, tenantID, kind, name string,
) (*x509.Certificate, error) {
	return c.bound.SignCSRWithSPIFFE(ctx, csrPEM, profile, validityDays, trustDomain, tenantID, kind, name)
}

// IssueCertificateRaw is tenantBoundXKMSCA.IssueCertificateRaw.
func (c *tenantBoundXKMSTCGCA) IssueCertificateRaw(
	ctx context.Context,
	commonName, organization string,
	sans []string,
	validityDays int,
	profile, algorithm, tenantID string,
) (certPEM, chainPEM, keyPEM []byte, serialHex string, err error) {
	return c.bound.IssueCertificateRaw(ctx, commonName, organization, sans, validityDays, profile, algorithm, tenantID)
}
