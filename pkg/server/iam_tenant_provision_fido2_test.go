//go:build !integration

package server

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
	"math/big"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
	webauthncbor "github.com/go-webauthn/webauthn/protocol/webauthncbor"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// passkeyRPID and passkeyOrigin are the relying party the invited admin's
// browser would talk to.
const (
	passkeyRPID   = "localhost"
	passkeyOrigin = "https://localhost"
)

// passkeyAAGUID is the authenticator model the invited admin enrolls. It is
// allowlisted, and its batch certificate chains to the fixture's root, so the
// passkey proves hardware as an administrative credential must.
var passkeyAAGUID = []byte{
	0x51, 0x7a, 0x0c, 0x36, 0x2d, 0x4e, 0x46, 0x9b,
	0x8f, 0x13, 0x77, 0xa1, 0x06, 0xc2, 0xd4, 0x58,
}

// nodeUserServicer resolves the embedded node's user service, so invited
// admins are qrdb users exactly as in production and the WebAuthn relying
// party (which reads qrdb's user store) sees them.
type nodeUserServicer struct {
	svc qrdbsdk.UserServicer
}

func (r nodeUserServicer) UserServicer(context.Context) (qrdbsdk.UserServicer, error) {
	return r.svc, nil
}

// nodeRoleClient resolves the embedded node's role client, so tenant roles
// live where the node's RBAC reads their grants, as in production.
type nodeRoleClient struct {
	rc qrdbtransport.RoleClient
}

func (r nodeRoleClient) RoleClient(context.Context) (qrdbtransport.RoleClient, error) {
	return r.rc, nil
}

// passkeyFixture composes the invite-mode provisioner, the WebAuthn invite
// activation and the login ceremony over one in-process qrdb node, wired as
// iam_init.go and initStandaloneQRDBNode wire them.
type passkeyFixture struct {
	srv  *Server
	node *qrdbnode.Node
	c    *iamComponents
	p    *tenantProvisioner
	key  *passkeyAuthenticator
}

func newPasskeyFixture(t *testing.T) *passkeyFixture {
	t.Helper()
	srv := withTempServer(t)
	srv.buildLoginSessionIssuer()
	srv.buildLoginFactorVerifiers()
	nodeOpts := []qrdbnode.NodeOption{qrdbnode.WithSessionIssuer(srv.loginSessionIssuer)}
	for _, v := range srv.loginFactorVerifiers {
		nodeOpts = append(nodeOpts, qrdbnode.WithLoginFactorVerifier(v))
	}
	node := buildMemoryStandaloneNode(t, nodeOpts...)
	t.Cleanup(func() { require.NoError(t, node.Close()) })
	srv.qrdbNode = node

	storage := iam.NewMemoryStorage()
	users := qrdbadapter.NewQRDBUserStorage(nodeUserServicer{svc: node.UserService()})
	roles := iam.NewQRDBRoleStorage(nodeRoleClient{rc: node.Client().Roles()})
	c, _ := buildTestIAMComponentsOver(t, storage, users, roles)
	auditLogger, err := iam.NewAuditLogger()
	require.NoError(t, err)
	c.auditLogger = auditLogger

	key, root := newPasskeyAuthenticator(t)
	roots, err := iam.NewWebAuthnAttestationRoots(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw}))
	require.NoError(t, err)
	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          passkeyRPID,
			RPDisplayName: "QuicBaaS Test",
			RPOrigins:     []string{passkeyOrigin},
			Timeout:       time.Minute,
		},
		QRDB:                node.Client(),
		AttestationRecorder: srv.qrdbAttestationRecorder,
		Assurance:           iam.WebAuthnAssuranceHardware,
		AAGUIDAllowlist:     iam.NewAAGUIDAllowlist([]string{hex.EncodeToString(passkeyAAGUID)}, iam.AAGUIDPolicyStrict),
		Admins:              iam.NewWebAuthnAdminResolver(users, qrdbadapter.WrapRole(c.identityStore)),
		AttestationRoots:    roots,
	})
	require.NoError(t, err)
	mgr, err := iam.NewXKMSWebAuthnManager(factory)
	require.NoError(t, err)
	mgr.SetUserResolver(users)
	c.webauthnManager = mgr
	c.oidcProvider = newPasskeyOIDCProvider(t, srv, c.identityStore)
	srv.bindLoginCeremonyProvider(c)

	return &passkeyFixture{srv: srv, node: node, c: c, p: srv.newTenantProvisioner(c), key: key}
}

// newPasskeyOIDCProvider builds the session authority as iam_init.go does:
// token scopes come from the node's RBAC through serverPermissionResolver.
func newPasskeyOIDCProvider(t *testing.T, srv *Server, identity *iam.IdentityStore) *iam.OIDCProvider {
	t.Helper()
	setupOIDCTestXKMS(t)
	keys, err := iam.NewSigningKeyManagerFromXKMS("passkey-admin-test", "")
	require.NoError(t, err)
	backend := iam.NewBackendStorage(kvstore.NewMemory())
	provider, err := iam.NewOIDCProvider(
		&iam.OIDCConfig{
			Issuer:            "https://issuer.test",
			SigningAlgorithm:  iam.SigningAlgorithmES256,
			SigningKeyManager: keys,
			AccessTokenTTL:    15 * time.Minute,
			RefreshTokenTTL:   24 * time.Hour,
			IDTokenTTL:        time.Hour,
			AuthCodeTTL:       10 * time.Minute,
			PKCERequired:      true,
			AllowedMethods:    []string{"S256"},
		},
		iam.OIDCDependencies{
			IdentityStore:      identity,
			AppStorage:         backend,
			TokenStorage:       backend,
			PermissionResolver: &serverPermissionResolver{server: srv},
		},
	)
	require.NoError(t, err)
	return provider
}

// deleteUsers asks the policy enforcement point whether a principal may
// delete users in target, the Admin-only permission behind DELETE
// /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id}
// (revoking another user's passkey). The request pipeline binds the target
// tenant to the context before authorizing; so does this.
func (f *passkeyFixture) deleteUsers(ctx context.Context, target, subject, tenantID string, roles []string) error {
	principal := &iam.Subject{ID: subject, TenantID: tenantID, Roles: roles}
	return newQRDBPEPRBACAuthorizer(f.srv).Authorize(qrdbsdk.ContextWithTenant(ctx, target), principal, "users", "delete")
}

// login runs the unified login ceremony the /api/v1/auth/login routes serve:
// qrdb opens the WebAuthn challenge through the tenant's relying party, the
// passkey asserts, and qrdb verifies it and mints the session bearer.
func (f *passkeyFixture) login(ctx context.Context, tenantID, email string) (*service.FinishLoginResult, error) {
	login := service.NewLoginService(f.node.Client().Login())
	begin, err := login.BeginLogin(ctx, &qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     email,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	credentialID := base64.RawURLEncoding.EncodeToString(f.key.cred.ID)
	return login.FinishLogin(ctx, &qrdbtransport.FinishLoginRequest{
		SessionID: begin.SessionID,
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte(credentialID),
		Signature: []byte(f.key.assert(begin.ChallengeMetadata["options"])),
		MethodMetadata: map[string]string{
			"session_id":    begin.ChallengeMetadata["session_id"],
			"credential_id": credentialID,
		},
	})
}

// activation is the invite activation service the REST, gRPC and MCP invite
// routes all delegate to.
func (f *passkeyFixture) activation() service.InviteActivationService {
	return service.NewInviteActivationService(passkeyActivationSource{c: f.c})
}

// passkeyActivationSource serves the invite activation service from the
// composed IAM components.
type passkeyActivationSource struct {
	c *iamComponents
}

func (s passkeyActivationSource) GetInviteStore(context.Context) (iam.InviteStore, error) {
	return s.c.inviteStore, nil
}

func (s passkeyActivationSource) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return s.c.identityStore, nil
}

func (s passkeyActivationSource) GetWebAuthnManager(context.Context) (service.WebAuthnManager, error) {
	return s.c.webauthnManager, nil
}

func (s passkeyActivationSource) GetPIVActivator(context.Context) (*iam.PIVActivator, error) {
	return s.c.pivActivator, nil
}

func (s passkeyActivationSource) GetAuditLogger(context.Context) (iam.AuditLogger, error) {
	return s.c.auditLogger, nil
}

// passkeyAuthenticator is a software FIDO2 authenticator that attests like a
// security key: its packed attestation carries a batch certificate chained to
// a manufacturer root. The credential key IS the batch key, so the packed
// signature verifies under x5c[0].
type passkeyAuthenticator struct {
	t     *testing.T
	auth  virtualwebauthn.Authenticator
	cred  virtualwebauthn.Credential
	batch *x509.Certificate
}

func newPasskeyAuthenticator(t *testing.T) (*passkeyAuthenticator, *x509.Certificate) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test FIDO Attestation Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	root := signPasskeyCert(t, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)

	batchKey := fullWidthP256(t)
	aaguidExt, err := asn1.Marshal(passkeyAAGUID)
	require.NoError(t, err)
	batch := signPasskeyCert(t, &x509.Certificate{
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
	require.NoError(t, err)
	a := &passkeyAuthenticator{
		t:     t,
		auth:  virtualwebauthn.NewAuthenticator(),
		cred:  virtualwebauthn.NewCredentialWithImportedKey(virtualwebauthn.KeyTypeEC2, pkcs8),
		batch: batch,
	}
	copy(a.auth.Aaguid[:], passkeyAAGUID)
	return a, root
}

// fullWidthP256 returns a P-256 key whose coordinates both encode to 32 bytes;
// the virtual authenticator's COSE encoding assumes it.
func fullWidthP256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	for {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		if len(key.X.Bytes()) == 32 && len(key.Y.Bytes()) == 32 {
			return key
		}
	}
}

func signPasskeyCert(t *testing.T, tmpl, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// register answers a registration ceremony's options with a hardware-attested
// credential, in the JSON shape a browser posts.
func (a *passkeyAuthenticator) register(options map[string]interface{}) map[string]interface{} {
	a.t.Helper()
	encoded, err := json.Marshal(options)
	require.NoError(a.t, err)
	parsed, err := virtualwebauthn.ParseAttestationOptions(string(encoded))
	require.NoError(a.t, err)

	rp := virtualwebauthn.RelyingParty{ID: passkeyRPID, Name: parsed.RelyingPartyName, Origin: passkeyOrigin}
	a.auth.Options.UserHandle = []byte(parsed.UserID)
	raw := virtualwebauthn.CreateAttestationResponse(rp, a.auth, a.cred, *parsed)

	var credential map[string]interface{}
	require.NoError(a.t, json.Unmarshal([]byte(raw), &credential))
	response, ok := credential["response"].(map[string]interface{})
	require.True(a.t, ok, "virtual credential has no response")
	attestationObject, ok := response["attestationObject"].(string)
	require.True(a.t, ok, "virtual credential has no attestationObject")
	attestationCBOR, err := base64.RawURLEncoding.DecodeString(attestationObject)
	require.NoError(a.t, err)

	var attestation struct {
		AuthData []byte         `json:"authData"`
		Format   string         `json:"fmt"`
		AttStmt  map[string]any `json:"attStmt"`
	}
	require.NoError(a.t, webauthncbor.Unmarshal(attestationCBOR, &attestation))
	attestation.AttStmt["x5c"] = []any{a.batch.Raw}
	rebuilt, err := webauthncbor.Marshal(attestation)
	require.NoError(a.t, err)
	response["attestationObject"] = base64.RawURLEncoding.EncodeToString(rebuilt)
	credential["response"] = response
	return credential
}

// assert answers a login ceremony's request options with this passkey's
// assertion, in the JSON shape a browser posts.
func (a *passkeyAuthenticator) assert(options string) string {
	a.t.Helper()
	parsed, err := virtualwebauthn.ParseAssertionOptions(options)
	require.NoError(a.t, err)
	rp := virtualwebauthn.RelyingParty{ID: passkeyRPID, Name: "QuicBaaS Test", Origin: passkeyOrigin}
	return virtualwebauthn.CreateAssertionResponse(rp, a.auth, a.cred, *parsed)
}

func TestTenantProvisioner_InvitedAdminActivatesWithPasskey(t *testing.T) {
	f := newPasskeyFixture(t)
	ctx := context.Background()

	resp, err := f.p.Provision(ctx, inviteRequest())
	require.NoError(t, err)
	require.True(t, resp.AdminPending)

	activation := f.activation()
	options, err := activation.ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
	require.NoError(t, err, "the invited admin starts the passkey ceremony")
	activated, err := activation.ActivateFIDO2Finish(ctx, provisionTenantID, resp.InviteToken, f.key.register(options))
	require.NoError(t, err, "the invited admin finishes the passkey ceremony")
	assert.Equal(t, resp.AdminUserID, activated.UserID)

	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, resp.AdminUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, admin.Lifecycle)
	assert.Contains(t, admin.Roles, resp.AdminRoleID)

	session, err := f.login(ctx, provisionTenantID, admin.Email)
	require.NoError(t, err, "the activated admin signs in with the passkey")
	claims, err := f.c.oidcProvider.ValidateAccessToken(ctx, session.Bearer)
	require.NoError(t, err)
	assert.Equal(t, provisionTenantID, claims.TenantID)
	assert.Contains(t, claims.Roles, resp.AdminRoleID, "the session carries the tenant's real Admin role")
	assert.Equal(t, resp.AdminUserID, claims.UserID)

	require.NoError(t, f.deleteUsers(ctx, provisionTenantID, claims.Subject, claims.TenantID, claims.Roles),
		"the session may perform an Admin-only action in its tenant")
	require.Error(t, f.deleteUsers(ctx, provisionTenantID, claims.Subject, claims.TenantID, nil),
		"the action is Admin-only: the same subject without the role is refused")

	// A second tenant, provisioned the same way, admits none of it.
	other := inviteRequest()
	other.TenantID = "globex"
	_, err = f.p.Provision(ctx, other)
	require.NoError(t, err)
	_, err = f.login(ctx, other.TenantID, admin.Email)
	var notFound *qrdbtransport.FactorNotFoundError
	require.ErrorAs(t, err, &notFound, "the passkey does not sign the admin in to another tenant")
	require.Error(t, f.deleteUsers(ctx, other.TenantID, claims.Subject, claims.TenantID, claims.Roles),
		"the session's Admin role grants nothing in another tenant")
	require.Error(t, f.deleteUsers(ctx, other.TenantID, claims.Subject, other.TenantID, claims.Roles),
		"nor does claiming the other tenant with this tenant's Admin role")
}

func TestTenantProvisioner_PasskeyInviteRefusesExpiredToken(t *testing.T) {
	f := newPasskeyFixture(t)
	ctx := context.Background()
	resp, err := f.p.Provision(ctx, inviteRequest())
	require.NoError(t, err)

	// The same pending admin, invited by a token whose lifetime has run out.
	expired, _, err := f.c.inviteStore.Create(ctx, provisionTenantID, resp.AdminUserID, -time.Minute)
	require.NoError(t, err)

	activation := f.activation()
	_, err = activation.ActivateFIDO2Begin(ctx, provisionTenantID, expired)
	require.ErrorIs(t, err, iam.ErrInviteExpired, "an expired invite opens no ceremony")

	// A ceremony opened by the live invite cannot be closed with the expired one.
	options, err := activation.ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
	require.NoError(t, err)
	_, err = activation.ActivateFIDO2Finish(ctx, provisionTenantID, expired, f.key.register(options))
	require.ErrorIs(t, err, iam.ErrInviteExpired, "an expired invite activates nothing")
	f.requirePending(ctx, t, resp)
}

func TestTenantProvisioner_PasskeyInviteRefusesReplayedToken(t *testing.T) {
	f := newPasskeyFixture(t)
	ctx := context.Background()
	resp, err := f.p.Provision(ctx, inviteRequest())
	require.NoError(t, err)

	activation := f.activation()
	options, err := activation.ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
	require.NoError(t, err)
	_, err = activation.ActivateFIDO2Finish(ctx, provisionTenantID, resp.InviteToken, f.key.register(options))
	require.NoError(t, err)

	_, err = activation.ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
	require.ErrorIs(t, err, iam.ErrInviteUsed, "a redeemed invite opens no second ceremony")
	_, err = activation.ActivateFIDO2Finish(ctx, provisionTenantID, resp.InviteToken, f.key.register(options))
	require.ErrorIs(t, err, iam.ErrInviteUsed, "a redeemed invite cannot be replayed to enroll again")
}

func TestTenantProvisioner_PasskeyInviteUnfinishedLeavesAdminPending(t *testing.T) {
	t.Run("ceremony begun and never finished", func(t *testing.T) {
		f := newPasskeyFixture(t)
		ctx := context.Background()
		resp, err := f.p.Provision(ctx, inviteRequest())
		require.NoError(t, err)

		_, err = f.activation().ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
		require.NoError(t, err)
		f.requirePending(ctx, t, resp)
	})

	t.Run("finish without a ceremony", func(t *testing.T) {
		f := newPasskeyFixture(t)
		ctx := context.Background()
		resp, err := f.p.Provision(ctx, inviteRequest())
		require.NoError(t, err)

		_, err = f.activation().ActivateFIDO2Finish(ctx, provisionTenantID, resp.InviteToken, map[string]interface{}{})
		var noSession *iam.WebAuthnFactoryConfigError
		require.ErrorAs(t, err, &noSession, "no ceremony was opened for this invite")
		f.requirePending(ctx, t, resp)
	})

	t.Run("passkey refused by the hardware gate", func(t *testing.T) {
		f := newPasskeyFixture(t)
		ctx := context.Background()
		resp, err := f.p.Provision(ctx, inviteRequest())
		require.NoError(t, err)

		// Attested by a vendor root the deployment does not trust, so it is
		// not proven hardware and cannot become an administrative credential.
		untrusted, _ := newPasskeyAuthenticator(t)
		activation := f.activation()
		options, err := activation.ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
		require.NoError(t, err)
		_, err = activation.ActivateFIDO2Finish(ctx, provisionTenantID, resp.InviteToken, untrusted.register(options))
		require.Error(t, err)
		f.requirePending(ctx, t, resp)
	})
}

// requirePending asserts the invited admin is still pending and its invite
// is still redeemable: an activation that did not finish consumed nothing.
func (f *passkeyFixture) requirePending(ctx context.Context, t *testing.T, resp *service.IAMProvisionTenantResponse) {
	t.Helper()
	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, resp.AdminUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecyclePending, admin.Lifecycle, "the admin stays pending")
	_, err = f.c.inviteStore.Lookup(ctx, provisionTenantID, resp.InviteToken)
	require.NoError(t, err, "the invite stays redeemable")
}
