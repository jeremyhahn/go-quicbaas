// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

//go:build integration && quic

// QUIC transport authorization enforcement, rewritten onto the unified
// platform-token wiring (authz unification Stage 1.3b): the QUIC PEP no longer
// consumes an AuthManager + AuthorizationService pair — the ONLY enforcement
// seam is quic.WithHandlerValidatorAuthorizer, satisfied in production by the
// SDK *platformtoken.Validator. These tests install the REAL validator with a
// static in-memory JWKS and drive the handler with genuinely signed RFC 9068
// at+jwt access tokens, so the full production decision runs end-to-end:
// signature / iss / aud / exp / typ verification, the offline scp -> permission
// decision against each opcode's qrdb-canonical permission, the per-tenant
// liveness (revocation) check, and the error -> QUIC status byte mapping.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

const (
	quicTestIssuer   = "https://issuer.quicbaas.test"
	quicTestAudience = "api://quicbaas"
	quicTestKID      = "quic-authz-kid-1"
)

// revocableLiveness is an in-memory platformtoken.LivenessChecker keyed by the
// token jti. It stands in for the qrdb-backed liveness backend so the revoked
// -token regression (formerly authManager.RevokeToken) still runs against the
// real validator's online liveness path.
type revocableLiveness struct {
	mu      sync.RWMutex
	revoked map[string]bool
}

// newRevocableLiveness returns an empty (all tokens live) checker.
func newRevocableLiveness() *revocableLiveness {
	return &revocableLiveness{revoked: make(map[string]bool)}
}

// CheckLiveness reports the token active unless its jti has been revoked.
func (r *revocableLiveness) CheckLiveness(_ context.Context, _ string, claims *platformtoken.Claims) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.revoked[claims.ID], nil
}

// Revoke marks the token jti inactive; subsequent liveness checks deny it.
func (r *revocableLiveness) Revoke(jti string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revoked[jti] = true
}

// quicTokenSpec describes a platform access token to mint for a test case.
// Zero values default to a well-formed token accepted by the test validator.
type quicTokenSpec struct {
	subject  string
	username string
	tenant   string
	scopes   []string
	roles    []string
	// jti sets the token id used by the revocable liveness checker.
	jti string
	// jkt, when set, emits a cnf.jkt sender-constraint binding.
	jkt string
	// typ overrides the JOSE typ header. Empty defaults to RFC 9068 "at+jwt".
	typ string
	// expiresIn overrides the 1h expiry (negative mints an expired token).
	expiresIn time.Duration
	// issuer / audience override the trusted values (mismatch tests).
	issuer   string
	audience string
	// signWith overrides the issuer key (wrong-key signature test).
	signWith *ecdsa.PrivateKey
}

// testQUICContext provides the QUIC authorization test infrastructure: a real
// platformtoken.Validator over a static JWKS, a revocable liveness backend in
// online mode (revocation takes effect immediately, no cache), and the QUIC
// handler wired exactly as lifecycle wires it under RequireAuthz=true.
type testQUICContext struct {
	ctx      context.Context
	cancel   context.CancelFunc
	key      *ecdsa.PrivateKey
	liveness *revocableLiveness
	handler  *quic.Handler
}

// newTestQUICContext builds the validator + handler pair. Extra validator
// options are not needed for the common path; specialised tests construct
// their own validator via newQUICHandlerWithValidator.
func newTestQUICContext(t *testing.T) *testQUICContext {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	liveness := newRevocableLiveness()
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:          quicTestIssuer,
		Audience:        quicTestAudience,
		JWKS:            quicTestJWKS(key),
		LivenessChecker: liveness,
		// Online mode: every request live-checks, so Revoke is immediate.
		ModeResolver: func(string) platformtoken.AuthzMode {
			return platformtoken.AuthzModeOnline
		},
		ReplayCache: dpop.NewMemoryReplayCache(nil),
	})
	require.NoError(t, err)

	return &testQUICContext{
		ctx:      ctx,
		cancel:   cancel,
		key:      key,
		liveness: liveness,
		handler:  newQUICHandlerWithValidator(t, validator),
	}
}

// newQUICHandlerWithValidator wires a QUIC handler with nil services (every
// service handler is nil-guarded to StatusUnavailable) and the supplied
// validator as the single authorization seam, mirroring the production wiring
// in quic.Server.SetValidatorEnforcement.
func newQUICHandlerWithValidator(t *testing.T, v quic.ValidatorAuthorizer) *quic.Handler {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return quic.NewHandler(nil, nil, nil, nil, log,
		quic.WithHandlerValidatorAuthorizer(v))
}

// quicTestJWKS publishes the issuer public key as an in-memory JWKS.
func quicTestJWKS(key *ecdsa.PrivateKey) *jose.JSONWebKeySet {
	return &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       key.Public(),
		KeyID:     quicTestKID,
		Algorithm: "ES256",
		Use:       "sig",
	}}}
}

// mintToken signs a platform access token exactly as the issuer emits it
// (ES256, kid header, RFC 9068 at+jwt typ, scp claim with the qrdb-resolved
// effective permissions).
func (tc *testQUICContext) mintToken(t *testing.T, spec quicTokenSpec) string {
	t.Helper()

	now := time.Now()
	expiresIn := spec.expiresIn
	if expiresIn == 0 {
		expiresIn = time.Hour
	}
	issuer := spec.issuer
	if issuer == "" {
		issuer = quicTestIssuer
	}
	audience := spec.audience
	if audience == "" {
		audience = quicTestAudience
	}
	subject := spec.subject
	if subject == "" {
		subject = "user-quic"
	}
	jti := spec.jti
	if jti == "" {
		jti = "jti-" + subject
	}

	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": []string{audience},
		"sub": subject,
		"exp": now.Add(expiresIn).Unix(),
		"iat": now.Unix(),
		"jti": jti,
	}
	if spec.username != "" {
		claims["name"] = spec.username
	}
	if spec.tenant != "" {
		claims["tenant_id"] = spec.tenant
	}
	if len(spec.scopes) > 0 {
		claims["scp"] = spec.scopes
	}
	if len(spec.roles) > 0 {
		claims["roles"] = spec.roles
	}
	if spec.jkt != "" {
		claims["cnf"] = map[string]interface{}{"jkt": spec.jkt}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = quicTestKID
	if spec.typ != "" {
		token.Header["typ"] = spec.typ
	} else {
		token.Header["typ"] = "at+jwt"
	}

	signWith := spec.signWith
	if signWith == nil {
		signWith = tc.key
	}
	signed, err := token.SignedString(signWith)
	require.NoError(t, err)
	return signed
}

// assertNotDenied asserts a response was neither unauthenticated nor
// permission-denied (nil services may still answer StatusUnavailable).
func assertNotDenied(t *testing.T, resp *quic.Response, msgAndArgs ...interface{}) {
	t.Helper()
	require.NotNil(t, resp)
	assert.NotEqual(t, quic.StatusUnauthorized, resp.Status, msgAndArgs...)
	assert.NotEqual(t, quic.StatusPermissionDenied, resp.Status, msgAndArgs...)
}

// TestQUICPublicOperationsNoAuth verifies public opcodes bypass the validator
// gate entirely: no token is required and no authz status is returned.
func TestQUICPublicOperationsNoAuth(t *testing.T) {
	tc := newTestQUICContext(t)

	publicOperations := []struct {
		opcode byte
		name   string
	}{
		{quic.OpHealth, "Health"},
		{quic.OpLiveness, "Liveness"},
		{quic.OpReadiness, "Readiness"},
		{quic.OpAuthBeginLogin, "AuthBeginLogin"},
		{quic.OpAuthFinishLogin, "AuthFinishLogin"},
	}

	for _, op := range publicOperations {
		t.Run(op.name+"_is_public", func(t *testing.T) {
			resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  op.opcode,
				Token:   "", // No token
				Payload: []byte{},
			})
			assertNotDenied(t, resp, "public operation %s must not require auth", op.name)
		})
	}
}

// TestQUICProtectedOperationsRequireToken verifies every protected opcode is
// rejected with StatusUnauthorized (0x03) when no bearer token is presented.
func TestQUICProtectedOperationsRequireToken(t *testing.T) {
	tc := newTestQUICContext(t)

	protectedOperations := []struct {
		opcode byte
		name   string
	}{
		{quic.OpGet, "Get"},
		{quic.OpPut, "Put"},
		{quic.OpDelete, "Delete"},
		{quic.OpList, "List"},
		{quic.OpBatchGet, "BatchGet"},
		{quic.OpBatchPut, "BatchPut"},
		{quic.OpTransaction, "Transaction"},
		{quic.OpQuery, "Query"},
		{quic.OpClusterStatus, "ClusterStatus"},
	}

	for _, op := range protectedOperations {
		t.Run(op.name+"_requires_token", func(t *testing.T) {
			resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  op.opcode,
				Token:   "",
				Payload: []byte{},
			})
			require.NotNil(t, resp)
			assert.Equal(t, quic.StatusUnauthorized, resp.Status,
				"protected operation %s must return StatusUnauthorized without token", op.name)
			assert.Equal(t, byte(0x03), resp.Status, "StatusUnauthorized should be 0x03")
			assert.Contains(t, string(resp.Payload), "authentication required")
		})
	}
}

// TestQUICInvalidTokenRejected drives the real validator with malformed and
// mis-issued tokens: garbage, wrong signing key, expired, wrong issuer, wrong
// audience, and an OIDC "JWT" typ (RFC 9068 token-type confusion). Every case
// must fail closed with StatusUnauthorized and the opaque deny payload.
func TestQUICInvalidTokenRejected(t *testing.T) {
	tc := newTestQUICContext(t)

	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	adminScopes := []string{"*:*"}
	tests := []struct {
		name  string
		token string
	}{
		{"garbage_token", "invalid-token-12345"},
		{"wrong_signing_key", tc.mintToken(t, quicTokenSpec{scopes: adminScopes, signWith: wrongKey})},
		{"expired_token", tc.mintToken(t, quicTokenSpec{scopes: adminScopes, expiresIn: -time.Hour})},
		{"wrong_issuer", tc.mintToken(t, quicTokenSpec{scopes: adminScopes, issuer: "https://evil.example.com"})},
		{"wrong_audience", tc.mintToken(t, quicTokenSpec{scopes: adminScopes, audience: "api://other-service"})},
		{"id_token_typ_confusion", tc.mintToken(t, quicTokenSpec{scopes: adminScopes, typ: "JWT"})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  quic.OpGet,
				Token:   tt.token,
				Payload: []byte{},
			})
			require.NotNil(t, resp)
			assert.Equal(t, quic.StatusUnauthorized, resp.Status)
			assert.Contains(t, string(resp.Payload), "invalid token")
		})
	}
}

// TestQUICScopeDeniedIsPermissionDenied verifies an authenticated token whose
// scp set does not admit the opcode's required permission is rejected with
// StatusPermissionDenied (0x07), the ScopeDeniedError mapping.
func TestQUICScopeDeniedIsPermissionDenied(t *testing.T) {
	tc := newTestQUICContext(t)

	// Read-only grant attempting a write (OpPut requires kv:put).
	token := tc.mintToken(t, quicTokenSpec{
		subject: "readonly-quic",
		scopes:  []string{"kv:get"},
	})

	resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
		Opcode:  quic.OpPut,
		Token:   token,
		Payload: []byte{},
	})

	require.NotNil(t, resp)
	assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	assert.Equal(t, byte(0x07), resp.Status, "StatusPermissionDenied should be 0x07")
	assert.Contains(t, string(resp.Payload), "permission denied")
}

// TestQUICAdminWildcardScope verifies the full wildcard grant "*:*" (a
// qrdb-resolved admin) admits every mapped opcode, matching the PDP's
// whole-segment wildcard semantics.
func TestQUICAdminWildcardScope(t *testing.T) {
	tc := newTestQUICContext(t)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "admin-quic",
		scopes:  []string{"*:*"},
	})

	operations := []struct {
		opcode byte
		name   string
	}{
		{quic.OpGet, "Get"},
		{quic.OpPut, "Put"},
		{quic.OpDelete, "Delete"},
		{quic.OpList, "List"},
		{quic.OpClusterStatus, "ClusterStatus"},
		{quic.OpAddNode, "AddNode"},
	}

	for _, op := range operations {
		t.Run("admin_can_"+op.name, func(t *testing.T) {
			resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  op.opcode,
				Token:   token,
				Payload: []byte{},
			})
			assertNotDenied(t, resp, "admin wildcard must admit %s", op.name)
		})
	}
}

// TestQUICReadWriteScopes verifies a kv read/write/delete grant set admits the
// corresponding KV opcodes but not cluster administration.
func TestQUICReadWriteScopes(t *testing.T) {
	tc := newTestQUICContext(t)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "readwrite-quic",
		scopes:  []string{"kv:get", "kv:put", "kv:delete"},
	})

	t.Run("can_read", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpGet, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("can_write", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpPut, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("can_delete", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpDelete, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("cannot_admin_cluster", func(t *testing.T) {
		// OpAddNode requires admin:add_member.
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpAddNode, Token: token, Payload: []byte{}})
		require.NotNil(t, resp)
		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICReadOnlyScope verifies a kv:get-only grant admits reads and denies
// writes and deletes.
func TestQUICReadOnlyScope(t *testing.T) {
	tc := newTestQUICContext(t)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "readonly-quic-2",
		scopes:  []string{"kv:get"},
	})

	t.Run("can_read", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpGet, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("cannot_write", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpPut, Token: token, Payload: []byte{}})
		require.NotNil(t, resp)
		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_delete", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpDelete, Token: token, Payload: []byte{}})
		require.NotNil(t, resp)
		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICResourceWildcardScope verifies the resource wildcard "kv:*" admits
// every kv action but no other resource, matching qrdb's whole-segment
// wildcard semantics (no glob prefixes).
func TestQUICResourceWildcardScope(t *testing.T) {
	tc := newTestQUICContext(t)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "kv-wildcard-quic",
		scopes:  []string{"kv:*"},
	})

	t.Run("wildcard_admits_kv_read", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpGet, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("wildcard_admits_kv_write", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpPut, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("wildcard_does_not_leak_across_resources", func(t *testing.T) {
		// OpKMSGetKey requires keys:get, outside the kv:* grant.
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpKMSGetKey, Token: token, Payload: []byte{}})
		require.NotNil(t, resp)
		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICCustomKMSScopes models a narrow kms-operator grant set: keys:get and
// keys:create only. It replaces the old custom-role test with the equivalent
// scp-set decision.
func TestQUICCustomKMSScopes(t *testing.T) {
	tc := newTestQUICContext(t)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "kms-only-quic",
		scopes:  []string{"keys:get", "keys:create"},
	})

	t.Run("can_read_kms", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpKMSGetKey, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("can_generate_kms_key", func(t *testing.T) {
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpKMSGenerateKey, Token: token, Payload: []byte{}})
		assertNotDenied(t, resp)
	})

	t.Run("cannot_delete_kms_key", func(t *testing.T) {
		// OpKMSDeleteKey requires keys:delete.
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpKMSDeleteKey, Token: token, Payload: []byte{}})
		require.NotNil(t, resp)
		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})

	t.Run("cannot_access_kv", func(t *testing.T) {
		// OpGet requires kv:get.
		resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{Opcode: quic.OpGet, Token: token, Payload: []byte{}})
		require.NotNil(t, resp)
		assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
	})
}

// TestQUICOpcodePermissionEnforcement behaviorally pins the opcode ->
// qrdb-canonical permission mapping the PEP enforces: for each opcode a token
// granted EXACTLY the required permission is admitted and a token granted an
// unrelated permission is denied. A mapping drift in either direction fails
// this table.
func TestQUICOpcodePermissionEnforcement(t *testing.T) {
	tc := newTestQUICContext(t)

	tests := []struct {
		name         string
		opcode       byte
		payload      []byte
		requiredPerm string
	}{
		// KV
		{"Get_kv_get", quic.OpGet, []byte{}, "kv:get"},
		{"Put_kv_put", quic.OpPut, []byte{}, "kv:put"},
		{"Delete_kv_delete", quic.OpDelete, []byte{}, "kv:delete"},
		{"List_kv_list", quic.OpList, []byte{}, "kv:list"},
		{"Query_kv_range", quic.OpQuery, []byte{}, "kv:range"},

		// Batch / transaction
		{"BatchGet_batch_execute", quic.OpBatchGet, []byte{}, "batch:execute"},
		{"BatchPut_batch_execute", quic.OpBatchPut, []byte{}, "batch:execute"},
		{"Transaction_transaction_execute", quic.OpTransaction, []byte{}, "transaction:execute"},

		// Cluster admin
		{"ClusterStatus_admin_status", quic.OpClusterStatus, []byte{}, "admin:status"},
		{"GetLeader_admin_status", quic.OpGetLeader, []byte{}, "admin:status"},
		{"AddNode_admin_add_member", quic.OpAddNode, []byte{}, "admin:add_member"},
		{"RemoveNode_admin_remove_member", quic.OpRemoveNode, []byte{}, "admin:remove_member"},

		// Backup
		{"ListBackups_backup_list", quic.OpListBackups, []byte{}, "backup:list"},
		{"CreateBackup_backup_create", quic.OpCreateBackup, []byte{}, "backup:create"},
		{"DeleteBackup_backup_delete", quic.OpDeleteBackup, []byte{}, "backup:delete"},

		// KMS -> keys
		{"KMSGetKey_keys_get", quic.OpKMSGetKey, []byte{}, "keys:get"},
		{"KMSListKeys_keys_list", quic.OpKMSListKeys, []byte{}, "keys:list"},
		{"KMSGenerateKey_keys_create", quic.OpKMSGenerateKey, []byte{}, "keys:create"},
		{"KMSDeleteKey_keys_delete", quic.OpKMSDeleteKey, []byte{}, "keys:delete"},

		// Secrets -> credentials
		{"SecretsGet_credentials_retrieve", quic.OpSecretsGet, []byte{}, "credentials:retrieve"},
		{"SecretsList_credentials_list", quic.OpSecretsList, []byte{}, "credentials:list"},
		{"SecretsStore_credentials_store", quic.OpSecretsStore, []byte{}, "credentials:store"},
		{"SecretsDelete_credentials_delete", quic.OpSecretsDelete, []byte{}, "credentials:delete"},

		// Crypto -> keys
		{"CryptoEncrypt_keys_encrypt", quic.OpCryptoEncrypt, []byte{}, "keys:encrypt"},
		{"CryptoDecrypt_keys_decrypt", quic.OpCryptoDecrypt, []byte{}, "keys:decrypt"},

		// Time-series
		{"TSQuery_timeseries_query", quic.OpTSQuery, []byte{}, "timeseries:query"},
		{"TSWritePoint_timeseries_write_point", quic.OpTSWritePoint, []byte{}, "timeseries:write_point"},
		{"TSDeleteSeries_timeseries_delete_series", quic.OpTSDeleteSeries, []byte{}, "timeseries:delete_series"},

		// Ceremony dispatch authorizes at the surface level.
		{"Ceremony_ceremony_status", quic.OpCeremony, []byte{}, "ceremony:status"},

		// Barrier dispatch is sub-op-aware: payload[0] selects the permission.
		{"BarrierStatus_tenant_barrier_status", quic.OpBarrier, []byte{quic.SubOpBarrierStatus}, "tenant_barrier:status"},
		{"BarrierUnseal_tenant_barrier_unseal", quic.OpBarrier, []byte{quic.SubOpBarrierUnseal}, "tenant_barrier:unseal"},
		{"BarrierRekey_seal_rekey", quic.OpBarrier, []byte{quic.SubOpBarrierRekey}, "seal:rekey"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			granted := tc.mintToken(t, quicTokenSpec{
				subject: "exact-grant",
				jti:     "jti-exact-" + tt.name,
				scopes:  []string{tt.requiredPerm},
			})
			resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  tt.opcode,
				Token:   granted,
				Payload: tt.payload,
			})
			assertNotDenied(t, resp, "scope %s must admit opcode 0x%02X", tt.requiredPerm, tt.opcode)

			denied := tc.mintToken(t, quicTokenSpec{
				subject: "unrelated-grant",
				jti:     "jti-unrelated-" + tt.name,
				scopes:  []string{"nothing:none"},
			})
			resp = tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  tt.opcode,
				Token:   denied,
				Payload: tt.payload,
			})
			require.NotNil(t, resp)
			assert.Equal(t, quic.StatusPermissionDenied, resp.Status,
				"unrelated scope must be denied for opcode 0x%02X (requires %s)", tt.opcode, tt.requiredPerm)
		})
	}
}

// TestQUICDefaultDenyUnmappedSurface verifies the gate is default-deny: an
// opcode (or barrier sub-op) with no qrdb permission mapping is rejected with
// StatusPermissionDenied even for a full-wildcard admin token, and the
// validator is never given a chance to admit it.
func TestQUICDefaultDenyUnmappedSurface(t *testing.T) {
	tc := newTestQUICContext(t)

	admin := tc.mintToken(t, quicTokenSpec{
		subject: "admin-unmapped",
		scopes:  []string{"*:*"},
	})

	tests := []struct {
		name    string
		opcode  byte
		payload []byte
	}{
		{"unmapped_opcode", 0xFE, []byte{}},
		{"barrier_empty_payload", quic.OpBarrier, []byte{}},
		{"barrier_unknown_subop", quic.OpBarrier, []byte{0xEE}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
				Opcode:  tt.opcode,
				Token:   admin,
				Payload: tt.payload,
			})
			require.NotNil(t, resp)
			assert.Equal(t, quic.StatusPermissionDenied, resp.Status)
			assert.Contains(t, string(resp.Payload), "permission denied")
		})
	}
}

// TestQUICRevokedTokenRejected verifies the online liveness path: a valid
// token is admitted, then revoked at the liveness backend, then rejected with
// StatusUnauthorized (TokenInactiveError) on the very next request.
func TestQUICRevokedTokenRejected(t *testing.T) {
	tc := newTestQUICContext(t)

	const jti = "jti-revocation-test"
	token := tc.mintToken(t, quicTokenSpec{
		subject: "revoke-quic",
		jti:     jti,
		scopes:  []string{"*:*"},
	})

	req := &quic.Request{Opcode: quic.OpGet, Token: token, Payload: []byte{}}

	resp := tc.handler.HandleRequest(tc.ctx, req)
	assertNotDenied(t, resp, "token must be admitted before revocation")

	tc.liveness.Revoke(jti)

	resp = tc.handler.HandleRequest(tc.ctx, req)
	require.NotNil(t, resp)
	assert.Equal(t, quic.StatusUnauthorized, resp.Status,
		"revoked token must be rejected by the online liveness check")
}

// TestQUICSenderConstrainedTokenRejected verifies a cnf.jkt-bound (DPoP
// sender-constrained) token cannot be used over QUIC: the transport carries no
// DPoP proof, so possession cannot be proven and the validator rejects the
// request with StatusUnauthorized. The check must not be skipped.
func TestQUICSenderConstrainedTokenRejected(t *testing.T) {
	tc := newTestQUICContext(t)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "dpop-quic",
		scopes:  []string{"*:*"},
		jkt:     "fake-jkt-thumbprint-value",
	})

	resp := tc.handler.HandleRequest(tc.ctx, &quic.Request{
		Opcode:  quic.OpGet,
		Token:   token,
		Payload: []byte{},
	})

	require.NotNil(t, resp)
	assert.Equal(t, quic.StatusUnauthorized, resp.Status,
		"cnf.jkt token without a DPoP proof must be rejected over QUIC")
}

// TestQUICMisconfiguredLivenessFailsClosed verifies a server misconfiguration
// (default hybrid mode with no liveness checker) DENIES the request with
// StatusInternalError rather than silently admitting it.
func TestQUICMisconfiguredLivenessFailsClosed(t *testing.T) {
	tc := newTestQUICContext(t)

	// Validator without a liveness checker; nil ModeResolver defaults every
	// tenant to hybrid, which requires one.
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:   quicTestIssuer,
		Audience: quicTestAudience,
		JWKS:     quicTestJWKS(tc.key),
	})
	require.NoError(t, err)
	handler := newQUICHandlerWithValidator(t, validator)

	token := tc.mintToken(t, quicTokenSpec{
		subject: "misconfig-quic",
		scopes:  []string{"*:*"},
	})

	resp := handler.HandleRequest(tc.ctx, &quic.Request{
		Opcode:  quic.OpGet,
		Token:   token,
		Payload: []byte{},
	})

	require.NotNil(t, resp)
	assert.Equal(t, quic.StatusInternalError, resp.Status,
		"missing liveness checker must fail closed as an internal error, never an allow")
}
