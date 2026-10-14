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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/fido2"
)

const (
	passkeyRPID   = "example.com"
	passkeyOrigin = "https://example.com"
	passkeyDevice = "/dev/hidraw-test"
)

// passkeyTestAAGUID is the authenticator model the E2E enrolls.
var passkeyTestAAGUID = []byte{
	0x2f, 0xc0, 0x57, 0x9f, 0x81, 0x13, 0x47, 0xea,
	0xb1, 0x16, 0xbb, 0x5a, 0x8d, 0xb9, 0x20, 0x2a,
}

// fakeAdminPasskey is an AdminPasskeyAuthenticator whose RegisterWebAuthn
// answers from attestation, or from create when set.
type fakeAdminPasskey struct {
	devices     []fido2.Device
	listErr     error
	attestation *fido2.WebAuthnAttestation
	registerErr error
	create      func(opts fido2.WebAuthnRegistrationOptions) (*fido2.WebAuthnAttestation, error)

	bound      string
	registered int
}

func (f *fakeAdminPasskey) ListDevices() ([]fido2.Device, error) { return f.devices, f.listErr }

func (f *fakeAdminPasskey) BindDevice(path string) { f.bound = path }

func (f *fakeAdminPasskey) RegisterWebAuthn(opts fido2.WebAuthnRegistrationOptions) (*fido2.WebAuthnAttestation, error) {
	f.registered++
	if f.create != nil {
		return f.create(opts)
	}
	return f.attestation, f.registerErr
}

// fakeAddCredentialClient is a FIDO2Client whose add-credential ceremony
// answers from its fields and counts the finishes it received.
type fakeAddCredentialClient struct {
	FIDO2Client
	begin     *FIDO2BeginRegistrationResponse
	beginErr  error
	finish    *FIDO2FinishRegistrationResponse
	finishErr error

	finishes int
}

func (f *fakeAddCredentialClient) BeginAddCredential(context.Context) (*FIDO2BeginRegistrationResponse, error) {
	return f.begin, f.beginErr
}

func (f *fakeAddCredentialClient) FinishRegistration(context.Context, *FIDO2FinishRegistrationRequest) (*FIDO2FinishRegistrationResponse, error) {
	f.finishes++
	return f.finish, f.finishErr
}

// addCredentialOptions is the creation-options JSON the server returns for a
// ceremony on the account with the given qrdb user ID.
func addCredentialOptions(t *testing.T, userID uint64) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"publicKey": map[string]any{
		"challenge": base64.RawURLEncoding.EncodeToString([]byte("add-credential-challenge")),
		"rp":        map[string]any{"id": passkeyRPID, "name": "Example"},
		"user": map[string]any{
			"id":          base64.RawURLEncoding.EncodeToString(adminWebAuthnUserHandle(userID)),
			"name":        "root",
			"displayName": "root",
		},
	}})
	if err != nil {
		t.Fatalf("marshal creation options: %v", err)
	}
	return string(raw)
}

// newPasskeyEnroller is the admin enroller fixture with its FIDO2 client
// replaced by client. The fixture's administrator is user 9.
func newPasskeyEnroller(t *testing.T, client FIDO2Client) (AdminFactorEnroller, AuthFactorsClient) {
	t.Helper()
	enroller, _, factors := newAdminEnrollerFixture(t)
	transportBacked, ok := enroller.(*adminFactorEnroller)
	if !ok {
		t.Fatalf("enroller is %T, want *adminFactorEnroller", enroller)
	}
	transportBacked.fido2 = client
	return enroller, factors
}

func passkeyRequest(handler AdminPasskeyAuthenticator) *AdminFactorEnrollRequest {
	return &AdminFactorEnrollRequest{
		Factors:        []string{"fido2"},
		Subject:        "root",
		TenantID:       adminTenant,
		RelyingPartyID: passkeyRPID,
		FIDO2Handler:   handler,
	}
}

func oneDevice() []fido2.Device { return []fido2.Device{{Path: passkeyDevice}} }

// A passkey the server stored is reported with the server's credential ID,
// created on the named device, and nothing is written from this process.
func TestAdminFactors_PasskeyReportedOnlyFromTheServer(t *testing.T) {
	client := &fakeAddCredentialClient{
		begin:  &FIDO2BeginRegistrationResponse{SessionID: "add-1", OptionsJSON: addCredentialOptions(t, 9)},
		finish: &FIDO2FinishRegistrationResponse{Success: true, CredentialID: "cred-1"},
	}
	enroller, factors := newPasskeyEnroller(t, client)
	passkey := &fakeAdminPasskey{
		devices:     []fido2.Device{{Path: "/dev/other"}, {Path: passkeyDevice}},
		attestation: &fido2.WebAuthnAttestation{CredentialID: []byte("c"), PublicKeyCredentialJSON: []byte("{}")},
	}
	req := passkeyRequest(passkey)
	req.PasskeyDevice = passkeyDevice

	results, err := enroller.Enroll(context.Background(), req)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if len(results) != 1 || results[0].Factor != qrdbsdk.FactorFIDO2 {
		t.Fatalf("results = %+v, want one fido2 enrollment", results)
	}
	if got := results[0].Enrollment.Metadata["credential_id"]; got != "cred-1" {
		t.Errorf("credential_id = %q, want the server's cred-1", got)
	}
	if passkey.bound != passkeyDevice {
		t.Errorf("bound device = %q, want %q", passkey.bound, passkeyDevice)
	}
	list, err := factors.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 0 {
		t.Errorf("%d factors written from the SDK, want 0: the server stores passkeys", list.Total)
	}
}

// Every way the request cannot reach a usable authenticator is refused in
// preflight, before the server is asked to open a ceremony.
func TestAdminFactors_PasskeyPreflightRefusals(t *testing.T) {
	var typedNil *fido2.FIDO2Handler
	cases := map[string]struct {
		handler AdminPasskeyAuthenticator
		device  string
		rpID    string
		want    sdkerrors.ErrorCode
	}{
		"typed nil handler":  {handler: typedNil, rpID: passkeyRPID, want: sdkerrors.CodePreconditionFailed},
		"no relying party":   {handler: &fakeAdminPasskey{devices: oneDevice()}, want: sdkerrors.CodeInvalidInput},
		"enumeration failed": {handler: &fakeAdminPasskey{listErr: errors.New("hid closed")}, rpID: passkeyRPID, want: sdkerrors.CodePreconditionFailed},
		"no device":          {handler: &fakeAdminPasskey{}, rpID: passkeyRPID, want: sdkerrors.CodePreconditionFailed},
		"ambiguous devices": {
			handler: &fakeAdminPasskey{devices: []fido2.Device{{Path: "/dev/a"}, {Path: "/dev/b"}}},
			rpID:    passkeyRPID, want: sdkerrors.CodePreconditionFailed,
		},
		"unmatched device": {handler: &fakeAdminPasskey{devices: oneDevice()}, device: "/dev/missing", rpID: passkeyRPID, want: sdkerrors.CodePreconditionFailed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := &fakeAddCredentialClient{beginErr: errors.New("begin must not be reached")}
			enroller, _ := newPasskeyEnroller(t, client)
			req := passkeyRequest(tc.handler)
			req.PasskeyDevice = tc.device
			req.RelyingPartyID = tc.rpID

			_, err := enroller.Enroll(context.Background(), req)
			if got := sdkerrors.Code(err); got != tc.want {
				t.Fatalf("Enroll = %v (code %v), want code %v", err, got, tc.want)
			}
			if tc.want == sdkerrors.CodePreconditionFailed {
				var unavailable *qrdbsdk.AdminFactorUnavailableError
				if name != "enumeration failed" && !errors.As(err, &unavailable) {
					t.Errorf("Enroll = %v, want *AdminFactorUnavailableError", err)
				}
			}
			if fake, ok := tc.handler.(*fakeAdminPasskey); ok && fake.registered != 0 {
				t.Error("authenticator was asked to create a passkey")
			}
		})
	}
}

// Once the ceremony is open, any failure is reported and no passkey is
// claimed; a ceremony for another account never reaches the authenticator.
func TestAdminFactors_PasskeyCeremonyRefusals(t *testing.T) {
	attestation := &fido2.WebAuthnAttestation{CredentialID: []byte("c"), PublicKeyCredentialJSON: []byte("{}")}
	cases := map[string]struct {
		client       *fakeAddCredentialClient
		passkey      *fakeAdminPasskey
		want         sdkerrors.ErrorCode
		wantRegister int
		wantFinishes int
	}{
		"begin refused": {
			client:  &fakeAddCredentialClient{beginErr: sdkerrors.NewUnauthorized("FIDO2.BeginAddCredential", "no session")},
			passkey: &fakeAdminPasskey{attestation: attestation},
			want:    sdkerrors.CodeUnauthorized,
		},
		"unusable options": {
			client:  &fakeAddCredentialClient{begin: &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: "{}"}},
			passkey: &fakeAdminPasskey{attestation: attestation},
			want:    sdkerrors.CodeInternal,
		},
		"another account": {
			client:  &fakeAddCredentialClient{begin: &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: addCredentialOptions(t, 10)}},
			passkey: &fakeAdminPasskey{attestation: attestation},
			want:    sdkerrors.CodeForbidden,
		},
		"authenticator failed": {
			client:       &fakeAddCredentialClient{begin: &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: addCredentialOptions(t, 9)}},
			passkey:      &fakeAdminPasskey{registerErr: errors.New("user did not touch the key")},
			want:         sdkerrors.CodeInternal,
			wantRegister: 1,
		},
		"finish refused": {
			client: &fakeAddCredentialClient{
				begin:     &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: addCredentialOptions(t, 9)},
				finishErr: sdkerrors.NewForbidden("FIDO2.FinishRegistration", "attestation not hardware"),
			},
			passkey:      &fakeAdminPasskey{attestation: attestation},
			want:         sdkerrors.CodeForbidden,
			wantRegister: 1, wantFinishes: 1,
		},
		"finish not stored": {
			client: &fakeAddCredentialClient{
				begin:  &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: addCredentialOptions(t, 9)},
				finish: &FIDO2FinishRegistrationResponse{Success: true},
			},
			passkey:      &fakeAdminPasskey{attestation: attestation},
			want:         sdkerrors.CodeInternal,
			wantRegister: 1, wantFinishes: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.passkey.devices = oneDevice()
			enroller, _ := newPasskeyEnroller(t, tc.client)

			results, err := enroller.Enroll(context.Background(), passkeyRequest(tc.passkey))
			if got := sdkerrors.Code(err); got != tc.want {
				t.Fatalf("Enroll = %v (code %v), want code %v", err, got, tc.want)
			}
			if len(results) != 0 {
				t.Errorf("Enroll reported %d enrollments after a failure", len(results))
			}
			if tc.passkey.registered != tc.wantRegister {
				t.Errorf("authenticator asked %d times, want %d", tc.passkey.registered, tc.wantRegister)
			}
			if tc.client.finishes != tc.wantFinishes {
				t.Errorf("finish called %d times, want %d", tc.client.finishes, tc.wantFinishes)
			}
		})
	}
}

// =============================================================================
// Enroll -> login through the production WebAuthn relying party.
// =============================================================================

// fido2QuicBaaS is a service.QuicBaaS whose only live method is FIDO2.
type fido2QuicBaaS struct {
	service.QuicBaaS
	fido2 service.FIDO2Service
}

func (m *fido2QuicBaaS) FIDO2() service.FIDO2Service { return m.fido2 }

// webAuthnManagerGetter supplies the WebAuthn manager and the identity store
// the FIDO2 service resolves the add-credential caller through.
type webAuthnManagerGetter struct {
	mgr        *pkgiam.XKMSWebAuthnManager
	identities *pkgiam.IdentityStore
}

func (g webAuthnManagerGetter) GetWebAuthnManager(context.Context) (pkgservice.WebAuthnManager, error) {
	return g.mgr, nil
}

func (g webAuthnManagerGetter) GetIdentityStore(context.Context) (*pkgiam.IdentityStore, error) {
	return g.identities, nil
}

// newCallerIdentities returns an identity store holding the one account an
// add-credential caller resolves to. The account keeps the qrdb user ID the
// test directory knows it by.
func newCallerIdentities(t *testing.T, tenantID string, id uint64, username, email string) *pkgiam.IdentityStore {
	t.Helper()
	mem := pkgiam.NewMemoryStorage()
	if err := mem.CreateUser(context.Background(), &pkgiam.User{
		ID: id, TenantID: tenantID, Username: username, Email: email,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return pkgiam.NewIdentityStore(pkgiam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
}

// hardwareAuthenticator is a software FIDO2 authenticator that attests like a
// security key: its packed statement is signed by a batch attestation key
// whose certificate chains to a manufacturer root.
type hardwareAuthenticator struct {
	t     *testing.T
	auth  virtualwebauthn.Authenticator
	cred  virtualwebauthn.Credential
	batch *x509.Certificate
}

// newHardwareAuthenticator mints a manufacturer root and a batch certificate
// meeting the WebAuthn packed-attestation certificate requirements, and uses
// the batch key as the credential key so the self-signed statement verifies
// under the batch certificate.
func newHardwareAuthenticator(t *testing.T) (*hardwareAuthenticator, *x509.Certificate) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test FIDO Attestation Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	root := createCert(t, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)

	batchKey := fullWidthP256Key(t)
	aaguidExt, err := asn1.Marshal(passkeyTestAAGUID)
	if err != nil {
		t.Fatalf("marshal aaguid extension: %v", err)
	}
	batch := createCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Country:            []string{"US"},
			Organization:       []string{"Test Authenticator Vendor"},
			OrganizationalUnit: []string{"Authenticator Attestation"},
			CommonName:         "Test Batch Attestation",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		ExtraExtensions:       []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 45724, 1, 1, 4}, Value: aaguidExt}},
	}, root, &batchKey.PublicKey, rootKey)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(batchKey)
	if err != nil {
		t.Fatalf("marshal batch key: %v", err)
	}
	h := &hardwareAuthenticator{
		t:     t,
		auth:  virtualwebauthn.NewAuthenticator(),
		cred:  virtualwebauthn.NewCredentialWithImportedKey(virtualwebauthn.KeyTypeEC2, pkcs8),
		batch: batch,
	}
	copy(h.auth.Aaguid[:], passkeyTestAAGUID)
	return h, root
}

// fullWidthP256Key returns a P-256 key whose coordinates are both 32 bytes.
// virtualwebauthn encodes the COSE key with big.Int.Bytes, which drops leading
// zero bytes, and go-webauthn rightly refuses the short coordinate.
func fullWidthP256Key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	for {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate batch key: %v", err)
		}
		if len(key.X.Bytes()) == 32 && len(key.Y.Bytes()) == 32 {
			return key
		}
	}
}

func createCert(t *testing.T, tmpl, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func (h *hardwareAuthenticator) ListDevices() ([]fido2.Device, error) { return oneDevice(), nil }

func (h *hardwareAuthenticator) BindDevice(string) {}

// RegisterWebAuthn creates the credential and adds the batch certificate to
// the attestation statement, as a security key's firmware does.
func (h *hardwareAuthenticator) RegisterWebAuthn(opts fido2.WebAuthnRegistrationOptions) (*fido2.WebAuthnAttestation, error) {
	rp := virtualwebauthn.RelyingParty{ID: opts.RPID, Name: opts.RPName, Origin: opts.Origin}
	h.auth.Options.UserHandle = opts.UserID
	raw := virtualwebauthn.CreateAttestationResponse(rp, h.auth, h.cred, virtualwebauthn.AttestationOptions{
		Challenge:        opts.Challenge,
		RelyingPartyID:   opts.RPID,
		RelyingPartyName: opts.RPName,
		UserName:         opts.UserName,
		UserDisplayName:  opts.UserDisplayName,
	})

	var credential map[string]any
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return nil, err
	}
	response, ok := credential["response"].(map[string]any)
	if !ok {
		h.t.Fatalf("virtual credential has no response: %s", raw)
	}
	encoded, ok := response["attestationObject"].(string)
	if !ok {
		h.t.Fatalf("virtual credential has no attestationObject: %s", raw)
	}
	attestationCBOR, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	var attestation struct {
		AuthData []byte         `json:"authData"`
		Format   string         `json:"fmt"`
		AttStmt  map[string]any `json:"attStmt"`
	}
	if err := webauthncbor.Unmarshal(attestationCBOR, &attestation); err != nil {
		return nil, err
	}
	attestation.AttStmt["x5c"] = []any{h.batch.Raw}
	rebuilt, err := webauthncbor.Marshal(attestation)
	if err != nil {
		return nil, err
	}
	response["attestationObject"] = base64.RawURLEncoding.EncodeToString(rebuilt)
	body, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}
	return &fido2.WebAuthnAttestation{CredentialID: h.cred.ID, PublicKeyCredentialJSON: body}, nil
}

// The administrator every passkey E2E enrolls.
const (
	passkeyTenant = "acme"
	passkeyUserID = uint64(9)
	passkeyEmail  = "root@example.com"
)

// passkeyServer is the server side of an administrator's passkey enrollment:
// the production FIDO2 service over the xkms WebAuthn relying party, the
// factor store the attested credential is recorded in, and the hardware
// authenticator the administrator holds.
type passkeyServer struct {
	client        *client
	store         qrdbsdk.UserAuthFactorServicer
	factory       *pkgiam.WebAuthnServiceFactory
	fido2         service.FIDO2Service
	authenticator *hardwareAuthenticator
}

func newPasskeyServer(t *testing.T) *passkeyServer {
	t.Helper()
	c, store := buildLoginFactorRoundtrip(t, newDirectory(testUser{
		id: passkeyUserID, tenantID: passkeyTenant, username: "root", email: passkeyEmail,
	}))
	qrdb, err := c.qrdbClient()
	if err != nil {
		t.Fatalf("qrdbClient: %v", err)
	}
	recorder, ok := store.(qrdbsdk.AttestationRecorder)
	if !ok {
		t.Fatalf("factor store %T records no attestations", store)
	}
	authenticator, root := newHardwareAuthenticator(t)
	roots, err := pkgiam.NewWebAuthnAttestationRoots(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}))
	if err != nil {
		t.Fatalf("NewWebAuthnAttestationRoots: %v", err)
	}
	factory, err := pkgiam.NewWebAuthnServiceFactory(pkgiam.WebAuthnFactoryParams{
		Config:              pkgiam.WebAuthnFactoryConfig{RPID: passkeyRPID, RPDisplayName: "Example", RPOrigins: []string{passkeyOrigin}},
		QRDB:                qrdb,
		AttestationRecorder: func() qrdbsdk.AttestationRecorder { return recorder },
		AAGUIDAllowlist:     pkgiam.NewAAGUIDAllowlist([]string{hex.EncodeToString(passkeyTestAAGUID)}, pkgiam.AAGUIDPolicyStrict),
		AttestationRoots:    roots,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	mgr, err := pkgiam.NewXKMSWebAuthnManager(factory)
	if err != nil {
		t.Fatalf("NewXKMSWebAuthnManager: %v", err)
	}
	return &passkeyServer{
		client:  c,
		store:   store,
		factory: factory,
		fido2: pkgservice.NewFIDO2Service(webAuthnManagerGetter{
			mgr:        mgr,
			identities: newCallerIdentities(t, passkeyTenant, 9, "root", passkeyEmail),
		}),
		authenticator: authenticator,
	}
}

// passkeyCaller is ctx authenticated as the administrator being enrolled.
func passkeyCaller(ctx context.Context) context.Context {
	return auth.SubjectIntoContext(ctx, interfaces.Subject{
		ID: "9", IDKind: interfaces.SubjectIDUserID, TenantID: passkeyTenant, Authenticated: true,
	})
}

// enroll runs AdminFactors().Enroll for the administrator's passkey with the
// FIDO2 ceremony carried over tr, and returns the credential ID it reports.
func (s *passkeyServer) enroll(t *testing.T, tr transport.Transport) string {
	t.Helper()
	enroller := newAdminFactorEnroller(s.client, s.client.Security().Auth().Factors(), &fido2TransportClient{t: tr})
	results, err := enroller.Enroll(passkeyCaller(context.Background()), &AdminFactorEnrollRequest{
		Factors:        []string{"fido2"},
		Subject:        "root",
		TenantID:       passkeyTenant,
		RelyingPartyID: passkeyRPID,
		FIDO2Handler:   s.authenticator,
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if len(results) != 1 || results[0].Factor != qrdbsdk.FactorFIDO2 {
		t.Fatalf("results = %+v, want one fido2 enrollment", results)
	}
	return results[0].Enrollment.Metadata["credential_id"]
}

// embeddedFIDO2 carries the ceremony to the FIDO2 service in-process.
func embeddedFIDO2(t *testing.T, svc service.FIDO2Service) transport.Transport {
	t.Helper()
	tr, err := embedded.New(&fido2QuicBaaS{fido2: svc})
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	t.Cleanup(func() { closeFIDO2Transport(t, tr) })
	return tr
}

func closeFIDO2Transport(t *testing.T, tr transport.Transport) {
	if err := tr.Close(); err != nil {
		t.Errorf("close transport: %v", err)
	}
}

// Every transport reports an enrolled passkey by the ID of the credential the
// authenticator attested, and that ID names the factor the server stored.
func TestAdminFactors_PasskeyCredentialIDOnEveryTransport(t *testing.T) {
	for name, open := range adminFactorTransports {
		t.Run(name, func(t *testing.T) {
			s := newPasskeyServer(t)
			credentialID := s.enroll(t, open(t, newAdminFactorServer(t, s.fido2, nil)))

			if want := base64.RawURLEncoding.EncodeToString(s.authenticator.cred.ID); credentialID != want {
				t.Fatalf("credential_id = %q, want the attested credential %q", credentialID, want)
			}
			factor, err := s.store.LookupByCredentialKey(passkeyCaller(context.Background()), qrdbsdk.AuthFactorFIDO2, []byte(credentialID))
			if err != nil {
				t.Fatalf("LookupByCredentialKey(%q): %v", credentialID, err)
			}
			if factor.UserID != passkeyUserID {
				t.Errorf("factor user = %d, want %d", factor.UserID, passkeyUserID)
			}
		})
	}
}

// A passkey enrollment with no authenticator handler is refused in preflight
// on every remote transport, before any request reaches the server.
func TestAdminFactors_PasskeyWithoutHandlerRefusedOnEveryTransport(t *testing.T) {
	for name, open := range adminFactorTransports {
		if name == "embedded" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			s := newPasskeyServer(t)
			server := newAdminFactorServer(t, s.fido2, nil)
			enroller := newAdminFactorEnroller(s.client, s.client.Security().Auth().Factors(), &fido2TransportClient{t: open(t, server)})

			_, err := enroller.Enroll(passkeyCaller(context.Background()), &AdminFactorEnrollRequest{
				Factors:        []string{"fido2"},
				Subject:        "root",
				TenantID:       passkeyTenant,
				RelyingPartyID: passkeyRPID,
			})
			if got := sdkerrors.Code(err); got != sdkerrors.CodePreconditionFailed {
				t.Fatalf("Enroll = %v (code %v), want code %v", err, got, sdkerrors.CodePreconditionFailed)
			}
			var unavailable *qrdbsdk.AdminFactorUnavailableError
			if !errors.As(err, &unavailable) {
				t.Errorf("Enroll = %v, want *AdminFactorUnavailableError", err)
			}
			if calls := server.calls.Load(); calls != 0 {
				t.Errorf("%d requests reached the server, want 0", calls)
			}
		})
	}
}

// An administrator enrolled through AdminFactors().Enroll holds a passkey the
// server stored as hardware-attested, and that passkey logs in through the
// production WebAuthn factor verifier.
func TestAdminFactors_PasskeyEnrollsAndLogsIn(t *testing.T) {
	s := newPasskeyServer(t)
	ctx := passkeyCaller(context.Background())
	credentialID := s.enroll(t, embeddedFIDO2(t, s.fido2))
	if credentialID != base64.RawURLEncoding.EncodeToString(s.authenticator.cred.ID) {
		t.Fatalf("credential_id = %q, want the authenticator's credential", credentialID)
	}

	// The factor login resolves is the one the server stored, proven hardware.
	factor, err := s.store.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorFIDO2, []byte(credentialID))
	if err != nil {
		t.Fatalf("LookupByCredentialKey: %v", err)
	}
	if factor.UserID != passkeyUserID {
		t.Fatalf("factor user = %d, want %d", factor.UserID, passkeyUserID)
	}
	if got := factor.Metadata[qrdbsdk.MetadataAttestLevel]; got != string(qrdbsdk.AttestationLevelHardware) {
		t.Errorf("attest level = %q, want hardware", got)
	}

	// Log in: the production verifier opens an assertion ceremony and checks
	// the authenticator's signature against the stored credential.
	svc, err := s.factory.ServiceFor(ctx, passkeyTenant)
	if err != nil {
		t.Fatalf("ServiceFor: %v", err)
	}
	verifier, err := xkmssdk.NewWebAuthnFactorVerifier(svc)
	if err != nil {
		t.Fatalf("NewWebAuthnFactorVerifier: %v", err)
	}
	_, meta, _, err := verifier.BeginChallenge(ctx, passkeyTenant, qrdbtransport.FactorTypeFIDO2, passkeyEmail)
	if err != nil {
		t.Fatalf("BeginChallenge: %v", err)
	}
	assertion := func(cred virtualwebauthn.Credential) string {
		opts, err := virtualwebauthn.ParseAssertionOptions(meta["options"])
		if err != nil {
			t.Fatalf("ParseAssertionOptions: %v", err)
		}
		rp := virtualwebauthn.RelyingParty{ID: passkeyRPID, Name: "Example", Origin: passkeyOrigin}
		return virtualwebauthn.CreateAssertionResponse(rp, s.authenticator.auth, cred, *opts)
	}
	err = verifier.Verify(ctx, factor, qrdbtransport.VerifyChallenge{
		Type: qrdbtransport.FactorTypeFIDO2,
		MethodMetadata: map[string]string{
			"session_id":    meta["session_id"],
			"credential_id": credentialID,
			"assertion":     assertion(s.authenticator.cred),
		},
	})
	if err != nil {
		t.Fatalf("login with the enrolled passkey: %v", err)
	}

	// The control: a different key presenting the same credential ID fails.
	_, meta, _, err = verifier.BeginChallenge(ctx, passkeyTenant, qrdbtransport.FactorTypeFIDO2, passkeyEmail)
	if err != nil {
		t.Fatalf("BeginChallenge: %v", err)
	}
	impostorKey, err := x509.MarshalPKCS8PrivateKey(fullWidthP256Key(t))
	if err != nil {
		t.Fatalf("marshal impostor key: %v", err)
	}
	impostor := virtualwebauthn.NewCredentialWithImportedKey(virtualwebauthn.KeyTypeEC2, impostorKey)
	impostor.ID = s.authenticator.cred.ID
	err = verifier.Verify(ctx, factor, qrdbtransport.VerifyChallenge{
		Type: qrdbtransport.FactorTypeFIDO2,
		MethodMetadata: map[string]string{
			"session_id":    meta["session_id"],
			"credential_id": credentialID,
			"assertion":     assertion(impostor),
		},
	})
	var sigErr *xkmssdk.SignatureVerifyError
	if !errors.As(err, &sigErr) {
		t.Fatalf("login with another key = %v, want *SignatureVerifyError", err)
	}
}
