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

// ca_service_adapter_extensions_test.go exercises the certificate-extension
// capability against a REAL go-xkms Certificate Authority backed by the
// in-memory software keystore — not a mock.
//
// This is deliberate. go-xkms/pkg/acme/challenges/enroll-01/ca_compliance.go
// documents a shipped bug in which a runtime type assertion against a
// locally-declared CA interface silently stopped matching after an upstream
// signature drifted, making TPM IDevID enrollment impossible on every
// deployment — while the package's own tests kept passing because they were
// written against a mock of the unsatisfiable declaration. A mock cannot
// detect that failure mode; a real CA can.

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// Real xkms CA fixture
// =============================================================================

// newRealSoftwareCA builds and initializes a genuine go-xkms TrustedCA over
// in-memory PKCS#8 key storage and an in-memory certificate store. Nothing
// touches the host filesystem and no goroutines outlive the test.
func newRealSoftwareCA(t *testing.T) xkmssdk.TrustedCA {
	t.Helper()
	return xkmsca.New(t, "QuicBaaS Extensions Test Root CA")
}

// TestACMEIssuance_RealCAStampsCallerTenant drives ACME Issue and
// FinalizeOrder through the production adapter onto a real go-xkms CA. The CA
// refuses an issuance with no tenant, so a certificate whose first subject OU
// is the caller's tenant proves the tenant reached SignOptions.TenantID.
func TestACMEIssuance_RealCAStampsCallerTenant(t *testing.T) {
	ca := newRealSoftwareCA(t)
	tcg, ok := ca.(xkmsacme.CACertificateAuthority)
	require.True(t, ok, "software CA must implement the ACME CA interface")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)
	storage, err := xkmsacme.NewACMEStorageFactoryFromKVStore(qrdbsdk.NewMemoryKVStore())
	require.NoError(t, err)
	acme, err := service.NewACMEService(&service.ACMEServiceParams{
		BaseRESTURI:    "https://baas.example.test",
		CA:             tcg,
		StorageFactory: storage,
		IssueCA:        adapter,
	})
	require.NoError(t, err)
	require.NotNil(t, acme)

	tenantCtx := tenantctx.WithTenant(context.Background(), "acme-corp")
	issueCases := map[string]func(ctx context.Context, csrPEM []byte) ([]byte, error){
		"Issue": func(ctx context.Context, csrPEM []byte) ([]byte, error) {
			resp, err := acme.Issue(ctx, &service.ACMEIssueRequest{CSRPEM: csrPEM, ChallengeType: "http-01"})
			if err != nil {
				return nil, err
			}
			return resp.CertificatePEM, nil
		},
		"FinalizeOrder": func(ctx context.Context, csrPEM []byte) ([]byte, error) {
			resp, err := acme.FinalizeOrder(ctx, &service.ACMEFinalizeOrderRequest{CSRPEM: csrPEM})
			if err != nil {
				return nil, err
			}
			return resp.CertificatePEM, nil
		},
	}
	for name, issue := range issueCases {
		t.Run(name+" stamps the tenant", func(t *testing.T) {
			certPEM, err := issue(tenantCtx, makeCSRPEM(t, "leaf-"+name+".example.test"))
			require.NoError(t, err)
			block, _ := pem.Decode(certPEM)
			require.NotNil(t, block)
			cert, err := x509.ParseCertificate(block.Bytes)
			require.NoError(t, err)
			require.NotEmpty(t, cert.Subject.OrganizationalUnit)
			assert.Equal(t, "acme-corp", cert.Subject.OrganizationalUnit[0])
		})
		t.Run(name+" without a tenant is refused", func(t *testing.T) {
			_, err := issue(context.Background(), makeCSRPEM(t, "none-"+name+".example.test"))
			var missing *tenantctx.MissingTenantError
			assert.ErrorAs(t, err, &missing)
		})
	}
}

// newRealAdapter wraps a real software CA in the adapter under test.
func newRealAdapter(t *testing.T) *caServiceAdapter {
	t.Helper()
	adapter, err := newCAServiceAdapter(newRealSoftwareCA(t))
	require.NoError(t, err)
	return adapter
}

// newForeignRootPEM produces a self-signed CA certificate that stands in for
// an external trust anchor to be cross-signed.
func newForeignRootPEM(t *testing.T, commonName string) ([]byte, *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(4242),
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Foreign Org"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert
}

// newPKIXPublicKeyPEM returns a fresh RSA public key in PEM PKIX form. TCG EK
// and AK certificates admit RSA and ECC keys; RSA keeps the fixture aligned
// with the common TPM 2.0 endorsement key.
func newPKIXPublicKeyPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// =============================================================================
// Interface drift guards
// =============================================================================

// TestRealCASatisfiesTCGIssuer is the runtime half of the compile-time
// assertions in ca_service_adapter_extensions.go. The build already proves
// that any xkms TCGCA satisfies tcgIssuer; this proves that the CA the server
// actually constructs IS a TCGCA at runtime, which is precisely what the
// enroll-01 bug got wrong.
func TestRealCASatisfiesTCGIssuer(t *testing.T) {
	ca := newRealSoftwareCA(t)

	_, isTCGCA := ca.(xkmssdk.TCGCA)
	assert.True(t, isTCGCA, "the real xkms CA must satisfy xkmssdk.TCGCA")

	_, isIssuer := ca.(tcgIssuer)
	assert.True(t, isIssuer, "the real xkms CA must satisfy the locally-declared tcgIssuer")
}

// TestAdapterSatisfiesCertificateExtensionsCA proves the adapter resolves as
// the service-layer capability, which is the assertion
// certificateService.extensionsCA performs at runtime.
func TestAdapterSatisfiesCertificateExtensionsCA(t *testing.T) {
	adapter := newRealAdapter(t)

	_, ok := interface{}(adapter).(service.CertificateExtensionsCA)
	assert.True(t, ok, "adapter must satisfy service.CertificateExtensionsCA")
}

// =============================================================================
// CrossSignRoot
// =============================================================================

func TestCrossSignRoot_RealCA(t *testing.T) {
	adapter := newRealAdapter(t)
	foreignPEM, foreignRoot := newForeignRootPEM(t, "Foreign Root CA")

	crossSignedPEM, err := adapter.CrossSignRoot(context.Background(), foreignPEM)
	require.NoError(t, err)
	require.NotEmpty(t, crossSignedPEM)

	block, _ := pem.Decode(crossSignedPEM)
	require.NotNil(t, block, "cross-signed output must be PEM")
	crossSigned, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// The cross-certificate keeps the foreign root's identity and key...
	assert.Equal(t, foreignRoot.Subject.CommonName, crossSigned.Subject.CommonName)
	assert.Equal(t, foreignRoot.RawSubjectPublicKeyInfo, crossSigned.RawSubjectPublicKeyInfo)

	// ...but is issued by, and verifiably signed by, the local CA.
	localCACert, err := adapter.getCA().CACertificate()
	require.NoError(t, err)
	assert.Equal(t, localCACert.Subject.CommonName, crossSigned.Issuer.CommonName)
	require.NoError(t, crossSigned.CheckSignatureFrom(localCACert))

	// It is a CA certificate: that is what makes it a cross-certificate and
	// what justifies the ca:create authorization rather than certs:generate.
	assert.True(t, crossSigned.IsCA, "a cross-signed root must carry CA:TRUE")

	// It never outlives its issuer.
	assert.False(t, crossSigned.NotAfter.After(localCACert.NotAfter),
		"cross-certificate must not outlive the issuing CA")

	// Serials are unique per issuance.
	second, err := adapter.CrossSignRoot(context.Background(), foreignPEM)
	require.NoError(t, err)
	secondBlock, _ := pem.Decode(second)
	require.NotNil(t, secondBlock)
	secondCert, err := x509.ParseCertificate(secondBlock.Bytes)
	require.NoError(t, err)
	assert.NotEqual(t, crossSigned.SerialNumber.String(), secondCert.SerialNumber.String())
}

func TestCrossSignRoot_Errors(t *testing.T) {
	adapter := newRealAdapter(t)
	ctx := context.Background()

	t.Run("empty input", func(t *testing.T) {
		_, err := adapter.CrossSignRoot(ctx, nil)
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidPEM)
	})

	t.Run("not a certificate", func(t *testing.T) {
		_, err := adapter.CrossSignRoot(ctx, []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n"))
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidPEM)
	})

	t.Run("garbage bytes", func(t *testing.T) {
		_, err := adapter.CrossSignRoot(ctx, []byte("not a certificate at all"))
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidPEM)
	})

	t.Run("nil CA", func(t *testing.T) {
		empty := &caServiceAdapter{}
		_, err := empty.CrossSignRoot(ctx, []byte("x"))
		assert.ErrorIs(t, err, ErrCAServiceAdapterNilCA)
	})
}

// =============================================================================
// IssuerChainPEM
// =============================================================================

func TestIssuerChainPEM_RealCA(t *testing.T) {
	adapter := newRealAdapter(t)
	ctx := context.Background()

	// Issue a leaf from the real CA, then resolve its chain.
	leafResp, err := adapter.IssueCertificate(ctx, newLeafIssueRequest("chain-leaf.example.com"))
	require.NoError(t, err)
	require.NotEmpty(t, leafResp.CertificatePEM)

	chain, err := adapter.IssuerChainPEM(ctx, leafResp.CertificatePEM)
	require.NoError(t, err)
	require.Len(t, chain, 1, "a leaf under a single-tier root has exactly one issuer")

	block, _ := pem.Decode(chain[0])
	require.NotNil(t, block)
	issuer, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	caCert, err := adapter.getCA().CACertificate()
	require.NoError(t, err)
	assert.Equal(t, caCert.Subject.CommonName, issuer.Subject.CommonName)
	assert.True(t, issuer.IsCA)
}

func TestIssuerChainPEM_SelfSignedHasEmptyChain(t *testing.T) {
	adapter := newRealAdapter(t)

	caCert, err := adapter.getCA().CACertificate()
	require.NoError(t, err)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})

	chain, err := adapter.IssuerChainPEM(context.Background(), caPEM)
	require.NoError(t, err)
	assert.Empty(t, chain, "a self-signed root has no issuers above it")
}

func TestIssuerChainPEM_Errors(t *testing.T) {
	adapter := newRealAdapter(t)
	ctx := context.Background()

	t.Run("empty input", func(t *testing.T) {
		_, err := adapter.IssuerChainPEM(ctx, nil)
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidPEM)
	})

	t.Run("malformed PEM", func(t *testing.T) {
		_, err := adapter.IssuerChainPEM(ctx, []byte("still not a certificate"))
		assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidPEM)
	})

	t.Run("unknown issuer", func(t *testing.T) {
		// A foreign root's child references an issuer the local store has
		// never seen and whose AIA extension is absent, so resolution must
		// fail rather than return a truncated chain.
		foreign := newForeignSignedLeafPEM(t)
		_, err := adapter.IssuerChainPEM(ctx, foreign)
		assert.ErrorIs(t, err, ErrCAChainIssuerNotFound)
	})

	t.Run("nil CA", func(t *testing.T) {
		empty := &caServiceAdapter{}
		_, err := empty.IssuerChainPEM(ctx, []byte("x"))
		assert.ErrorIs(t, err, ErrCAServiceAdapterNilCA)
	})
}

// newForeignSignedLeafPEM builds a leaf signed by a throwaway CA that the
// adapter's certificate store does not contain.
func newForeignSignedLeafPEM(t *testing.T) []byte {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Nowhere CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "orphan-leaf.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
}

// =============================================================================
// IssueDeviceIdentityCert
// =============================================================================

func TestIssueDeviceIdentityCert_EK_RealCA(t *testing.T) {
	adapter := newRealAdapter(t)

	issued, err := adapter.IssueDeviceIdentityCert(context.Background(), &service.DeviceIdentityIssuance{
		Type:     service.DeviceIdentityTypeEK,
		DeviceID: "device-ek-0001",
		TenantID: "fixtures",
		EKPub:    newPKIXPublicKeyPEM(t),
	})
	require.NoError(t, err)
	require.NotNil(t, issued)
	require.NotEmpty(t, issued.CertificatePEM)
	assert.NotEmpty(t, issued.SerialNumber)

	block, _ := pem.Decode(issued.CertificatePEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	caCert, err := adapter.getCA().CACertificate()
	require.NoError(t, err)
	require.NoError(t, cert.CheckSignatureFrom(caCert),
		"the EK certificate must be signed by the real CA")
	assert.False(t, issued.NotAfter.IsZero())
}

// bundleFailingTCGCA pairs the fakeTrustedCA double, whose CABundle read
// fails, with a real TCG issuer so device identity issuance reaches the
// bundle read.
type bundleFailingTCGCA struct {
	*fakeTrustedCA
	tcgIssuer
}

func TestIssueDeviceIdentityCert_BundleError(t *testing.T) {
	tcg, ok := newRealSoftwareCA(t).(tcgIssuer)
	require.True(t, ok, "the real CA must implement the TCG surface")
	fake := newFakeTrustedCA(t)
	cause := errors.New("bundle unreadable")
	fake.bundleErr = cause
	adapter, err := newCAServiceAdapter(&bundleFailingTCGCA{fakeTrustedCA: fake, tcgIssuer: tcg})
	require.NoError(t, err)

	issued, err := adapter.IssueDeviceIdentityCert(context.Background(), &service.DeviceIdentityIssuance{
		Type:     service.DeviceIdentityTypeEK,
		DeviceID: "device-ek-bundle-error",
		TenantID: "fixtures",
		EKPub:    newPKIXPublicKeyPEM(t),
	})
	assert.Nil(t, issued, "no certificate may be returned when the CA bundle cannot be read")
	requireBundleError(t, err, cause)
}

func TestIssueDeviceIdentityCert_AK_RealCA(t *testing.T) {
	adapter := newRealAdapter(t)

	issued, err := adapter.IssueDeviceIdentityCert(context.Background(), &service.DeviceIdentityIssuance{
		Type:     service.DeviceIdentityTypeAK,
		DeviceID: "device-ak-0001",
		TenantID: "fixtures",
		AKPub:    newPKIXPublicKeyPEM(t),
	})
	require.NoError(t, err)
	require.NotEmpty(t, issued.CertificatePEM)

	block, _ := pem.Decode(issued.CertificatePEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	caCert, err := adapter.getCA().CACertificate()
	require.NoError(t, err)
	require.NoError(t, cert.CheckSignatureFrom(caCert))
}

func TestIssueDeviceIdentityCert_IDevIDRejectsUnverifiableCSR(t *testing.T) {
	adapter := newRealAdapter(t)

	// A TCG-CSR-IDEVID that does not unmarshal must be rejected before any
	// signing occurs. The CA never signs an unverified device CSR.
	_, err := adapter.IssueDeviceIdentityCert(context.Background(), &service.DeviceIdentityIssuance{
		Type:     service.DeviceIdentityTypeIDevID,
		DeviceID: "device-idevid-0001",
		CSR:      []byte("this is not a packed TCG-CSR-IDEVID"),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCAInvalidTCGCSR)
}

func TestIssueDeviceIdentityCert_Errors(t *testing.T) {
	adapter := newRealAdapter(t)
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		_, err := adapter.IssueDeviceIdentityCert(ctx, nil)
		assert.ErrorIs(t, err, ErrCAServiceAdapterNilRequest)
	})

	t.Run("nil CA", func(t *testing.T) {
		empty := &caServiceAdapter{}
		_, err := empty.IssueDeviceIdentityCert(ctx, &service.DeviceIdentityIssuance{
			Type: service.DeviceIdentityTypeEK, DeviceID: "d",
		})
		assert.ErrorIs(t, err, ErrCAServiceAdapterNilCA)
	})

	t.Run("unknown identity type", func(t *testing.T) {
		_, err := adapter.IssueDeviceIdentityCert(ctx, &service.DeviceIdentityIssuance{
			Type: "tpm-quote", DeviceID: "d",
		})
		assert.ErrorIs(t, err, service.ErrCertExtUnknownIdentityType)
	})

	t.Run("malformed EK public key", func(t *testing.T) {
		_, err := adapter.IssueDeviceIdentityCert(ctx, &service.DeviceIdentityIssuance{
			Type: service.DeviceIdentityTypeEK, DeviceID: "d", EKPub: []byte("nope"),
		})
		assert.ErrorIs(t, err, ErrCAInvalidPublicKey)
	})

	t.Run("missing AK public key", func(t *testing.T) {
		_, err := adapter.IssueDeviceIdentityCert(ctx, &service.DeviceIdentityIssuance{
			Type: service.DeviceIdentityTypeAK, DeviceID: "d",
		})
		assert.ErrorIs(t, err, ErrCAInvalidPublicKey)
	})
}

// TestIssueDeviceIdentityCert_NonTCGCAFailsClosed proves the drift path: a CA
// that satisfies TrustedCA but NOT the TCG surface must be refused, not
// silently treated as capable.
func TestIssueDeviceIdentityCert_NonTCGCAFailsClosed(t *testing.T) {
	adapter := &caServiceAdapter{ca: &nonTCGTrustedCA{TrustedCA: newRealSoftwareCA(t)}}

	_, err := adapter.IssueDeviceIdentityCert(context.Background(), &service.DeviceIdentityIssuance{
		Type:     service.DeviceIdentityTypeEK,
		DeviceID: "device-ek-0002",
		EKPub:    newPKIXPublicKeyPEM(t),
	})
	assert.ErrorIs(t, err, ErrCADeviceIdentityUnsupported)
}

// nonTCGTrustedCA wraps a real CA and shadows the three TCG methods with
// signatures that deliberately do NOT match tcgIssuer, modelling exactly the
// signature drift that broke enroll-01 upstream. The wrapper therefore
// satisfies xkmssdk.TrustedCA but not tcgIssuer.
type nonTCGTrustedCA struct {
	xkmssdk.TrustedCA
}

func (c *nonTCGTrustedCA) IssueEKCertificate() error   { return nil }
func (c *nonTCGTrustedCA) IssueAKCertificate() error   { return nil }
func (c *nonTCGTrustedCA) SignTCGCSRIDevID() error     { return nil }
func (c *nonTCGTrustedCA) VerifyTCGCSRIDevID() error   { return nil }
func (c *nonTCGTrustedCA) unusedDriftGuardMarker() int { return 0 }

// =============================================================================
// Helpers
// =============================================================================

// newLeafIssueRequest builds a minimal leaf issuance request for the adapter's
// existing IssueCertificate path, used to obtain a real CA-signed leaf.
func newLeafIssueRequest(commonName string) *xkmstransport.IssueCertificateRequest {
	return &xkmstransport.IssueCertificateRequest{
		TenantID:     "fixtures",
		CommonName:   commonName,
		Organization: "Automate The Things",
		ValidityDays: 30,
		Algorithm:    "ecdsa-p256",
	}
}
