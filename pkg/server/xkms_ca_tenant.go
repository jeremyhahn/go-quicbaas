package server

import (
	"context"
	"crypto/x509"

	xkmscaprovider "github.com/jeremyhahn/go-xkms/pkg/ca/provider"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantBoundXKMSCA is the node CA as the composed XKMS service sees it.
// go-xkms's canonical SignCSR (gRPC, REST and MCP) carries no tenant in its
// request, so it signs with tenantID "" and the certificate would be recorded
// as system-owned. The wrapper supplies the authenticated caller's tenant
// from the request context, the same source the quicbaas CertificateService
// uses, so the tenant SPIFFE SAN is stamped and the tenant can revoke what it
// had signed. Every other method is the node CA's own.
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

// newTenantBoundXKMSCA wraps pc so a CSR signed with no tenant is recorded for
// the authenticated caller's tenant. trustDomain is the operator's SPIFFE
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
		caller, err := tenantctx.FromContext(ctx, "xkms.sign_csr")
		if err == nil && !tenantctx.IsSystem(caller) {
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
