// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

// Package main: success-path coverage for the runCanonicalAuthFactor*
// handlers. The existing tests only reach the factory-error branch; these
// drive the full List/Remove/Relabel handlers through a fake
// AuthFactorsClient so the output-formatting and SDK-error branches that
// operators see are exercised.
package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// fakeAuthFactorsClient is a configurable AuthFactorsClient. Unset funcs
// return zero-value successes.
type fakeAuthFactorsClient struct {
	addFunc     func(ctx context.Context, req *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error)
	listFunc    func(ctx context.Context, userID string) (*quicbaas.AuthFactorListResponse, error)
	removeFunc  func(ctx context.Context, factorID string, force bool) error
	rotateFunc  func(ctx context.Context, oldID string, newFactor *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error)
	relabelFunc func(ctx context.Context, factorID, label string) error
	loginFunc   func(ctx context.Context, req *quicbaas.AuthFactorLoginRequest) (*quicbaas.AuthFactorLoginResponse, error)
}

func (f *fakeAuthFactorsClient) Add(ctx context.Context, req *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
	if f.addFunc != nil {
		return f.addFunc(ctx, req)
	}
	return &quicbaas.AuthFactorEnrollResponse{EnrollmentID: "enr-1", FactorName: req.FactorName}, nil
}

func (f *fakeAuthFactorsClient) List(ctx context.Context, userID string) (*quicbaas.AuthFactorListResponse, error) {
	if f.listFunc != nil {
		return f.listFunc(ctx, userID)
	}
	return &quicbaas.AuthFactorListResponse{}, nil
}

func (f *fakeAuthFactorsClient) Remove(ctx context.Context, factorID string, force bool) error {
	if f.removeFunc != nil {
		return f.removeFunc(ctx, factorID, force)
	}
	return nil
}

func (f *fakeAuthFactorsClient) Rotate(ctx context.Context, oldID string, newFactor *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
	if f.rotateFunc != nil {
		return f.rotateFunc(ctx, oldID, newFactor)
	}
	return &quicbaas.AuthFactorEnrollResponse{EnrollmentID: "enr-new"}, nil
}

func (f *fakeAuthFactorsClient) Relabel(ctx context.Context, factorID, label string) error {
	if f.relabelFunc != nil {
		return f.relabelFunc(ctx, factorID, label)
	}
	return nil
}

func (f *fakeAuthFactorsClient) Login(ctx context.Context, req *quicbaas.AuthFactorLoginRequest) (*quicbaas.AuthFactorLoginResponse, error) {
	if f.loginFunc != nil {
		return f.loginFunc(ctx, req)
	}
	return &quicbaas.AuthFactorLoginResponse{}, nil
}

// fakeAuthService embeds quicbaas.AuthService (nil) and overrides only the
// Factors() accessor the canonical-auth handlers use.
type fakeAuthService struct {
	quicbaas.AuthService
	factors quicbaas.AuthFactorsClient
}

func (f *fakeAuthService) Factors() quicbaas.AuthFactorsClient { return f.factors }

// fakeSecurityService embeds quicbaas.SecurityService (nil) and overrides
// only Auth().
type fakeSecurityService struct {
	quicbaas.SecurityService
	auth quicbaas.AuthService
}

func (f *fakeSecurityService) Auth() quicbaas.AuthService { return f.auth }

// fakeSecurityClient embeds quicbaas.Client (nil) and overrides Security()
// and Close().
type fakeSecurityClient struct {
	quicbaas.Client
	security quicbaas.SecurityService
}

func (f *fakeSecurityClient) Security() quicbaas.SecurityService { return f.security }
func (f *fakeSecurityClient) Close() error                       { return nil }

// newFakeFactorsClient wires a fakeAuthFactorsClient through the full SDK
// accessor chain and returns a quicbaas.Client usable by sdkClientFactory.
func newFakeFactorsClient(factors quicbaas.AuthFactorsClient) quicbaas.Client {
	return &fakeSecurityClient{
		security: &fakeSecurityService{
			auth: &fakeAuthService{factors: factors},
		},
	}
}

func TestRunCanonicalAuthFactorList_Empty(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(&fakeAuthFactorsClient{}), nil)
	require.NoError(t, runCanonicalAuthFactorList(context.Background(), "alice"))
}

func TestRunCanonicalAuthFactorList_WithEnrollments(t *testing.T) {
	revoked := time.Now()
	factors := &fakeAuthFactorsClient{
		listFunc: func(_ context.Context, _ string) (*quicbaas.AuthFactorListResponse, error) {
			return &quicbaas.AuthFactorListResponse{
				Enrollments: []quicbaas.AuthFactorEntry{
					{EnrollmentID: "e1", FactorName: "mtls", UserID: "alice", TenantID: "acme", EnrolledAt: time.Now()},
					{EnrollmentID: "e2", FactorName: "piv", UserID: "alice", TenantID: "acme", EnrolledAt: time.Now(), RevokedAt: &revoked},
				},
			}, nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	require.NoError(t, runCanonicalAuthFactorList(context.Background(), "alice"))
}

func TestRunCanonicalAuthFactorList_SDKError(t *testing.T) {
	factors := &fakeAuthFactorsClient{
		listFunc: func(_ context.Context, _ string) (*quicbaas.AuthFactorListResponse, error) {
			return nil, errors.New(errors.ErrInternal, "auth", "list failed")
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	require.Error(t, runCanonicalAuthFactorList(context.Background(), "alice"))
}

func TestRunCanonicalAuthFactorRemove_Success(t *testing.T) {
	var gotID string
	var gotForce bool
	factors := &fakeAuthFactorsClient{
		removeFunc: func(_ context.Context, factorID string, force bool) error {
			gotID, gotForce = factorID, force
			return nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	require.NoError(t, runCanonicalAuthFactorRemove(context.Background(), "factor-1", true))
	assert.Equal(t, "factor-1", gotID)
	assert.True(t, gotForce)
}

func TestRunCanonicalAuthFactorRemove_SDKError(t *testing.T) {
	factors := &fakeAuthFactorsClient{
		removeFunc: func(_ context.Context, _ string, _ bool) error {
			return errors.New(errors.ErrInternal, "auth", "remove failed")
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	require.Error(t, runCanonicalAuthFactorRemove(context.Background(), "factor-1", false))
}

func TestRunCanonicalAuthFactorRelabel_Success(t *testing.T) {
	var gotID, gotLabel string
	factors := &fakeAuthFactorsClient{
		relabelFunc: func(_ context.Context, factorID, label string) error {
			gotID, gotLabel = factorID, label
			return nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	require.NoError(t, runCanonicalAuthFactorRelabel(context.Background(), "factor-1", "new label"))
	assert.Equal(t, "factor-1", gotID)
	assert.Equal(t, "new label", gotLabel)
}

func TestRunCanonicalAuthFactorRelabel_SDKError(t *testing.T) {
	factors := &fakeAuthFactorsClient{
		relabelFunc: func(_ context.Context, _, _ string) error {
			return errors.New(errors.ErrInternal, "auth", "relabel failed")
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	require.Error(t, runCanonicalAuthFactorRelabel(context.Background(), "factor-1", "x"))
}

func TestRunCanonicalAuthFactorAdd_MtlsSuccess(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "user.pem")
	writePEMCertToPath(t, certPath)

	var gotReq *quicbaas.AuthFactorEnrollRequest
	factors := &fakeAuthFactorsClient{
		addFunc: func(_ context.Context, req *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			gotReq = req
			return &quicbaas.AuthFactorEnrollResponse{EnrollmentID: "enr-1", FactorName: req.FactorName}, nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	require.NoError(t, runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls",
		label:      "yubi",
		user:       "alice",
		certPath:   certPath,
	}))
	require.NotNil(t, gotReq)
	assert.Equal(t, "mtls", gotReq.FactorName)
}

func TestRunCanonicalAuthFactorAdd_SDKError(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "user.pem")
	writePEMCertToPath(t, certPath)

	factors := &fakeAuthFactorsClient{
		addFunc: func(_ context.Context, _ *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			return nil, errors.New(errors.ErrInternal, "auth", "add failed")
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	require.Error(t, runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls", label: "yubi", user: "alice", certPath: certPath,
	}))
}

func TestRunCanonicalAuthFactorAdd_InvalidFlags(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(&fakeAuthFactorsClient{}), nil)
	// Missing label -> buildCanonicalEnrollRequest fails after the factors
	// resolver succeeds.
	err := runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "mtls",
		certPath:   "/dev/null",
	})
	require.Error(t, err)
}

func TestRunCanonicalAuthFactorRotate_Success(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "user.pem")
	writePEMCertToPath(t, certPath)

	var gotOldID string
	factors := &fakeAuthFactorsClient{
		rotateFunc: func(_ context.Context, oldID string, _ *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			gotOldID = oldID
			return &quicbaas.AuthFactorEnrollResponse{EnrollmentID: "enr-new", FactorName: "mtls"}, nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	require.NoError(t, runCanonicalAuthFactorRotate(context.Background(), "old-id", &canonicalAuthFactorAddFlags{
		factorType: "mtls", label: "yubi", user: "alice", certPath: certPath,
	}))
	assert.Equal(t, "old-id", gotOldID)
}

func TestRunCanonicalAuthFactorRotate_SDKError(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "user.pem")
	writePEMCertToPath(t, certPath)

	factors := &fakeAuthFactorsClient{
		rotateFunc: func(_ context.Context, _ string, _ *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			return nil, errors.New(errors.ErrInternal, "auth", "rotate failed")
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	require.Error(t, runCanonicalAuthFactorRotate(context.Background(), "old-id", &canonicalAuthFactorAddFlags{
		factorType: "mtls", label: "yubi", user: "alice", certPath: certPath,
	}))
}

// fakeAuthServiceFull lets tests configure WebAuthn/PIV/Factors accessors.
type fakeAuthServiceFull struct {
	quicbaas.AuthService
	factors  quicbaas.AuthFactorsClient
	webauthn quicbaas.WebAuthnClient
	piv      quicbaas.PIVClient
}

func (f *fakeAuthServiceFull) Factors() quicbaas.AuthFactorsClient { return f.factors }
func (f *fakeAuthServiceFull) WebAuthn() quicbaas.WebAuthnClient   { return f.webauthn }
func (f *fakeAuthServiceFull) PIV() quicbaas.PIVClient             { return f.piv }

func newFakeAuthClient(auth quicbaas.AuthService) quicbaas.Client {
	return &fakeSecurityClient{security: &fakeSecurityService{auth: auth}}
}

func TestCanonicalAuthService_Success(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, newFakeAuthClient(&fakeAuthServiceFull{}), nil)
	svc, err := canonicalAuthService("test")
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestCanonicalAuthService_NilAuthSurface(t *testing.T) {
	// Security() returns a service whose Auth() is nil.
	c := &fakeSecurityClient{security: &fakeSecurityService{auth: nil}}
	withFakeSDKClientForCanonicalAuth(t, c, nil)
	_, err := canonicalAuthService("test")
	require.Error(t, err)
	var typed *SecuritySurfaceUnavailableError
	require.ErrorAs(t, err, &typed)
}

func TestCanonicalAuthWebAuthn_NilSubClient(t *testing.T) {
	// Auth() is present but WebAuthn() returns nil.
	withFakeSDKClientForCanonicalAuth(t, newFakeAuthClient(&fakeAuthServiceFull{webauthn: nil}), nil)
	_, err := canonicalAuthWebAuthn("test")
	require.Error(t, err)
	var typed *SecuritySurfaceUnavailableError
	require.ErrorAs(t, err, &typed)
}

func TestCanonicalAuthPIV_NilSubClient(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, newFakeAuthClient(&fakeAuthServiceFull{piv: nil}), nil)
	_, err := canonicalAuthPIV("test")
	require.Error(t, err)
	var typed *SecuritySurfaceUnavailableError
	require.ErrorAs(t, err, &typed)
}

func TestCanonicalAuthFactors_NilSubClient(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, newFakeAuthClient(&fakeAuthServiceFull{factors: nil}), nil)
	_, err := canonicalAuthFactors("test")
	require.Error(t, err)
	var typed *SecuritySurfaceUnavailableError
	require.ErrorAs(t, err, &typed)
}
