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

package quicbaas

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/backend"
)

// adminTenant is empty because Factors().List resolves its subject without a
// tenant, and the tests count factors through it.
const adminTenant = ""

func newAdminEnrollerFixture(t *testing.T) (AdminFactorEnroller, qrdbsdk.UserAuthFactorServicer, AuthFactorsClient) {
	t.Helper()
	enroller, store, factors, _ := newAdminEnrollerCAFixture(t)
	return enroller, store, factors
}

// newAdminEnrollerCAFixture also routes Security().Certificate() over the
// embedded transport to the server's certificate service, backed by a real
// in-memory go-xkms CA. The CA is returned so a test can take its roots, the
// same roots the server's PIV login verifier trusts.
func newAdminEnrollerCAFixture(t *testing.T) (AdminFactorEnroller, qrdbsdk.UserAuthFactorServicer, AuthFactorsClient, *xkmsca.CAService) {
	t.Helper()
	signer := &caSigningClient{ca: xkmsca.NewCAService(t, t.Name())}
	c, store := buildAdminEnrollerClient(t, adminTenant, signer)
	auth := c.Security().Auth()
	return auth.AdminFactors(), store, auth.Factors(), signer.ca
}

// buildAdminEnrollerClient wires an SDK client whose directory holds the
// administrator "root" (id 9) in tenantID, and whose certificate service
// signs through signer.
func buildAdminEnrollerClient(t *testing.T, tenantID string, signer *caSigningClient) (*client, qrdbsdk.UserAuthFactorServicer) {
	t.Helper()
	dir := newDirectory(testUser{id: 9, tenantID: tenantID, username: "root"})
	c, store := buildLoginFactorRoundtrip(t, dir)
	tr, err := embedded.New(&certificateQuicBaaS{
		cert: pkgservice.NewCertificateService(signer),
	})
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	c.certificateClient = newCertificateClient(tr)
	return c, store
}

// certificateQuicBaaS is a service.QuicBaaS whose only live method is the
// Certificate service the embedded certificate handlers route to.
type certificateQuicBaaS struct {
	service.QuicBaaS
	cert service.CertificateService
}

func (m *certificateQuicBaaS) Certificate() service.CertificateService { return m.cert }

// caSigningClient is the go-xkms client the certificate service signs through.
// Only SignCSR is live, and it runs the real CA.
// It records the tenant each signing request was served for.
type caSigningClient struct {
	xkmstransport.Client
	ca       *xkmsca.CAService
	signedIn []string
}

func (c *caSigningClient) SignCSR(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	tenantID, bound := tenantctx.Lookup(ctx)
	if !bound {
		tenantID = "<unbound>"
	}
	c.signedIn = append(c.signedIn, tenantID)
	return c.ca.SignCSR(ctx, req)
}

// tenantRecordingFactors records the tenant each factor write was sent for.
type tenantRecordingFactors struct {
	AuthFactorsClient
	storedIn []string
}

func (f *tenantRecordingFactors) Add(ctx context.Context, req *AuthFactorEnrollRequest) (*AuthFactorEnrollResponse, error) {
	f.storedIn = append(f.storedIn, transport.TenantFromContext(ctx))
	return f.AuthFactorsClient.Add(ctx, req)
}

// pivLogin signs a login challenge with the card key and runs it through the
// production PIV verifier, trusting only the CA's roots.
func pivLogin(t *testing.T, ca *xkmsca.CAService, factor *qrdbsdk.AuthFactor, card crypto.Signer) error {
	t.Helper()
	roots, err := ca.TrustedCA().CABundleCertPool()
	if err != nil {
		t.Fatalf("CA roots: %v", err)
	}
	verifier, err := xkmssdk.NewPIVHardwareFactorVerifier(roots, nil)
	if err != nil {
		t.Fatalf("NewPIVHardwareFactorVerifier: %v", err)
	}
	nonce := []byte("admin login challenge")
	digest := sha256.Sum256(nonce)
	sig, err := card.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("card sign: %v", err)
	}
	return verifier.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypePIV,
		Nonce:     nonce,
		Signature: sig,
	})
}

// A software PIV plan, explicitly opted into, enrolls through the embedded
// transport and the credential is resolvable by the keys login presents.
func TestAdminFactors_EnrollPIVThroughEmbeddedTransport(t *testing.T) {
	enroller, store, _ := newAdminEnrollerFixture(t)

	results, err := enroller.Enroll(context.Background(), &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     adminTenant,
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
		PIVCertStore: newPIVCertStore(t),
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if len(results) != 1 || results[0].Factor != qrdbsdk.FactorPIV {
		t.Fatalf("results = %+v, want one piv enrollment", results)
	}

	cert, err := parseAdminFactorCertificate(results[0].Enrollment.PublicKey)
	if err != nil {
		t.Fatalf("enrollment key material is not a certificate: %v", err)
	}
	if cert.Issuer.String() == cert.Subject.String() {
		t.Errorf("persisted certificate is self-signed (%s); the login verifier cannot chain it", cert.Subject)
	}
	for name, key := range map[string][]byte{
		"cert digest": pkgiam.MTLSCredentialKey(cert),
		"spki digest": pkgiam.MTLSSPKICredentialKey(cert),
	} {
		got, err := store.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, key)
		if err != nil || got == nil {
			t.Fatalf("login lookup by %s: %v", name, err)
		}
		if got.UserID != 9 {
			t.Errorf("%s resolved user %d, want 9", name, got.UserID)
		}
	}
}

// A PIV enrollment for a named tenant from an unbound context issues the
// certificate and stores the factor in that tenant, not the platform's.
func TestAdminFactors_EnrollPIVBindsRequestedTenant(t *testing.T) {
	const tenantID = "acme"
	signer := &caSigningClient{ca: xkmsca.NewCAService(t, t.Name())}
	c, store := buildAdminEnrollerClient(t, tenantID, signer)
	factors := &tenantRecordingFactors{AuthFactorsClient: c.Security().Auth().Factors()}
	enroller := newAdminFactorEnroller(c, factors, nil)

	results, err := enroller.Enroll(context.Background(), &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     tenantID,
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
		PIVCertStore: newPIVCertStore(t),
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if len(signer.signedIn) != 1 || signer.signedIn[0] != tenantID {
		t.Errorf("certificate issued in %v, want [%s]", signer.signedIn, tenantID)
	}
	if len(factors.storedIn) != 1 || factors.storedIn[0] != tenantID {
		t.Errorf("factor stored in %v, want [%s]", factors.storedIn, tenantID)
	}
	cert, err := parseAdminFactorCertificate(results[0].Enrollment.PublicKey)
	if err != nil {
		t.Fatalf("enrollment key material is not a certificate: %v", err)
	}
	got, err := store.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, pkgiam.MTLSSPKICredentialKey(cert))
	if err != nil || got == nil || got.UserID != 9 {
		t.Fatalf("login lookup = %+v, %v; want user 9", got, err)
	}
}

// A context already bound to another tenant is refused, never overridden,
// before a certificate is issued or a factor stored.
func TestAdminFactors_EnrollRefusesConflictingTenant(t *testing.T) {
	signer := &caSigningClient{ca: xkmsca.NewCAService(t, t.Name())}
	c, _ := buildAdminEnrollerClient(t, "acme", signer)
	factors := &tenantRecordingFactors{AuthFactorsClient: c.Security().Auth().Factors()}
	enroller := newAdminFactorEnroller(c, factors, nil)
	req := &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     "acme",
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
		PIVCertStore: newPIVCertStore(t),
	}
	ctx := transport.ContextWithTenant(context.Background(), "globex")

	if err := enroller.Preflight(ctx, req); sdkerrors.Code(err) != sdkerrors.CodeConflict {
		t.Errorf("Preflight: err = %v, want a conflict", err)
	}
	_, err := enroller.Enroll(ctx, req)
	if sdkerrors.Code(err) != sdkerrors.CodeConflict {
		t.Fatalf("Enroll: err = %v, want a conflict", err)
	}
	if len(signer.signedIn) != 0 || len(factors.storedIn) != 0 {
		t.Fatalf("refused enrollment still signed %v and stored %v", signer.signedIn, factors.storedIn)
	}
}

// TestAdminFactors_EnrolledPIVCompletesLogin is the Codex P1 regression: the
// persisted PIV factor must pass the production login verifier, which chains
// the factor's certificate to the server CA. A self-signed enrollment
// certificate failed that chain check, so the factor could never log in.
// newPIVCertStore returns an in-memory PIV certificate store: the card's slot
// certificates, which login reads.
func newPIVCertStore(t *testing.T) backend.PIVCertificateStorage {
	t.Helper()
	mem, err := xkmssdk.NewMemoryBackend()
	if err != nil {
		t.Fatalf("NewMemoryBackend: %v", err)
	}
	store, err := xkmssdk.NewPIVFileBackend(&xkmssdk.PIVFileStorageConfig{Backend: mem, PEMEnabled: true})
	if err != nil {
		t.Fatalf("NewPIVFileBackend: %v", err)
	}
	return store
}

// Login reads the certificate from the card's slot, finds the factor by its
// digest and proves possession with the slot key. Enrollment must therefore
// leave the issued certificate in the slot, not only in the factor store.
func TestAdminFactors_EnrolledPIVCompletesLogin(t *testing.T) {
	enroller, store, _, ca := newAdminEnrollerCAFixture(t)
	card := xkmssdk.NewSoftwarePIVKeyGenerator()
	slots := newPIVCertStore(t)

	results, err := enroller.Enroll(context.Background(), &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     adminTenant,
		PIVGenerator: card,
		PIVCertStore: slots,
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	enrollment := results[0].Enrollment
	slot := xkmssdk.PIVSlot(enrollment.Metadata["slot"])
	slotCert, err := slots.Retrieve(slot)
	if err != nil {
		t.Fatalf("slot %s holds no certificate after enrollment: %v", slot, err)
	}
	issued, err := parseAdminFactorCertificate(enrollment.PublicKey)
	if err != nil {
		t.Fatalf("enrollment key material is not a certificate: %v", err)
	}
	if !slotCert.Equal(issued) {
		t.Fatalf("slot %s holds %q, want the issued certificate %q", slot, slotCert.Issuer, issued.Issuer)
	}

	factor, err := store.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorPIV, pkgiam.MTLSCredentialKey(slotCert))
	if err != nil || factor == nil {
		t.Fatalf("login lookup by the slot certificate: %v", err)
	}
	signer, err := card.GetPIVSigner(slot, enrollment.Metadata["cn"])
	if err != nil {
		t.Fatalf("card key: %v", err)
	}
	if err := pivLogin(t, ca, factor, signer); err != nil {
		t.Fatalf("enrolled PIV factor failed login: %v", err)
	}

	// A different key holding the same factor must not log in.
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := pivLogin(t, ca, factor, other); err == nil {
		t.Fatal("a key other than the card's passed PIV login")
	}
}

func TestAdminFactors_PIVRefusedWhenCARefuses(t *testing.T) {
	enroller, _, factors, ca := newAdminEnrollerCAFixture(t)
	ca.SignCSRErr = errors.New("ca offline")

	_, err := enroller.Enroll(context.Background(), &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     adminTenant,
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
		PIVCertStore: newPIVCertStore(t),
	})
	if err == nil {
		t.Fatal("Enroll succeeded although the CA refused the certificate")
	}
	if ca.SignCSRCalls != 1 {
		t.Errorf("SignCSR calls = %d, want 1", ca.SignCSRCalls)
	}
	listed, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listed.Total != 0 {
		t.Errorf("a factor was stored without an issued certificate: %d factors", listed.Total)
	}
}

// A fido2 plan with no handler is refused before any state changes.
func TestAdminFactors_FIDO2WithoutHandlerRefused(t *testing.T) {
	enroller, _, factors := newAdminEnrollerFixture(t)

	req := &AdminFactorEnrollRequest{
		Factors:        []string{"fido2"},
		Subject:        "root",
		TenantID:       adminTenant,
		RelyingPartyID: "example.test",
	}
	for name, run := range map[string]func() error{
		"Preflight": func() error { return enroller.Preflight(context.Background(), req) },
		"Enroll": func() error {
			res, err := enroller.Enroll(context.Background(), req)
			if len(res) != 0 {
				t.Errorf("Enroll returned %d enrollments with no handler", len(res))
			}
			return err
		},
	} {
		err := run()
		if err == nil {
			t.Fatalf("%s: want refusal for a fido2 plan with no handler", name)
		}
		if sdkerrors.Code(err) != sdkerrors.CodePreconditionFailed {
			t.Errorf("%s: code = %v, want precondition failed", name, sdkerrors.Code(err))
		}
	}

	list, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 0 {
		t.Errorf("refused enrollment left %d factors", list.Total)
	}
}

// A piv plan with no generator and no software opt-in is refused.
func TestAdminFactors_PIVWithoutGeneratorRefused(t *testing.T) {
	enroller, _, _ := newAdminEnrollerFixture(t)

	err := enroller.Preflight(context.Background(), &AdminFactorEnrollRequest{
		Factors: []string{"piv"}, Subject: "root", TenantID: adminTenant,
	})
	if err == nil {
		t.Fatal("want refusal for a piv plan with no PIVGenerator")
	}
}

// A piv factor is only usable if the caller can reach its private key
// again at login, so Enroll never creates a generator of its own: a piv plan
// without a caller-owned PIVGenerator is refused and nothing is stored.
func TestAdminFactors_EnrollPIVWithoutGeneratorStoresNothing(t *testing.T) {
	enroller, _, factors, ca := newAdminEnrollerCAFixture(t)

	_, err := enroller.Enroll(context.Background(), &AdminFactorEnrollRequest{
		Factors:  []string{"piv"},
		Subject:  "root",
		TenantID: adminTenant,
	})
	var unavailable *qrdbsdk.AdminFactorUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Factor != qrdbsdk.FactorPIV {
		t.Fatalf("Enroll err = %v, want a piv AdminFactorUnavailableError", err)
	}
	if ca.SignCSRCalls != 0 {
		t.Errorf("SignCSR calls = %d, want 0", ca.SignCSRCalls)
	}
	listed, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listed.Total != 0 {
		t.Errorf("a piv factor was stored for a key the caller cannot reach: %d factors", listed.Total)
	}
}

func TestAdminFactors_InvalidRequestsRefused(t *testing.T) {
	enroller, _, _ := newAdminEnrollerFixture(t)

	cases := map[string]*AdminFactorEnrollRequest{
		"nil request":     nil,
		"no subject":      {Factors: []string{"piv"}, TenantID: adminTenant, PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(), PIVCertStore: newPIVCertStore(t)},
		"no factors":      {Subject: "root", TenantID: adminTenant},
		"unknown factor":  {Factors: []string{"password"}, Subject: "root", TenantID: adminTenant},
		"unknown subject": {Factors: []string{"piv"}, Subject: "nobody", TenantID: adminTenant, PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(), PIVCertStore: newPIVCertStore(t)},
	}
	for name, req := range cases {
		if res, err := enroller.Enroll(context.Background(), req); err == nil || len(res) != 0 {
			t.Errorf("%s: Enroll = (%v, %v), want refusal", name, res, err)
		}
	}
	var nilCtx context.Context
	if err := enroller.Preflight(nilCtx, &AdminFactorEnrollRequest{
		Factors: []string{"piv"}, Subject: "root", PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
	}); err == nil {
		t.Error("nil context: want refusal")
	}
}

func TestAdminFactorPersister_RefusesUnusableEnrollments(t *testing.T) {
	_, _, factors := newAdminEnrollerFixture(t)
	p := &adminFactorPersister{factors: factors, subject: "root", tenantID: adminTenant}

	cases := map[string]struct {
		factor     qrdbsdk.AdminFactor
		enrollment *qrdbsdk.AuthFactorEnrollment
	}{
		"nil enrollment": {qrdbsdk.FactorFIDO2, nil},
		// The server's ceremony stores a passkey; a factor row written here
		// would carry no WebAuthn credential and could never log in.
		"fido2": {qrdbsdk.FactorFIDO2, &qrdbsdk.AuthFactorEnrollment{
			Metadata: map[string]string{"credential_id": "cred-AQIDBA"},
		}},
		"piv no certificate":  {qrdbsdk.FactorPIV, &qrdbsdk.AuthFactorEnrollment{PublicKey: []byte("not a cert")}},
		"piv no key material": {qrdbsdk.FactorPIV, &qrdbsdk.AuthFactorEnrollment{}},
		"unsupported factor":  {qrdbsdk.AdminFactor("pkcs11"), &qrdbsdk.AuthFactorEnrollment{}},
	}
	for name, tc := range cases {
		if err := p.PersistAdminFactor(context.Background(), tc.factor, tc.enrollment); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// keySwappingIssuer stands for a CA that certifies some key other than the one
// in the CSR it was sent. It signs a CSR for its own key with the real CA.
type keySwappingIssuer struct {
	t  *testing.T
	ca *xkmsca.CAService
}

func (s *keySwappingIssuer) SignCSR(ctx context.Context, _ *SignCSRRequest) (*SignCSRResponse, error) {
	s.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		s.t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "intruder"},
	}, key)
	if err != nil {
		s.t.Fatalf("CreateCertificateRequest: %v", err)
	}
	resp, err := s.ca.SignCSR(ctx, &xkmstransport.SignCSRRequest{
		CSRPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}),
	})
	if err != nil {
		return nil, err
	}
	return &SignCSRResponse{CertificatePEM: string(resp.CertificatePEM)}, nil
}

func TestAdminFactorPersister_RefusesCertificateForAnotherKey(t *testing.T) {
	_, _, factors, ca := newAdminEnrollerCAFixture(t)
	card := xkmssdk.NewSoftwarePIVKeyGenerator()
	if _, err := card.GeneratePIVKey(xkmssdk.PIVSlotAuthentication, "ecdsap256", "PIV 9a"); err != nil {
		t.Fatalf("GeneratePIVKey: %v", err)
	}
	slots := newPIVCertStore(t)
	p := &adminFactorPersister{
		factors:  factors,
		issuer:   &keySwappingIssuer{t: t, ca: ca},
		pivKeys:  card,
		pivCerts: slots,
		subject:  "root",
		tenantID: adminTenant,
	}

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorPIV, &qrdbsdk.AuthFactorEnrollment{
		Metadata: map[string]string{"slot": "9a", "cn": "PIV 9a"},
	})
	if err == nil {
		t.Fatal("persisted a certificate that does not certify the card key")
	}
	listed, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listed.Total != 0 {
		t.Errorf("a factor was stored with a foreign certificate: %d factors", listed.Total)
	}
	if cert, err := slots.Retrieve(xkmssdk.PIVSlotAuthentication); err == nil {
		t.Errorf("slot 9a was given a foreign certificate: %s", cert.Subject)
	}
}

// refusingPIVCertStore is a slot store whose writes fail, as a full or
// write-protected card does.
type refusingPIVCertStore struct {
	backend.PIVCertificateStorage
}

func (refusingPIVCertStore) Store(xkmssdk.PIVSlot, *x509.Certificate) error {
	return xkmssdk.ErrPIVStorageFull
}

// A certificate that cannot be installed in the slot cannot be presented at
// login, so the factor is not stored either.
func TestAdminFactors_PIVRefusedWhenSlotRefusesCertificate(t *testing.T) {
	enroller, _, factors, _ := newAdminEnrollerCAFixture(t)

	_, err := enroller.Enroll(context.Background(), &AdminFactorEnrollRequest{
		Factors:      []string{"piv"},
		Subject:      "root",
		TenantID:     adminTenant,
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
		PIVCertStore: refusingPIVCertStore{PIVCertificateStorage: newPIVCertStore(t)},
	})
	if !errors.Is(err, xkmssdk.ErrPIVStorageFull) {
		t.Fatalf("Enroll err = %v, want the slot store's refusal", err)
	}
	listed, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listed.Total != 0 {
		t.Errorf("a factor was stored whose certificate is not in the slot: %d factors", listed.Total)
	}
}

func TestAdminFactors_PIVWithoutCertStoreRefused(t *testing.T) {
	enroller, _, _ := newAdminEnrollerFixture(t)

	err := enroller.Preflight(context.Background(), &AdminFactorEnrollRequest{
		Factors: []string{"piv"}, Subject: "root", TenantID: adminTenant,
		PIVGenerator: xkmssdk.NewSoftwarePIVKeyGenerator(),
	})
	var unavailable *qrdbsdk.AdminFactorUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Factor != qrdbsdk.FactorPIV {
		t.Fatalf("Preflight err = %v, want a piv AdminFactorUnavailableError", err)
	}
}

// mTLS is bound by first boot, so the persister must not write it twice.
func TestAdminFactorPersister_SkipsMTLS(t *testing.T) {
	_, _, factors := newAdminEnrollerFixture(t)
	p := &adminFactorPersister{factors: factors, subject: "root", tenantID: adminTenant}

	if err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorMTLS, &qrdbsdk.AuthFactorEnrollment{}); err != nil {
		t.Fatalf("mtls: %v", err)
	}
	list, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 0 {
		t.Errorf("mtls persisted %d factors, want 0", list.Total)
	}
}
