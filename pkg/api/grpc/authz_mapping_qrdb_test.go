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

package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"

	// Blank import registers the qrdb.v1 proto file descriptors in the global
	// protoregistry so the guard tests can enumerate qrdb.v1.QRDBService and
	// qrdb.v1.UserAuthFactorService methods from the descriptors — the same
	// SDK package pkg/server uses to register the services, so the guard and
	// the registration can never drift. The generated grpc.ServiceDesc values
	// live in qrdb's pb package, which quicbaas must not import directly (SDK
	// boundary).
	_ "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// qrdbServiceNames are the qrdb.v1 proto services quicbaasd registers on its
// gRPC (and Unix-over-gRPC) server via qrdbsdk.RegisterGRPCService
// (pkg/server/grpc_qrdb_service.go registerQRDBService).
var qrdbServiceNames = []string{
	"qrdb.v1.QRDBService",
	"qrdb.v1.UserAuthFactorService",
}

// qrdbUserSurfaceMethods are the QRDBService user-management methods served by
// the WithRegisterUserService servicer proxy. They are enumerated explicitly
// because the user family shares no method-name prefix; a NEW qrdb user-surface
// RPC must be added here AND mapped in grpcMethodPermissions.
var qrdbUserSurfaceMethods = map[string]bool{
	"CreateUser":              true,
	"GetUser":                 true,
	"GetUserByUsername":       true,
	"GetUserByCertThumbprint": true,
	"AddCertBinding":          true,
	"UpdateUser":              true,
	"DeleteUser":              true,
	"ListUsers":               true,
	"EnableUser":              true,
	"DisableUser":             true,
}

// qrdbWiredMethod reports whether a qrdb.v1.QRDBService method belongs to a
// family whose servicer quicbaasd wires in buildQRDBRegisterOptions (barrier,
// ceremony, credential, user) or to the shared health/status surface. Every
// other QRDBService method dispatches to codes.Unimplemented server-side and
// must remain default-denied by the authz gate.
func qrdbWiredMethod(name string) bool {
	if strings.HasPrefix(name, "Barrier") ||
		strings.HasPrefix(name, "Ceremony") ||
		strings.HasPrefix(name, "Credential") {
		return true
	}
	return qrdbUserSurfaceMethods[name] || name == "Health" || name == "Status"
}

// qrdbServiceDescriptor resolves a qrdb.v1 service descriptor from the global
// protoregistry (populated by the blank qrdb SDK import).
func qrdbServiceDescriptor(t *testing.T, name string) protoreflect.ServiceDescriptor {
	t.Helper()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name))
	require.NoError(t, err, "qrdb.v1 service descriptor %q must be registered", name)
	sd, ok := d.(protoreflect.ServiceDescriptor)
	require.True(t, ok, "%q is not a service descriptor", name)
	return sd
}

// TestQRDBGRPCPermissions_EveryRegisteredMethodMapped is the qrdb.v1
// counterpart of TestGRPCPermissions_EveryMethodMapped. It enumerates every
// method of the qrdb.v1 services quicbaasd registers, straight from the proto
// service descriptors, and asserts:
//
//   - every method of a WIRED family (barrier / ceremony / credential / user /
//     user-auth-factor / health / status) is exactly one of: mapped in
//     grpcMethodPermissions, declared public, or marked identity-gated;
//   - every method OUTSIDE the wired families stays unmapped, non-public, and
//     unmarked, pinning the default-deny posture for the qrdb.v1 surfaces the
//     broker does not serve (KV, lease, lock, vector, ...).
//
// A new qrdb barrier/ceremony/credential RPC therefore fails this test until a
// mapping (or an explicit identity-gated marking) is added.
func TestQRDBGRPCPermissions_EveryRegisteredMethodMapped(t *testing.T) {
	checkMethod := func(t *testing.T, fullMethod string, wired bool) {
		t.Helper()
		_, _, mapped := grpcResourceAndAction(fullMethod)
		public := isPublicGRPCMethod(fullMethod)
		gated := qrdbIdentityGatedMethods[fullMethod]

		if wired {
			states := 0
			for _, s := range []bool{mapped, public, gated} {
				if s {
					states++
				}
			}
			if states == 0 {
				t.Errorf("wired qrdb.v1 method %q is neither mapped, public, nor marked identity-gated; add a permission mapping", fullMethod)
			}
			if states > 1 {
				t.Errorf("qrdb.v1 method %q is in more than one of mapped/public/identity-gated (mapped=%v public=%v gated=%v); it must be exactly one", fullMethod, mapped, public, gated)
			}
			return
		}
		if mapped || public || gated {
			t.Errorf("unwired qrdb.v1 method %q must stay default-denied (mapped=%v public=%v gated=%v); the broker does not serve this family on the qrdb.v1 surface", fullMethod, mapped, public, gated)
		}
	}

	// qrdb.v1.QRDBService: wired families only.
	sd := qrdbServiceDescriptor(t, "qrdb.v1.QRDBService")
	for i := 0; i < sd.Methods().Len(); i++ {
		name := string(sd.Methods().Get(i).Name())
		checkMethod(t, "/qrdb.v1.QRDBService/"+name, qrdbWiredMethod(name))
	}

	// qrdb.v1.UserAuthFactorService: the entire service is wired
	// (WithRegisterUserAuthFactorService is always applied).
	sd = qrdbServiceDescriptor(t, "qrdb.v1.UserAuthFactorService")
	for i := 0; i < sd.Methods().Len(); i++ {
		name := string(sd.Methods().Get(i).Name())
		checkMethod(t, "/qrdb.v1.UserAuthFactorService/"+name, true)
	}
}

// TestQRDBGRPCPermissions_NoStaleEntries is the reverse guard: every
// "/qrdb.v1.*" key in grpcMethodPermissions, publicGRPCMethods, and
// qrdbIdentityGatedMethods must correspond to a real method on a registered
// qrdb.v1 service descriptor. This catches typos in the hand-written FullMethod
// strings (an entry with a typo would silently never match, leaving the real
// method default-denied) and entries left behind after a qrdb proto rename.
func TestQRDBGRPCPermissions_NoStaleEntries(t *testing.T) {
	known := make(map[string]bool)
	for _, svc := range qrdbServiceNames {
		sd := qrdbServiceDescriptor(t, svc)
		for i := 0; i < sd.Methods().Len(); i++ {
			known["/"+svc+"/"+string(sd.Methods().Get(i).Name())] = true
		}
	}

	check := func(source, key string) {
		if strings.HasPrefix(key, "/qrdb.v1.") && !known[key] {
			t.Errorf("%s entry %q does not correspond to any method on a registered qrdb.v1 service descriptor (typo or stale entry)", source, key)
		}
	}
	for key := range grpcMethodPermissions {
		check("grpcMethodPermissions", key)
	}
	for key := range publicGRPCMethods {
		check("publicGRPCMethods", key)
	}
	for key := range qrdbIdentityGatedMethods {
		check("qrdbIdentityGatedMethods", key)
	}
}

// TestGRPCResourceAndAction_QRDBMapped spot-checks one representative method
// per qrdb.v1 family against the qrdb-canonical vocabulary
// (go-qrdb/pkg/auth/middleware/mapping.go grpcPermissions and
// go-qrdb/pkg/permission/constants.go).
func TestGRPCResourceAndAction_QRDBMapped(t *testing.T) {
	cases := []struct {
		method   string
		resource string
		action   string
	}{
		// Barrier lifecycle.
		{"/qrdb.v1.QRDBService/BarrierInitialize", "seal", "bootstrap"},
		{"/qrdb.v1.QRDBService/BarrierUnseal", "seal", "unseal"},
		{"/qrdb.v1.QRDBService/BarrierSeal", "seal", "seal"},
		{"/qrdb.v1.QRDBService/BarrierStatus", "seal", "status"},
		{"/qrdb.v1.QRDBService/BarrierShamirInitialize", "seal", "initialize_shamir"},
		{"/qrdb.v1.QRDBService/BarrierShamirUnsealShare", "barrier", "unseal-share"},
		{"/qrdb.v1.QRDBService/BarrierShamirUnsealShares", "barrier", "unseal-share"},
		{"/qrdb.v1.QRDBService/BarrierRekey", "seal", "rekey"},
		{"/qrdb.v1.QRDBService/BarrierRootToken", "seal", "bootstrap"},
		{"/qrdb.v1.QRDBService/BarrierEnableEncryption", "seal", "enable_encryption"},
		{"/qrdb.v1.QRDBService/BarrierShamirSealVote", "seal", "submit_seal_vote"},
		{"/qrdb.v1.QRDBService/BarrierOfficerRegister", "seal", "officer_register"},
		{"/qrdb.v1.QRDBService/BarrierOfficerList", "seal", "officer_list"},
		{"/qrdb.v1.QRDBService/BarrierRecoveryGenerate", "seal", "generate_recovery"},
		{"/qrdb.v1.QRDBService/BarrierRecoveryRecover", "seal", "recover_with_keys"},
		{"/qrdb.v1.QRDBService/BarrierRecoveryDelete", "seal", "generate_recovery"},
		{"/qrdb.v1.QRDBService/BarrierRecoveryStatus", "seal", "status"},
		// Ceremony.
		{"/qrdb.v1.QRDBService/CeremonyInitialize", "ceremony", "initialize"},
		{"/qrdb.v1.QRDBService/CeremonyStatus", "ceremony", "status"},
		{"/qrdb.v1.QRDBService/CeremonyClaimCertBegin", "ceremony", "claim_cert"},
		{"/qrdb.v1.QRDBService/CeremonyClaimShareBegin", "ceremony", "claim_share_begin"},
		{"/qrdb.v1.QRDBService/CeremonyClaimShareComplete", "ceremony", "claim_share_complete"},
		{"/qrdb.v1.QRDBService/CeremonyEnrollExternalOfficer", "ceremony", "enroll_custodian"},
		{"/qrdb.v1.QRDBService/CeremonySignCSR", "ceremony", "sign_csr"},
		{"/qrdb.v1.QRDBService/CeremonySubmitUnsealShare", "barrier", "unseal-share"},
		// Credential.
		{"/qrdb.v1.QRDBService/CredentialStore", "credentials", "store"},
		{"/qrdb.v1.QRDBService/CredentialRetrieve", "credentials", "retrieve"},
		{"/qrdb.v1.QRDBService/CredentialRotate", "credentials", "rotate"},
		{"/qrdb.v1.QRDBService/CredentialGCExpired", "credentials", "gc_expired"},
		{"/qrdb.v1.QRDBService/CredentialRetrieveVersion", "credentials", "retrieve_version"},
		// User.
		{"/qrdb.v1.QRDBService/GetUser", "users", "read"},
		{"/qrdb.v1.QRDBService/GetUserByUsername", "users", "read"},
		{"/qrdb.v1.QRDBService/GetUserByCertThumbprint", "users", "get_by_cert_thumbprint"},
		{"/qrdb.v1.QRDBService/AddCertBinding", "users", "add_cert_binding"},
		{"/qrdb.v1.QRDBService/EnableUser", "users", "enable"},
		// Admin status.
		{"/qrdb.v1.QRDBService/Status", "admin", "status"},
		// UserAuthFactorService.
		{"/qrdb.v1.UserAuthFactorService/Add", "user_authfactors", "add"},
		{"/qrdb.v1.UserAuthFactorService/List", "user_authfactors", "list"},
		{"/qrdb.v1.UserAuthFactorService/Get", "user_authfactors", "get"},
		{"/qrdb.v1.UserAuthFactorService/Remove", "user_authfactors", "remove"},
		{"/qrdb.v1.UserAuthFactorService/Rotate", "user_authfactors", "rotate"},
		{"/qrdb.v1.UserAuthFactorService/Relabel", "user_authfactors", "relabel"},
		{"/qrdb.v1.UserAuthFactorService/Login", "user_authfactors", "login"},
	}
	for _, c := range cases {
		res, act, ok := grpcResourceAndAction(c.method)
		require.True(t, ok, c.method)
		assert.Equal(t, c.resource, res, c.method)
		assert.Equal(t, c.action, act, c.method)
	}
}

// TestQRDBHealth_Public asserts the qrdb.v1 health probe bypasses the authz
// gate entirely, matching the public posture of /quicbaas.v1.QuicBaaS/Health:
// the qrdb SDK's gRPC and Unix transports probe it while building a client,
// before any bearer token exists.
func TestQRDBHealth_Public(t *testing.T) {
	assert.True(t, isPublicGRPCMethod("/qrdb.v1.QRDBService/Health"))

	authorizer := &fakeValidatorAuthorizer{denyErr: &platformtoken.InvalidTokenError{Detail: "should never be called"}}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	resp, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/qrdb.v1.QRDBService/Health"},
		func(context.Context, interface{}) (interface{}, error) { return "healthy", nil })
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp)
	assert.Equal(t, "", authorizer.lastPerm, "validator must not be consulted for the public qrdb health probe")

	// Status is NOT public: it carries cluster state and requires admin:status.
	assert.False(t, isPublicGRPCMethod("/qrdb.v1.QRDBService/Status"))
}

// qrdbAdmitDenyCase drives one interceptor round trip per qrdb.v1 family: a
// token granting the exact required permission admits, and the SAME method
// with a token granting a sibling permission from the same family denies with
// PermissionDenied (offline scp deny).
type qrdbAdmitDenyCase struct {
	name      string
	method    string
	perm      string // exact required permission (colon join)
	wrongPerm string // same-family sibling that must NOT admit
}

// TestUnaryAuthorizationInterceptor_QRDBAdmitDeny runs the per-family spot
// checks through the real interceptor decision path.
func TestUnaryAuthorizationInterceptor_QRDBAdmitDeny(t *testing.T) {
	cases := []qrdbAdmitDenyCase{
		{"barrier_unseal", "/qrdb.v1.QRDBService/BarrierUnseal", "seal:unseal", "seal:status"},
		{"barrier_initialize", "/qrdb.v1.QRDBService/BarrierInitialize", "seal:bootstrap", "seal:unseal"},
		{"barrier_shamir_share", "/qrdb.v1.QRDBService/BarrierShamirUnsealShare", "barrier:unseal-share", "seal:unseal"},
		{"ceremony_initialize", "/qrdb.v1.QRDBService/CeremonyInitialize", "ceremony:initialize", "ceremony:status"},
		{"credential_rotate", "/qrdb.v1.QRDBService/CredentialRotate", "credentials:rotate", "credentials:list"},
		{"user_get_by_thumbprint", "/qrdb.v1.QRDBService/GetUserByCertThumbprint", "users:get_by_cert_thumbprint", "users:read"},
		{"user_get", "/qrdb.v1.QRDBService/GetUser", "users:read", "users:list"},
		{"authfactor_login", "/qrdb.v1.UserAuthFactorService/Login", "user_authfactors:login", "user_authfactors:list"},
		{"authfactor_add", "/qrdb.v1.UserAuthFactorService/Add", "user_authfactors:add", "user_authfactors:login"},
		{"admin_status", "/qrdb.v1.QRDBService/Status", "admin:status", "admin:health"},
	}

	for _, c := range cases {
		t.Run(c.name+"_allow", func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: c.perm,
				authzCtx: &platformtoken.AuthorizedContext{
					Subject:  "u1",
					Username: "so-admin",
					TenantID: "system",
					Roles:    []string{"admin"},
					Scopes:   []string{c.perm},
				},
			}
			interceptor := UnaryAuthorizationInterceptor(authorizer)

			var sawSubject bool
			resp, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: c.method},
				func(ctx context.Context, _ interface{}) (interface{}, error) {
					if subj, ok := auth.SubjectFromContext(ctx); ok && subj.Authenticated && subj.ID == "u1" {
						sawSubject = true
					}
					return "ok", nil
				})
			require.NoError(t, err, c.method)
			assert.Equal(t, "ok", resp)
			assert.True(t, sawSubject, "authenticated subject must be injected for %s", c.method)
			assert.Equal(t, c.perm, authorizer.lastPerm, "required permission join for %s", c.method)
		})

		t.Run(c.name+"_deny_wrong_perm", func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: c.wrongPerm,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "system"},
			}
			interceptor := UnaryAuthorizationInterceptor(authorizer)

			called := false
			_, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: c.method},
				func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
			require.Error(t, err, c.method)
			assert.Equal(t, codes.PermissionDenied, status.Code(err), c.method)
			assert.False(t, called, "handler must not run on scope deny for %s", c.method)
		})
	}
}

// TestUnaryAuthorizationInterceptor_QRDBMissingToken asserts a qrdb.v1 mapped
// method rejects a tokenless call Unauthenticated before the validator runs —
// the exact gap this mapping closes was "no qrdb permission mapping for gRPC
// method: /qrdb.v1.QRDBService/BarrierUnseal" WITH a valid token; without a
// token the deny must be Unauthenticated, not PermissionDenied.
func TestUnaryAuthorizationInterceptor_QRDBMissingToken(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{token: "good", grantedPerm: "seal:unseal"}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	_, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/qrdb.v1.QRDBService/BarrierUnseal"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, "", authorizer.lastPerm, "validator must not be consulted without a token")
}

// fakeAuthenticatingAuthorizer extends fakeValidatorAuthorizer with the
// scope-free Validate path (GRPCValidatorAuthenticator), mirroring
// *platformtoken.Validator which satisfies both seams.
type fakeAuthenticatingAuthorizer struct {
	fakeValidatorAuthorizer
	validateCalls int
	validateErr   error
}

func (f *fakeAuthenticatingAuthorizer) Validate(_ context.Context, token, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	f.validateCalls++
	if f.validateErr != nil {
		return nil, f.validateErr
	}
	if token != f.token {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature did not verify"}
	}
	return f.authzCtx, nil
}

// TestQRDBIdentityGatedMethods_MappingPosture pins the mapping posture for
// the qrdb IdentityGated share-claim verbs: never mapped to a registered
// permission (qrdb gates them by identity, not scope) and never public (qrdb
// requires an authenticated identity).
func TestQRDBIdentityGatedMethods_MappingPosture(t *testing.T) {
	require.NotEmpty(t, qrdbIdentityGatedMethods)
	for method := range qrdbIdentityGatedMethods {
		_, _, mapped := grpcResourceAndAction(method)
		assert.False(t, mapped, "%s must not be mapped: qrdb gates it by identity, not by a registered permission", method)
		assert.False(t, isPublicGRPCMethod(method), "%s must not be public: qrdb requires an authenticated identity", method)
	}
}

// TestQRDBIdentityGatedMethods_AdmitAuthenticated asserts the qrdb
// IdentityGated parity path: ANY authenticated principal (validated through
// the scope-free Validate seam) is admitted post-unseal, the handler runs
// with the reconstructed authenticated Subject on its context, and the
// scoped Authorize path is never consulted (no scp permission is demanded).
// The handler enforces the real credential (officer cert + nonce checks).
func TestQRDBIdentityGatedMethods_AdmitAuthenticated(t *testing.T) {
	require.NotEmpty(t, qrdbIdentityGatedMethods)
	for method := range qrdbIdentityGatedMethods {
		authorizer := &fakeAuthenticatingAuthorizer{
			fakeValidatorAuthorizer: fakeValidatorAuthorizer{
				token:    "good",
				authzCtx: &platformtoken.AuthorizedContext{Subject: "u1", Username: "officer-1", TenantID: "system"},
			},
		}
		interceptor := UnaryAuthorizationInterceptor(authorizer)

		called := false
		_, err := interceptor(ctxWithBearer("good"), nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			func(ctx context.Context, _ interface{}) (interface{}, error) {
				called = true
				subject, ok := auth.SubjectFromContext(ctx)
				require.True(t, ok, "authenticated subject must be injected for %s", method)
				assert.True(t, subject.Authenticated)
				assert.Equal(t, "u1", subject.ID)
				assert.Equal(t, "system", subject.TenantID)
				return nil, nil
			})
		require.NoError(t, err, method)
		assert.True(t, called, "handler must run for authenticated caller on %s", method)
		assert.Equal(t, 1, authorizer.validateCalls, "Validate consulted exactly once for %s", method)
		assert.Equal(t, "", authorizer.lastPerm, "scoped Authorize must not be consulted for identity-gated method %s", method)
	}
}

// TestQRDBIdentityGatedMethods_DenyAnonymous asserts an anonymous caller (no
// bearer token) is denied Unauthenticated before the validator is consulted.
func TestQRDBIdentityGatedMethods_DenyAnonymous(t *testing.T) {
	require.NotEmpty(t, qrdbIdentityGatedMethods)
	for method := range qrdbIdentityGatedMethods {
		authorizer := &fakeAuthenticatingAuthorizer{
			fakeValidatorAuthorizer: fakeValidatorAuthorizer{token: "good"},
		}
		interceptor := UnaryAuthorizationInterceptor(authorizer)

		called := false
		_, err := interceptor(context.Background(), nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
		require.Error(t, err, method)
		assert.Equal(t, codes.Unauthenticated, status.Code(err), method)
		assert.False(t, called, "handler must not run for anonymous caller on %s", method)
		assert.Equal(t, 0, authorizer.validateCalls, "validator must not be consulted without a token on %s", method)
	}
}

// TestQRDBIdentityGatedMethods_DenyInvalidToken asserts a caller presenting a
// token the validator rejects is denied Unauthenticated — validation errors
// on the identity-gated path map through validatorGRPCStatus like every
// other decision.
func TestQRDBIdentityGatedMethods_DenyInvalidToken(t *testing.T) {
	require.NotEmpty(t, qrdbIdentityGatedMethods)
	for method := range qrdbIdentityGatedMethods {
		authorizer := &fakeAuthenticatingAuthorizer{
			fakeValidatorAuthorizer: fakeValidatorAuthorizer{token: "good"},
		}
		interceptor := UnaryAuthorizationInterceptor(authorizer)

		called := false
		_, err := interceptor(ctxWithBearer("forged"), nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
		require.Error(t, err, method)
		assert.Equal(t, codes.Unauthenticated, status.Code(err), method)
		assert.False(t, called, "handler must not run for invalid token on %s", method)
		assert.Equal(t, 1, authorizer.validateCalls, method)
	}
}

// TestQRDBIdentityGatedMethods_FailClosedWithoutAuthenticator asserts an
// authorizer that lacks the scope-free Validate seam denies the
// identity-gated verbs outright (PermissionDenied): the gate never falls
// back to the scoped Authorize path or admits without validation.
func TestQRDBIdentityGatedMethods_FailClosedWithoutAuthenticator(t *testing.T) {
	require.NotEmpty(t, qrdbIdentityGatedMethods)
	for method := range qrdbIdentityGatedMethods {
		authorizer := &fakeValidatorAuthorizer{
			token:    "good",
			authzCtx: &platformtoken.AuthorizedContext{Subject: "u1"},
		}
		interceptor := UnaryAuthorizationInterceptor(authorizer)

		called := false
		_, err := interceptor(ctxWithBearer("good"), nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
		require.Error(t, err, method)
		assert.Equal(t, codes.PermissionDenied, status.Code(err), method)
		assert.False(t, called, "handler must not run without the authenticated seam on %s", method)
		assert.Equal(t, "", authorizer.lastPerm, "scoped Authorize must not be consulted for identity-gated method %s", method)
	}
}

// TestQRDBUnwiredSurface_DefaultDenied asserts representative qrdb.v1 methods
// from UNWIRED families (KV, lease, lock, vector) stay default-denied even for
// a caller with a valid token: the broker's canonical surface for those
// capabilities is the quicbaas.* service namespace.
func TestQRDBUnwiredSurface_DefaultDenied(t *testing.T) {
	for _, method := range []string{
		"/qrdb.v1.QRDBService/Put",
		"/qrdb.v1.QRDBService/Get",
		"/qrdb.v1.QRDBService/LeaseGrant",
		"/qrdb.v1.QRDBService/LockAcquire",
		"/qrdb.v1.QRDBService/VectorUpsert",
		"/qrdb.v1.QRDBService/Watch",
	} {
		_, _, mapped := grpcResourceAndAction(method)
		assert.False(t, mapped, method)
		assert.False(t, isPublicGRPCMethod(method), method)

		authorizer := &fakeValidatorAuthorizer{denyErr: &platformtoken.InvalidTokenError{Detail: "should never be called"}}
		interceptor := UnaryAuthorizationInterceptor(authorizer)
		_, err := interceptor(ctxWithBearer("good"), nil,
			&grpc.UnaryServerInfo{FullMethod: method},
			func(context.Context, interface{}) (interface{}, error) { return nil, nil })
		require.Error(t, err, method)
		assert.Equal(t, codes.PermissionDenied, status.Code(err), method)
	}
}
