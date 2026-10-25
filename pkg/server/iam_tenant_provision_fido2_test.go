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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
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
	srv   *Server
	node  *qrdbnode.Node
	c     *iamComponents
	users iam.UserStorage
	p     *tenantProvisioner
	key   *passkeyAuthenticator
}

func newPasskeyFixture(t *testing.T) *passkeyFixture {
	t.Helper()
	return newPasskeyFixtureOn(t, func(t *testing.T, nodeOpts ...qrdbnode.NodeOption) *qrdbnode.Node {
		node := buildMemoryStandaloneNode(t, nodeOpts...)
		t.Cleanup(func() { require.NoError(t, node.Close()) })
		return node
	})
}

// newPasskeyFixtureOn builds the passkey fixture over the node buildNode
// returns, so a test can run the invitation ceremony on an auth-enabled node.
func newPasskeyFixtureOn(t *testing.T, buildNode func(*testing.T, ...qrdbnode.NodeOption) *qrdbnode.Node) *passkeyFixture {
	t.Helper()
	return newPasskeyFixtureAssured(t, buildNode, iam.WebAuthnAssuranceHardware)
}

// newPasskeyFixtureAssured is newPasskeyFixtureOn under the enrollment
// assurance policy a deployment configures (iam.webauthn.assurance).
func newPasskeyFixtureAssured(t *testing.T, buildNode func(*testing.T, ...qrdbnode.NodeOption) *qrdbnode.Node, assurance iam.WebAuthnAssurance) *passkeyFixture {
	t.Helper()
	srv := withTempServer(t)
	srv.buildLoginSessionIssuer()
	srv.buildLoginFactorVerifiers()
	nodeOpts := []qrdbnode.NodeOption{qrdbnode.WithSessionIssuer(srv.loginSessionIssuer)}
	for _, v := range srv.loginFactorVerifiers {
		nodeOpts = append(nodeOpts, qrdbnode.WithLoginFactorVerifier(v))
	}
	node := buildNode(t, nodeOpts...)
	srv.qrdbNode = node
	// As lifecycle.go wires it: a login replays its factor's attestation.
	if factors := node.Client().UserAuthFactors(); factors != nil {
		srv.loginSessionIssuer.(*oidcSessionIssuer).SetAuthFactorClient(factors)
	}

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
		Service:             node,
		AttestationRecorder: srv.qrdbAttestationRecorder,
		Assurance:           assurance,
		AAGUIDAllowlist:     iam.NewAAGUIDAllowlist([]string{hex.EncodeToString(passkeyAAGUID)}, iam.AAGUIDPolicyStrict),
		Admins:              srv.webAuthnAdminResolver(users, c.identityStore),
		AttestationRoots:    roots,
	})
	require.NoError(t, err)
	mgr, err := iam.NewXKMSWebAuthnManager(factory)
	require.NoError(t, err)
	mgr.SetUserResolver(users)
	c.webauthnManager = mgr
	c.oidcProvider = newPasskeyOIDCProvider(t, srv, c.identityStore)
	srv.bindLoginCeremonyProvider(c)

	return &passkeyFixture{srv: srv, node: node, c: c, users: users, p: srv.newTenantProvisioner(c), key: key}
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
			// As iam_init.go wires it: issuance has no caller yet.
			IssuanceReadBinder: srv.issuanceReadContext,
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

	// On this release a non-system tenant's passkey activation is refused
	// before any ceremony opens: the passkey it would enroll cannot sign in
	// until go-quicbaas #1816 re-pins go-qrdb to v0.54.0-alpha, and that
	// change turns both calls back into an activation.
	activation := f.activation()
	_, err = activation.ActivateFIDO2Begin(ctx, provisionTenantID, resp.InviteToken)
	var refused *sdkerrors.TenantLoginRefusedError
	require.ErrorAs(t, err, &refused, "the passkey ceremony is refused until #1816")
	_, err = activation.ActivateFIDO2Finish(ctx, provisionTenantID, resp.InviteToken, map[string]interface{}{})
	require.ErrorAs(t, err, &refused, "the passkey enrollment is refused until #1816")
	f.requirePending(ctx, t, provisionTenantID, resp.AdminUserID, resp.InviteToken)

	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, resp.AdminUserID)
	require.NoError(t, err)
	session, err := f.login(ctx, provisionTenantID, admin.Email)
	require.Error(t, err, "the refused activation enrolled no passkey to sign in with")
	assert.Nil(t, session)

	// The Admin role the provisioner granted is still the tenant's real one.
	assert.Contains(t, admin.Roles, resp.AdminRoleID)
	claims := &iam.Claims{Subject: admin.LoginName(), TenantID: provisionTenantID, Roles: admin.Roles}
	require.NoError(t, f.deleteUsers(ctx, provisionTenantID, claims.Subject, claims.TenantID, claims.Roles),
		"the session may perform an Admin-only action in its tenant")
	require.Error(t, f.deleteUsers(ctx, provisionTenantID, claims.Subject, claims.TenantID, nil),
		"the action is Admin-only: the same subject without the role is refused")

	// A second tenant, provisioned the same way, admits none of it.
	other := inviteRequest()
	other.TenantID = "globex"
	_, err = f.p.Provision(ctx, other)
	require.NoError(t, err)
	require.Error(t, f.deleteUsers(ctx, other.TenantID, claims.Subject, claims.TenantID, claims.Roles),
		"the session's Admin role grants nothing in another tenant")
	require.Error(t, f.deleteUsers(ctx, other.TenantID, claims.Subject, other.TenantID, claims.Roles),
		"nor does claiming the other tenant with this tenant's Admin role")
}

// TestTenantProvisioner_SystemInviteActivatesWithPasskey is the activation
// the refusal above withholds, in the tenant where it runs on this release:
// the invited user enrolls a passkey and turns active.
func TestTenantProvisioner_SystemInviteActivatesWithPasskey(t *testing.T) {
	f := newPasskeyFixtureOn(t, newPrincipalNode)
	ctx := context.Background()
	invite := f.inviteSystemUser(ctx, t)

	activation := f.activation()
	options, err := activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.NoError(t, err, "the invited user starts the passkey ceremony")
	activated, err := activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, f.key.register(options))
	require.NoError(t, err, "the invited user finishes the passkey ceremony")
	assert.Equal(t, invite.userID, activated.UserID)

	user, err := f.c.identityStore.GetUser(ctx, qrdbsdk.SystemTenantID, invite.userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle)
	_, err = activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.ErrorIs(t, err, iam.ErrInviteUsed, "the activation spent the invite")

	// The activated user signs in with the passkey on the auth-enabled node,
	// and the session replays the attestation the enrollment recorded.
	session, err := f.login(ctx, qrdbsdk.SystemTenantID, user.Email)
	require.NoError(t, err, "the activated user signs in with the passkey")
	claims, err := f.c.oidcProvider.ValidateAccessToken(ctx, session.Bearer)
	require.NoError(t, err)
	assert.Equal(t, string(iam.AttestLevelHardware), claims.AttestLevel, "the session replays the factor's attestation")
	assert.Equal(t, hex.EncodeToString(passkeyAAGUID), claims.AttestAAGUID)
}

func TestTenantProvisioner_PasskeyInviteRefusesExpiredToken(t *testing.T) {
	f := newPasskeyFixtureOn(t, newPrincipalNode)
	ctx := context.Background()
	invite := f.inviteSystemUser(ctx, t)

	// The same pending user, invited by a token whose lifetime has run out.
	expired, _, err := f.c.inviteStore.Create(ctx, qrdbsdk.SystemTenantID, invite.userID, -time.Minute)
	require.NoError(t, err)

	activation := f.activation()
	_, err = activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, expired)
	require.ErrorIs(t, err, iam.ErrInviteExpired, "an expired invite opens no ceremony")

	// A ceremony opened by the live invite cannot be closed with the expired one.
	options, err := activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.NoError(t, err)
	_, err = activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, expired, f.key.register(options))
	require.ErrorIs(t, err, iam.ErrInviteExpired, "an expired invite activates nothing")
	f.requirePending(ctx, t, qrdbsdk.SystemTenantID, invite.userID, invite.token)
}

func TestTenantProvisioner_PasskeyInviteRefusesReplayedToken(t *testing.T) {
	f := newPasskeyFixtureOn(t, newPrincipalNode)
	ctx := context.Background()
	invite := f.inviteSystemUser(ctx, t)

	activation := f.activation()
	options, err := activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.NoError(t, err)
	_, err = activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, f.key.register(options))
	require.NoError(t, err)

	_, err = activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.ErrorIs(t, err, iam.ErrInviteUsed, "a redeemed invite opens no second ceremony")
	_, err = activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, f.key.register(options))
	require.ErrorIs(t, err, iam.ErrInviteUsed, "a redeemed invite cannot be replayed to enroll again")
}

func TestTenantProvisioner_PasskeyInviteUnfinishedLeavesAdminPending(t *testing.T) {
	t.Run("ceremony begun and never finished", func(t *testing.T) {
		f := newPasskeyFixtureOn(t, newPrincipalNode)
		ctx := context.Background()
		invite := f.inviteSystemUser(ctx, t)

		_, err := f.activation().ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
		require.NoError(t, err)
		f.requirePending(ctx, t, qrdbsdk.SystemTenantID, invite.userID, invite.token)
	})

	t.Run("finish without a ceremony", func(t *testing.T) {
		f := newPasskeyFixtureOn(t, newPrincipalNode)
		ctx := context.Background()
		invite := f.inviteSystemUser(ctx, t)

		_, err := f.activation().ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, map[string]interface{}{})
		var noSession *iam.WebAuthnFactoryConfigError
		require.ErrorAs(t, err, &noSession, "no ceremony was opened for this invite")
		f.requirePending(ctx, t, qrdbsdk.SystemTenantID, invite.userID, invite.token)
	})

	t.Run("passkey refused by the hardware gate", func(t *testing.T) {
		f := newPasskeyFixtureOn(t, newPrincipalNode)
		ctx := context.Background()
		invite := f.inviteSystemUser(ctx, t)

		// Attested by a vendor root the deployment does not trust, so it is
		// not proven hardware and cannot become an administrative credential.
		untrusted, _ := newPasskeyAuthenticator(t)
		activation := f.activation()
		options, err := activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
		require.NoError(t, err)
		_, err = activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, untrusted.register(options))
		require.Error(t, err)
		f.requirePending(ctx, t, qrdbsdk.SystemTenantID, invite.userID, invite.token)
	})
}

// TestTenantProvisioner_AuthNodePasskeyInviteRefusedForTenant runs the
// refusal on a node with a service principal, the node a deployment boots:
// a non-system tenant's passkey activation is refused with the typed error
// until go-quicbaas #1816, while its invite lookup and PIV activation, which
// sign nobody in through the refused path, keep working.
func TestTenantProvisioner_AuthNodePasskeyInviteRefusedForTenant(t *testing.T) {
	f := newPasskeyFixtureOn(t, newPrincipalNode)
	_, err := f.c.tenantManager.CreateTenant(context.Background(), iam.CreateTenantOptions{
		ID:            provisionTenantID,
		Name:          "Acme",
		IsolationType: qrdbsdk.TenantIsolationPrefix,
	})
	require.NoError(t, err)
	// Every call below names the tenant, as the transports' tenant routing does.
	ctx := qrdbsdk.ContextWithTenant(context.Background(), provisionTenantID)
	invite := f.inviteUser(ctx, t, provisionTenantID)

	activation := f.activation()
	var refused *sdkerrors.TenantLoginRefusedError
	_, err = activation.ActivateFIDO2Begin(ctx, provisionTenantID, invite.token)
	require.ErrorAs(t, err, &refused, "the passkey ceremony is refused until #1816")
	_, err = activation.ActivateFIDO2Finish(ctx, provisionTenantID, invite.token, map[string]interface{}{})
	require.ErrorAs(t, err, &refused, "the passkey enrollment is refused until #1816")
	f.requirePending(ctx, t, provisionTenantID, invite.userID, invite.token)

	meta, err := activation.GetInvite(ctx, provisionTenantID, invite.token)
	require.NoError(t, err, "the tenant's invite lookup is not refused")
	assert.Equal(t, invite.userID, meta.UserID)

	leafPEM := f.enablePIVActivation(t)
	_, err = activation.ActivatePIV(ctx, provisionTenantID, invite.token, leafPEM)
	require.NoError(t, err, "the tenant's PIV activation is not refused")
	user, err := f.c.identityStore.GetUser(ctx, provisionTenantID, invite.userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle)
}

// enablePIVActivation wires a PIV activator trusting a fresh root into the
// fixture's components and returns a client certificate that root issued.
func (f *passkeyFixture) enablePIVActivation(t *testing.T) []byte {
	t.Helper()
	roots, leafPEM := newPIVChain(t)
	claims, err := iamtest.NewMemoryActivationClaims()
	require.NoError(t, err)
	activator, err := iam.NewPIVActivator(iam.PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      f.users,
		InviteStore:      f.c.inviteStore,
		LoginFactors:     f.c.certLoginFactors,
		ActivationClaims: claims,
	})
	require.NoError(t, err)
	f.c.pivActivator = activator
	return leafPEM
}

// passkeyInvite is a pending user and the one-time invite that activates it.
type passkeyInvite struct {
	userID uint64
	token  string
}

// inviteSystemUser creates a pending system-tenant user with its invite, the
// tenant whose passkey activation runs on this release (go-quicbaas #1816).
func (f *passkeyFixture) inviteSystemUser(ctx context.Context, t *testing.T) passkeyInvite {
	t.Helper()
	return f.inviteUser(ctx, t, qrdbsdk.SystemTenantID)
}

// inviteUser creates a pending user in tenantID with its invite.
func (f *passkeyFixture) inviteUser(ctx context.Context, t *testing.T, tenantID string, roleIDs ...string) passkeyInvite {
	t.Helper()
	user, token, _, err := f.c.identityStore.CreateInvitedUser(ctx, tenantID, iam.CreateUserOptions{
		Email:    "invitee@example.com",
		Username: "invitee",
		Roles:    roleIDs,
	}, f.c.inviteStore, time.Hour)
	require.NoError(t, err)
	return passkeyInvite{userID: user.ID, token: token}
}

// requirePending asserts the invited admin is still pending and its invite
// is still redeemable: an activation that did not finish consumed nothing.
func (f *passkeyFixture) requirePending(ctx context.Context, t *testing.T, tenantID string, userID uint64, token string) {
	t.Helper()
	user, err := f.c.identityStore.GetUser(ctx, tenantID, userID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecyclePending, user.Lifecycle, "the user stays pending")
	_, err = f.c.inviteStore.Lookup(ctx, tenantID, token)
	require.NoError(t, err, "the invite stays redeemable")
}
