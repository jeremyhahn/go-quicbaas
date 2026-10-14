//go:build !integration

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

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const provisionTenantID = "acme"

// provisionFixture is a server with a CA and trust domain, IAM components over
// one in-memory storage, and a provisioner wired the way iam_init wires it,
// except that the auth-factor servicer is the in-memory one (no qrdb node).
type provisionFixture struct {
	srv     *Server
	c       *iamComponents
	storage *iam.MemoryStorage
	factors qrdbsdk.UserAuthFactorServicer
	p       *tenantProvisioner
}

func newProvisionFixture(t *testing.T) *provisionFixture {
	t.Helper()
	storage := iam.NewMemoryStorage()
	return newProvisionFixtureOver(t, storage, storage)
}

// newProvisionFixtureOver builds the fixture with userStorage holding the
// users, so a test can fail a user-store step.
func newProvisionFixtureOver(t *testing.T, storage *iam.MemoryStorage, userStorage iam.UserStorage) *provisionFixture {
	t.Helper()
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.IAM.SPIFFETrustDomain = testTrustDomain

	c, factors := buildTestIAMComponentsOver(t, storage, userStorage, storage)
	p := srv.newTenantProvisioner(c)
	p.factors = func() qrdbsdk.UserAuthFactorServicer { return factors }
	return &provisionFixture{srv: srv, c: c, storage: storage, factors: factors, p: p}
}

func (f *provisionFixture) requireNoTenant(t *testing.T) {
	t.Helper()
	_, err := f.c.tenantManager.GetTenant(context.Background(), provisionTenantID)
	require.Error(t, err, "a refused provision must not have written the tenant")
}

func inviteRequest() *service.IAMProvisionTenantRequest {
	return &service.IAMProvisionTenantRequest{
		IsolationType:     qrdbsdk.TenantIsolationPrefix,
		TenantID:          provisionTenantID,
		Name:              "Acme",
		AdminEnrollment:   service.IAMTenantAdminEnrollInvite,
		InviteExpiryHours: 24,
	}
}

// failingInviteStore refuses to mint while fail is set.
type failingInviteStore struct {
	iam.InviteStore
	fail bool
}

var errInviteMint = errors.New("invite store unavailable")

func (s *failingInviteStore) Create(ctx context.Context, tenantID string, userID uint64, ttl time.Duration) (string, *iam.InviteRecord, error) {
	if s.fail {
		return "", nil, errInviteMint
	}
	return s.InviteStore.Create(ctx, tenantID, userID, ttl)
}

func TestTenantProvisioner_CertModeSignsTheAdminCSRAndBindsIt(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	require.NoError(t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})

	resp, err := f.p.Provision(ctx, &service.IAMProvisionTenantRequest{
		IsolationType: qrdbsdk.TenantIsolationPrefix,
		TenantID:      provisionTenantID,
		AdminCSRPEM:   string(csrPEM),
	})
	require.NoError(t, err)

	assert.Equal(t, provisionTenantID, resp.TenantID)
	assert.False(t, resp.AdminPending)
	assert.Empty(t, resp.InviteToken, "cert mode mints no invite")
	assert.NotEmpty(t, resp.AdminRoleID)

	block, _ := pem.Decode([]byte(resp.AdminCertPEM))
	require.NotNil(t, block, "the signed admin certificate is returned")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	assert.True(t, key.PublicKey.Equal(cert.PublicKey),
		"the certificate must carry the admin's own key: nothing was generated on their behalf")
	assert.Equal(t, qrdbsdk.ComputeSPKIThumbprint(cert), resp.AdminCertThumbprint)

	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, resp.AdminUserID)
	require.NoError(t, err)
	assert.Equal(t, resp.AdminCertThumbprint, admin.SPKIThumbprint(), "the thumbprint is bound to the admin")
	assert.Contains(t, admin.Roles, resp.AdminRoleID)
}

func TestTenantProvisioner_CertModeWithoutCSRIsRefusedBeforeAnyWrite(t *testing.T) {
	f := newProvisionFixture(t)

	_, err := f.p.Provision(context.Background(), &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID})
	require.Error(t, err)
	assert.True(t, dberrors.IsInvalidInput(err), "%v", err)
	var csrErr *service.ProvisionAdminCSRRequiredError
	assert.ErrorAs(t, err, &csrErr)
	f.requireNoTenant(t)
}

func TestTenantProvisioner_CertModeNeedsASignerAndTrustDomain(t *testing.T) {
	csr := string(makeCSRPEM(t, "admin"))
	req := &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: csr}

	t.Run("no CA", func(t *testing.T) {
		f := newProvisionFixture(t)
		f.srv.xkmsCA = nil

		_, err := f.p.Provision(context.Background(), req)
		var unavailableErr *TenantProvisionUnavailableError
		require.ErrorAs(t, err, &unavailableErr)
		assert.Equal(t, "certificate signer", unavailableErr.Seam)
		assert.True(t, dberrors.Is(err, dberrors.ErrUnavailable))
		f.requireNoTenant(t)
	})

	t.Run("no trust domain", func(t *testing.T) {
		f := newProvisionFixture(t)
		f.p.trustDomain = ""

		_, err := f.p.Provision(context.Background(), req)
		assert.ErrorIs(t, err, ErrNoSPIFFETrustDomain)
		f.requireNoTenant(t)
	})

	t.Run("no login factor enroller", func(t *testing.T) {
		f := newProvisionFixture(t)
		f.p.certLoginFactors = nil

		_, err := f.p.Provision(context.Background(), req)
		var unavailableErr *TenantProvisionUnavailableError
		require.ErrorAs(t, err, &unavailableErr)
		assert.Equal(t, "certificate login factor enroller", unavailableErr.Seam)
		f.requireNoTenant(t)
	})
}

// provisionCertTenant provisions tenantID in cert mode and returns the
// response with the certificate it issued.
func provisionCertTenant(t *testing.T, f *provisionFixture, tenantID string) (*service.IAMProvisionTenantResponse, *x509.Certificate) {
	t.Helper()
	resp, err := f.p.Provision(context.Background(), &service.IAMProvisionTenantRequest{
		IsolationType: qrdbsdk.TenantIsolationPrefix,
		TenantID:      tenantID,
		AdminCSRPEM:   string(makeCSRPEM(t, "admin")),
	})
	require.NoError(t, err)
	block, _ := pem.Decode([]byte(resp.AdminCertPEM))
	require.NotNil(t, block, "the signed admin certificate is returned")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return resp, cert
}

// lookupMTLSFactor resolves cert the way the unified login resolver does.
func lookupMTLSFactor(t *testing.T, f *provisionFixture, key []byte) *qrdbsdk.AuthFactor {
	t.Helper()
	factor, err := f.factors.LookupByCredentialKey(context.Background(), qrdbsdk.AuthFactorMTLS, key)
	require.NoError(t, err, "the login resolver must find the certificate's mtls factor")
	require.NotNil(t, factor)
	return factor
}

func TestTenantProvisioner_CertModeEnrollsTheAdminCertificateAsALoginFactor(t *testing.T) {
	f := newProvisionFixture(t)
	resp, cert := provisionCertTenant(t, f, provisionTenantID)

	adminID := resp.AdminUserID
	for name, key := range map[string][]byte{
		"certificate digest": iam.MTLSCredentialKey(cert),
		"SPKI digest":        iam.MTLSSPKICredentialKey(cert),
	} {
		factor := lookupMTLSFactor(t, f, key)
		assert.Equal(t, adminID, factor.UserID, "%s resolves to the new tenant admin", name)
		assert.Equal(t, provisionTenantID, factor.Metadata["tenant_id"])
		assert.Equal(t, adminCertBindingSourceTenantProvision, factor.Metadata["binding_source"])
	}
}

func TestTenantProvisioner_CertModeLoginFactorIsTenantScoped(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	acme, acmeCert := provisionCertTenant(t, f, provisionTenantID)
	globex, _ := provisionCertTenant(t, f, "globex")

	factor := lookupMTLSFactor(t, f, iam.MTLSCredentialKey(acmeCert))
	owner := factor.UserID
	assert.Equal(t, acme.AdminUserID, owner)
	assert.NotEqual(t, globex.AdminUserID, owner, "one tenant's admin certificate never resolves to another tenant's admin")

	_, err := f.c.identityStore.GetUser(ctx, "globex", owner)
	assert.Error(t, err, "the certificate's owner is not a user of the other tenant")
	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, owner)
	require.NoError(t, err)
	assert.Equal(t, provisionTenantID, admin.TenantID)
}

var errFactorStore = errors.New("factor store unavailable")

// errUserDelete is undeletableUserStorage's deletion failure.
var errUserDelete = errors.New("user store refuses deletion")

func TestTenantProvisioner_CertModeLoginFactorFailureRollsBackAndARetryRecovers(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	enroller := f.p.certLoginFactors
	f.p.certLoginFactors = newFailingCertLoginFactorEnroller(t)
	req := &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))}

	resp, err := f.p.Provision(ctx, req)
	assert.Nil(t, resp, "a certificate that cannot log in is not handed out")
	var factorErr *TenantProvisionLoginFactorError
	require.ErrorAs(t, err, &factorErr)
	assert.ErrorIs(t, err, errFactorStore)
	var enrollErr *iam.CertLoginFactorEnrollError
	assert.ErrorAs(t, err, &enrollErr)

	// The admin the call created is removed, so no half-provisioned admin is
	// left for a retry to mistake for a finished one. The tenant stays: qrdb
	// retires a deleted tenant id, so deleting it would refuse the retry.
	_, err = f.c.identityStore.GetUserByUsername(ctx, provisionTenantID, "admin")
	assert.Error(t, err, "the admin and its certificate binding are rolled back")
	_, err = f.c.tenantManager.GetTenant(ctx, provisionTenantID)
	assert.NoError(t, err, "the tenant is kept for the retry")

	f.p.certLoginFactors = enroller
	retried, cert := provisionCertTenant(t, f, provisionTenantID)
	factor := lookupMTLSFactor(t, f, iam.MTLSCredentialKey(cert))
	assert.Equal(t, retried.AdminUserID, factor.UserID, "the retry issues a certificate that logs in")
}

func TestTenantProvisioner_CertModeRollbackKeepsAPreexistingTenant(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	_, err := f.c.tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{ID: provisionTenantID, Name: "Acme"})
	require.NoError(t, err)
	f.p.certLoginFactors = newFailingCertLoginFactorEnroller(t)

	_, err = f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
	var factorErr *TenantProvisionLoginFactorError
	require.ErrorAs(t, err, &factorErr)

	_, err = f.c.identityStore.GetUserByUsername(ctx, provisionTenantID, "admin")
	assert.Error(t, err, "the admin the call created is rolled back")
	_, err = f.c.tenantManager.GetTenant(ctx, provisionTenantID)
	assert.NoError(t, err, "a tenant the call did not create is not deleted")
}

func TestTenantProvisioner_CertModeFailedRollbackIsNeverReportedAsSuccess(t *testing.T) {
	storage := iam.NewMemoryStorage()
	f := newProvisionFixtureOver(t, storage, undeletableUserStorage{storage})
	ctx := context.Background()
	enroller := f.p.certLoginFactors
	f.p.certLoginFactors = newFailingCertLoginFactorEnroller(t)
	req := &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))}

	resp, err := f.p.Provision(ctx, req)
	assert.Nil(t, resp)
	var rollbackErr *TenantProvisionRollbackError
	require.ErrorAs(t, err, &rollbackErr)
	assert.ErrorIs(t, err, errFactorStore)
	assert.ErrorIs(t, err, errUserDelete)
	require.NotEmpty(t, rollbackErr.AdminUserID)

	// The admin survives half-deleted and without a login factor: a re-run
	// must say so rather than report the tenant as provisioned.
	f.p.certLoginFactors = enroller
	resp, err = f.p.Provision(ctx, req)
	assert.Nil(t, resp, "a half-deleted admin is never reported as provisioned")
	var mismatch *TenantAdminAccountMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, rollbackErr.AdminUserID, mismatch.AdminUserID)
	assert.Equal(t, provisionTenantID, mismatch.TenantID)
	assert.Equal(t, string(iam.LifecycleDeleting), mismatch.Lifecycle)
}

func TestTenantProvisioner_CertModeRerunWithoutALoginFactorIsRefused(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	first, cert := provisionCertTenant(t, f, provisionTenantID)
	factor := lookupMTLSFactor(t, f, iam.MTLSCredentialKey(cert))
	require.NoError(t, f.factors.Remove(qrdbsdk.ContextWithTenant(ctx, provisionTenantID), factor.ID, true))

	resp, err := f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
	assert.Nil(t, resp, "an admin without a login credential is never reported as provisioned")
	var missing *TenantAdminLoginCredentialMissingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, first.AdminUserID, missing.AdminUserID)
}

func TestTenantProvisioner_CertModeRerunRefusesAnAccountThatIsNotTheAdmin(t *testing.T) {
	tests := map[string]struct {
		alter        func(user *iam.User)
		lifecycle    iam.Lifecycle
		hasAdminRole bool
	}{
		"ordinary account": {alter: func(user *iam.User) { user.Roles = nil }, lifecycle: iam.LifecycleActive, hasAdminRole: false},
		"disabled admin":   {alter: func(user *iam.User) { user.Lifecycle = iam.LifecycleDisabled }, lifecycle: iam.LifecycleDisabled, hasAdminRole: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newProvisionFixture(t)
			ctx := context.Background()
			first, _ := provisionCertTenant(t, f, provisionTenantID)
			admin, err := f.storage.GetUser(ctx, provisionTenantID, first.AdminUserID)
			require.NoError(t, err)
			altered := *admin
			tt.alter(&altered)
			require.NoError(t, f.storage.UpdateUser(ctx, &altered))

			resp, err := f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
			assert.Nil(t, resp, "the username alone does not make an account the administrator")
			var mismatch *TenantAdminAccountMismatchError
			require.ErrorAs(t, err, &mismatch)
			assert.Equal(t, first.AdminUserID, mismatch.AdminUserID)
			assert.Equal(t, string(tt.lifecycle), mismatch.Lifecycle)
			assert.Equal(t, tt.hasAdminRole, mismatch.HasAdminRole)
			assert.Contains(t, err.Error(), "admin")
		})
	}
}

// errSign is the failure failingCertSigner reports.
var errSign = errors.New("CA refuses to sign")

// failingCertSigner is a CA that refuses every admin CSR, as a CA does when
// it is unavailable or its policy rejects the request.
type failingCertSigner struct{}

func (failingCertSigner) SignAdminCSR(context.Context, string, string, []byte) ([]byte, error) {
	return nil, errSign
}

func TestTenantProvisioner_CertModeSignerFailureRollsBackAndARetryRecovers(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	signer := f.p.signer
	f.p.signer = func() qrdbsdk.CertSigner { return failingCertSigner{} }

	resp, err := f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
	assert.Nil(t, resp)
	var provisionErr *TenantProvisionError
	require.ErrorAs(t, err, &provisionErr)
	assert.ErrorIs(t, err, errSign)

	// qrdb created the admin before the CA refused it; the compensation
	// removes that admin, so the retry provisions a fresh one into the
	// tenant the failed call left in place.
	_, err = f.c.identityStore.GetUserByUsername(ctx, provisionTenantID, "admin")
	assert.Error(t, err, "the admin qrdb created is rolled back")
	_, err = f.c.tenantManager.GetTenant(ctx, provisionTenantID)
	assert.NoError(t, err, "the tenant is kept for the retry")

	f.p.signer = signer
	retried, cert := provisionCertTenant(t, f, provisionTenantID)
	assert.NotEmpty(t, retried.AdminCertPEM, "the retry issues the admin certificate")
	factor := lookupMTLSFactor(t, f, iam.MTLSCredentialKey(cert))
	assert.Equal(t, retried.AdminUserID, factor.UserID)
}

func TestTenantProvisioner_CertModeSignerFailureKeepsAPreexistingTenant(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	_, err := f.c.tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{ID: provisionTenantID, Name: "Acme"})
	require.NoError(t, err)
	f.p.signer = func() qrdbsdk.CertSigner { return failingCertSigner{} }

	_, err = f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
	assert.ErrorIs(t, err, errSign)

	_, err = f.c.identityStore.GetUserByUsername(ctx, provisionTenantID, "admin")
	assert.Error(t, err, "the admin the call created is rolled back")
	_, err = f.c.tenantManager.GetTenant(ctx, provisionTenantID)
	assert.NoError(t, err, "a tenant the call did not create is not deleted")
}

func TestTenantProvisioner_CertModeSignerFailureWithAFailedRollbackSaysSo(t *testing.T) {
	storage := iam.NewMemoryStorage()
	f := newProvisionFixtureOver(t, storage, undeletableUserStorage{storage})
	f.p.signer = func() qrdbsdk.CertSigner { return failingCertSigner{} }

	_, err := f.p.Provision(context.Background(), &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
	var rollbackErr *TenantProvisionRollbackError
	require.ErrorAs(t, err, &rollbackErr)
	assert.ErrorIs(t, err, errSign)
	assert.ErrorIs(t, err, errUserDelete)
	assert.NotEmpty(t, rollbackErr.AdminUserID)
}

// provisionFailurePoints are the two places a cert-mode call fails after qrdb
// created its admin: the CA refusing the CSR and the login-factor write. Each
// runs during at that point, then fails the call.
var provisionFailurePoints = []struct {
	name       string
	failDuring func(t *testing.T, f *provisionFixture, during func())
}{
	{
		name: "signer",
		failDuring: func(_ *testing.T, f *provisionFixture, during func()) {
			f.p.signer = func() qrdbsdk.CertSigner { return interleavingCertSigner{during: during} }
		},
	},
	{
		name: "login factor",
		failDuring: func(t *testing.T, f *provisionFixture, during func()) {
			enroller, err := iam.NewCertLoginFactorEnroller(interleavingFactors{during: during})
			require.NoError(t, err)
			f.p.certLoginFactors = enroller
		},
	},
}

// A concurrent provision of the same absent tenant can land between a call's
// admin creation and its failure. The failed call's rollback removes only the
// admin it created, never the tenant or the admin the other call created.
func TestTenantProvisioner_CertModeRollbackKeepsAConcurrentProvision(t *testing.T) {
	for _, point := range provisionFailurePoints {
		t.Run(point.name, func(t *testing.T) {
			f := newProvisionFixture(t)
			ctx := context.Background()
			concurrent := *f.p
			var winner *service.IAMProvisionTenantResponse
			point.failDuring(t, f, func() {
				var err error
				winner, err = concurrent.Provision(ctx, &service.IAMProvisionTenantRequest{
					IsolationType: qrdbsdk.TenantIsolationPrefix,
					TenantID:      provisionTenantID,
					AdminUsername: "operator",
					AdminCSRPEM:   string(makeCSRPEM(t, "operator")),
				})
				require.NoError(t, err, "the concurrent provision succeeds")
			})

			_, err := f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
			require.Error(t, err)

			_, err = f.c.identityStore.GetUserByUsername(ctx, provisionTenantID, "admin")
			assert.Error(t, err, "the failed call's own admin is rolled back")
			_, err = f.c.tenantManager.GetTenant(ctx, provisionTenantID)
			require.NoError(t, err, "the tenant the concurrent call provisioned into is kept")
			operator, err := f.c.identityStore.GetUserByUsername(ctx, provisionTenantID, "operator")
			require.NoError(t, err, "the concurrent call's admin is kept")
			assert.Equal(t, winner.AdminUserID, operator.ID)
		})
	}
}

// A request canceled after qrdb created the admin is still cleaned up: the
// rollback runs detached from the request, so the retry finds no
// half-provisioned admin and succeeds.
func TestTenantProvisioner_CertModeRollbackOutlivesACanceledRequest(t *testing.T) {
	for _, point := range provisionFailurePoints {
		t.Run(point.name, func(t *testing.T) {
			storage := iam.NewMemoryStorage()
			users := &contextBoundUserStorage{MemoryStorage: storage}
			f := newProvisionFixtureOver(t, storage, users)
			enroller, signer := f.p.certLoginFactors, f.p.signer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			point.failDuring(t, f, cancel)

			_, err := f.p.Provision(ctx, &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
			require.Error(t, err)
			var rollbackErr *TenantProvisionRollbackError
			assert.False(t, errors.As(err, &rollbackErr), "the rollback completes although the request was canceled: %v", err)
			assert.True(t, users.deleteBounded, "the detached rollback is still bounded by a deadline")

			f.p.certLoginFactors, f.p.signer = enroller, signer
			provisionCertTenant(t, f, provisionTenantID)
		})
	}
}

// interleavingCertSigner runs during inside the CSR signing, then refuses it.
type interleavingCertSigner struct {
	during func()
}

func (s interleavingCertSigner) SignAdminCSR(context.Context, string, string, []byte) ([]byte, error) {
	s.during()
	return nil, errSign
}

// interleavingFactors runs during inside the admin's factor write, then fails
// the write.
type interleavingFactors struct {
	qrdbsdk.UserAuthFactorServicer
	during func()
}

func (f interleavingFactors) Add(context.Context, uint64, *qrdbsdk.AuthFactor) error {
	f.during()
	return errFactorStore
}

// contextBoundUserStorage refuses a delete under a done context, as a store
// round trip does, and records whether the delete ran under a deadline.
type contextBoundUserStorage struct {
	*iam.MemoryStorage
	deleteBounded bool
}

func (s *contextBoundUserStorage) DeleteUser(ctx context.Context, tenantID string, userID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, s.deleteBounded = ctx.Deadline()
	return s.MemoryStorage.DeleteUser(ctx, tenantID, userID)
}

func TestTenantProvisioner_InvalidCSRIsRefusedBeforeAnyWrite(t *testing.T) {
	weakKey, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	weakDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "admin"}}, weakKey)
	require.NoError(t, err)

	tests := map[string]struct {
		csr    string
		target any
	}{
		"malformed": {csr: "-----BEGIN CERTIFICATE REQUEST-----\nAA==\n-----END CERTIFICATE REQUEST-----\n", target: new(*service.ProvisionAdminCSRInvalidError)},
		"weak key":  {csr: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: weakDER})), target: new(*service.ProvisionAdminCSRKeyError)},
		"not a CSR": {csr: "admin", target: new(*service.ProvisionAdminCSRInvalidError)},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newProvisionFixture(t)
			_, err := f.p.Provision(context.Background(), &service.IAMProvisionTenantRequest{IsolationType: qrdbsdk.TenantIsolationPrefix, TenantID: provisionTenantID, AdminCSRPEM: tt.csr})
			require.Error(t, err)
			assert.True(t, dberrors.IsInvalidInput(err), "%v", err)
			assert.ErrorAs(t, err, tt.target)
			f.requireNoTenant(t)
		})
	}
}

func TestTenantProvisioner_BarrierRoutedIsolationIsRefusedBeforeAnyWrite(t *testing.T) {
	for _, isolation := range []qrdbsdk.TenantIsolationType{"", qrdbsdk.TenantIsolationBarrier, qrdbsdk.TenantIsolationVolume, qrdbsdk.TenantIsolationRaft} {
		t.Run("isolation "+string(isolation), func(t *testing.T) {
			f := newProvisionFixture(t)
			_, err := f.p.Provision(context.Background(), &service.IAMProvisionTenantRequest{IsolationType: isolation, TenantID: provisionTenantID, AdminCSRPEM: string(makeCSRPEM(t, "admin"))})
			var barrierErr *service.ProvisionBarrierIsolationError
			require.ErrorAs(t, err, &barrierErr)
			f.requireNoTenant(t)
		})
	}
}

func TestTenantProvisioner_CertModeRerunReportsTheExistingAdmin(t *testing.T) {
	f := newProvisionFixture(t)
	first, _ := provisionCertTenant(t, f, provisionTenantID)

	again, err := f.p.Provision(context.Background(), &service.IAMProvisionTenantRequest{
		IsolationType: qrdbsdk.TenantIsolationPrefix,
		TenantID:      provisionTenantID,
		AdminCSRPEM:   string(makeCSRPEM(t, "admin")),
	})
	require.NoError(t, err)
	assert.Equal(t, first.AdminUserID, again.AdminUserID, "the re-run identifies the real administrator")
	assert.Empty(t, again.AdminCertPEM, "a re-run does not rotate the admin's certificate")
}

func TestTenantProvisioner_CertModeEnrollsUnderTheProvisionedTenant(t *testing.T) {
	f := newProvisionFixture(t)
	recorder := &tenantRecordingFactors{MemoryUserAuthFactors: testutil.NewMemoryUserAuthFactors()}
	enroller, err := iam.NewCertLoginFactorEnroller(recorder)
	require.NoError(t, err)
	f.p.certLoginFactors = enroller

	// provisionCertTenant calls with a tenantless context, as the system
	// admin's gRPC or embedded request does.
	provisionCertTenant(t, f, provisionTenantID)
	assert.Equal(t, []string{provisionTenantID}, recorder.addTenants)
}

// newFailingCertLoginFactorEnroller returns an enroller whose factor store
// refuses every write with errFactorStore.
func newFailingCertLoginFactorEnroller(t *testing.T) *iam.CertLoginFactorEnroller {
	t.Helper()
	enroller, err := iam.NewCertLoginFactorEnroller(&recordingFactorServicer{addErr: errFactorStore})
	require.NoError(t, err)
	return enroller
}

// undeletableUserStorage refuses every user deletion, so a rollback cannot
// remove the admin it created.
type undeletableUserStorage struct {
	*iam.MemoryStorage
}

func (undeletableUserStorage) DeleteUser(context.Context, string, uint64) error {
	return errUserDelete
}

// tenantRecordingFactors records the context tenant each factor write is
// routed under, as qrdb's tenant-scoped factor store routes it.
type tenantRecordingFactors struct {
	*testutil.MemoryUserAuthFactors
	addTenants []string
}

func (f *tenantRecordingFactors) Add(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	f.addTenants = append(f.addTenants, tenantID)
	return f.MemoryUserAuthFactors.Add(ctx, userID, factor)
}

func TestTenantProvisioner_InviteModeCreatesAPendingAdminAndARedeemableInvite(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()

	resp, err := f.p.Provision(ctx, inviteRequest())
	require.NoError(t, err)

	assert.True(t, resp.AdminPending)
	assert.Empty(t, resp.AdminCertPEM, "invite mode issues no certificate")
	require.NotEmpty(t, resp.InviteToken)
	require.NotNil(t, resp.InviteExpiresAt)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), *resp.InviteExpiresAt, time.Minute)

	record, err := f.c.inviteStore.Lookup(ctx, provisionTenantID, resp.InviteToken)
	require.NoError(t, err)
	assert.Equal(t, resp.AdminUserID, record.UserID, "the invite targets the provisioned admin")
	assert.False(t, record.Used)

	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, resp.AdminUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecyclePending, admin.Lifecycle)
	assert.Contains(t, admin.Roles, resp.AdminRoleID)
}

// The invite lifetime is clamped as CreateInvitedUser clamps it: an omitted
// expiry must not reach the store as zero, and an excessive one is capped.
func TestTenantProvisioner_InviteModeClampsTheInviteExpiry(t *testing.T) {
	tests := []struct {
		name  string
		hours int
		want  time.Duration
	}{
		{"omitted expiry gets the default", 0, iam.DefaultInviteTTL},
		{"excessive expiry is capped", int(iam.MaxInviteTTL/time.Hour) + 24, iam.MaxInviteTTL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newProvisionFixture(t)
			ctx := context.Background()
			req := inviteRequest()
			req.InviteExpiryHours = tc.hours

			resp, err := f.p.Provision(ctx, req)
			require.NoError(t, err)
			require.NotEmpty(t, resp.InviteToken)
			require.NotNil(t, resp.InviteExpiresAt)
			assert.WithinDuration(t, time.Now().Add(tc.want), *resp.InviteExpiresAt, time.Minute)

			record, err := f.c.inviteStore.Lookup(ctx, provisionTenantID, resp.InviteToken)
			require.NoError(t, err)
			assert.WithinDuration(t, *resp.InviteExpiresAt, record.ExpiresAt, time.Second)
		})
	}
}

func TestTenantProvisioner_InviteModeForTheSystemTenantIsRefused(t *testing.T) {
	f := newProvisionFixture(t)
	req := inviteRequest()
	req.TenantID = qrdbsdk.SystemTenantID

	_, err := f.p.Provision(context.Background(), req)
	var systemErr *service.ProvisionSystemTenantInviteError
	require.ErrorAs(t, err, &systemErr)
	assert.True(t, dberrors.IsInvalidInput(err))
}

func TestTenantProvisioner_InviteModeNeedsAFailClosedFactorChecker(t *testing.T) {
	servicers := map[string]func(*testutil.MemoryUserAuthFactors) qrdbsdk.UserAuthFactorServicer{
		"no qrdb node": func(*testutil.MemoryUserAuthFactors) qrdbsdk.UserAuthFactorServicer { return nil },
		// Embedding the interface hides HasActiveFactor, modelling a servicer
		// that offers only the display-path List.
		"servicer without HasActiveFactor": func(m *testutil.MemoryUserAuthFactors) qrdbsdk.UserAuthFactorServicer {
			return struct{ qrdbsdk.UserAuthFactorServicer }{m}
		},
	}
	for name, servicer := range servicers {
		t.Run(name, func(t *testing.T) {
			f := newProvisionFixture(t)
			factors := testutil.NewMemoryUserAuthFactors()
			f.p.factors = func() qrdbsdk.UserAuthFactorServicer { return servicer(factors) }

			_, err := f.p.Provision(context.Background(), inviteRequest())
			var unavailableErr *TenantProvisionUnavailableError
			require.ErrorAs(t, err, &unavailableErr)
			assert.Equal(t, "fail-closed auth-factor checker", unavailableErr.Seam)
			f.requireNoTenant(t)
		})
	}
}

func TestTenantProvisioner_InviteFailureIsNotRolledBackAndARerunRecovers(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()
	invites := &failingInviteStore{InviteStore: f.c.inviteStore, fail: true}
	f.p.invites = invites

	_, err := f.p.Provision(ctx, inviteRequest())
	var inviteErr *TenantProvisionInviteError
	require.ErrorAs(t, err, &inviteErr)
	assert.ErrorIs(t, err, errInviteMint)
	require.NotEmpty(t, inviteErr.AdminUserID)

	// The tenant and its pending admin stay; nothing was rolled back.
	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, inviteErr.AdminUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecyclePending, admin.Lifecycle)

	invites.fail = false
	resp, err := f.p.Provision(ctx, inviteRequest())
	require.NoError(t, err, "a re-run resolves the same pending admin")
	assert.Equal(t, inviteErr.AdminUserID, resp.AdminUserID)
	assert.NotEmpty(t, resp.InviteToken)
}

func TestTenantProvisioner_MissingIdentityOrInviteStoreIsRefused(t *testing.T) {
	f := newProvisionFixture(t)
	f.p.invites = nil
	_, err := f.p.Provision(context.Background(), inviteRequest())
	var unavailableErr *TenantProvisionUnavailableError
	require.ErrorAs(t, err, &unavailableErr)
	assert.Equal(t, "invite store", unavailableErr.Seam)

	f.p.identity = nil
	_, err = f.p.Provision(context.Background(), inviteRequest())
	require.ErrorAs(t, err, &unavailableErr)
	assert.Equal(t, "identity store", unavailableErr.Seam)

	_, err = f.p.Provision(context.Background(), nil)
	assert.True(t, dberrors.IsInvalidInput(err))
}

// TestTenantProvisioner_InvitedAdminActivatesWithPIV is the end-to-end path a
// tenant takes when public registration is off: a system admin provisions the
// tenant in invite mode, and the invited admin redeems the token with a PIV
// certificate and becomes the tenant's active Admin.
func TestTenantProvisioner_InvitedAdminActivatesWithPIV(t *testing.T) {
	f := newProvisionFixture(t)
	ctx := context.Background()

	resp, err := f.p.Provision(ctx, inviteRequest())
	require.NoError(t, err)

	roots, leafPEM := newPIVChain(t)
	claims, err := iamtest.NewMemoryActivationClaims()
	if err != nil {
		t.Fatalf("NewMemoryActivationClaims: %v", err)
	}
	activator, err := iam.NewPIVActivator(iam.PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      f.storage,
		InviteStore:      f.c.inviteStore,
		LoginFactors:     f.c.certLoginFactors,
		ActivationClaims: claims,
	})
	require.NoError(t, err)
	require.NoError(t, activator.Activate(ctx, provisionTenantID, resp.InviteToken, leafPEM))

	leafBlock, _ := pem.Decode(leafPEM)
	require.NotNil(t, leafBlock)
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	require.NoError(t, err)
	assert.Equal(t, resp.AdminUserID, lookupMTLSFactor(t, f, iam.MTLSCredentialKey(leaf)).UserID,
		"the activated PIV certificate is login-ready")

	admin, err := f.c.identityStore.GetUser(ctx, provisionTenantID, resp.AdminUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, admin.Lifecycle)
	assert.Contains(t, admin.Roles, resp.AdminRoleID)
	assert.NotEmpty(t, admin.PIVThumbprints(), "the PIV certificate is bound to the admin")

	// The grant is tenant-scoped: the system tenant gained no user and the
	// Admin role is not a system role.
	systemUsers, err := f.c.identityStore.ListUsers(ctx, qrdbsdk.SystemTenantID, iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, systemUsers)
	_, err = f.c.identityStore.GetRole(ctx, qrdbsdk.SystemTenantID, resp.AdminRoleID)
	assert.Error(t, err)

	_, err = f.c.inviteStore.Lookup(ctx, provisionTenantID, resp.InviteToken)
	assert.ErrorIs(t, err, iam.ErrInviteUsed, "the invite is single-use")
}

// newPIVChain returns a root pool and a client-auth leaf it issued.
func TestIAMComponents_GetTenantProvisioner(t *testing.T) {
	c := &iamComponents{}
	assert.Nil(t, c.GetTenantProvisioner(), "an unset provisioner is a true nil interface")

	f := newProvisionFixture(t)
	f.c.provisioner = f.p
	assert.Same(t, f.p, f.c.GetTenantProvisioner())
}

func newPIVChain(t *testing.T) (*x509.CertPool, []byte) {
	t.Helper()
	now := time.Now()

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PIV Test Root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	require.NoError(t, err)
	root, err := x509.ParseCertificate(rootDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "tenant admin"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, root, &leafKey.PublicKey, rootKey)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(root)
	return pool, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
}
