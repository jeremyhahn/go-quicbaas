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

// This file lives in the external test package so it can use the shared
// testutil doubles (pkg/testutil imports pkg/iam, so an in-package test cannot).
package iam_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const testAdminQRDBUserID uint64 = 4242

// newTestCert issues a self-signed certificate. Passing a shared key models a
// certificate RENEWAL: a new DER (new cert credential) over the same public key
// (same SPKI credential).
func newTestCert(t *testing.T, commonName string, key *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	if key == nil {
		var err error
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func newTestEnroller(t *testing.T) (*iam.CertLoginFactorEnroller, *testutil.MemoryUserAuthFactors) {
	t.Helper()
	factors := testutil.NewMemoryUserAuthFactors()
	enroller, err := iam.NewCertLoginFactorEnroller(factors)
	if err != nil {
		t.Fatalf("NewCertLoginFactorEnroller: %v", err)
	}
	return enroller, factors
}

// ---------------------------------------------------------------------------
// Credential key derivation
// ---------------------------------------------------------------------------

// TestMTLSCredentialKeys_MatchTheLoginResolverContract pins the exact blobs the
// login ceremony resolves with. pkg/service.LoginService.FinishLogin sends
// SHA-256(SPKI) as LookupKey and SHA-256(cert.Raw) as the alternate key; the
// enrolled factor must carry the identical bytes or the lookup misses.
func TestMTLSCredentialKeys_MatchTheLoginResolverContract(t *testing.T) {
	cert := newTestCert(t, "admin", nil)

	wantCert := sha256.Sum256(cert.Raw)
	if got := iam.MTLSCredentialKey(cert); !bytes.Equal(got, wantCert[:]) {
		t.Fatalf("MTLSCredentialKey = %x, want SHA-256(cert.Raw) %x", got, wantCert)
	}

	wantSPKI := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if got := iam.MTLSSPKICredentialKey(cert); !bytes.Equal(got, wantSPKI[:]) {
		t.Fatalf("MTLSSPKICredentialKey = %x, want SHA-256(SPKI) %x", got, wantSPKI)
	}

	if bytes.Equal(wantCert[:], wantSPKI[:]) {
		t.Fatal("the cert-DER and SPKI digests must differ — they are different algorithms over different inputs")
	}
}

// TestMTLSCredentialKeys_RejectUnusableCertificates ensures a nil or
// content-free certificate yields nil rather than a digest over nothing, so the
// caller fails on the typed validation error instead of enrolling a garbage key.
func TestMTLSCredentialKeys_RejectUnusableCertificates(t *testing.T) {
	tests := []struct {
		name string
		cert *x509.Certificate
	}{
		{name: "nil certificate", cert: nil},
		{name: "empty certificate", cert: &x509.Certificate{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := iam.MTLSCredentialKey(tt.cert); got != nil {
				t.Fatalf("MTLSCredentialKey = %x, want nil", got)
			}
			if got := iam.MTLSSPKICredentialKey(tt.cert); got != nil {
				t.Fatalf("MTLSSPKICredentialKey = %x, want nil", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Enrollment
// ---------------------------------------------------------------------------

// TestCertLoginFactorEnroller_EnrollMakesTheCertificateLoginResolvable is the
// regression guard for the bug this type closes: before it, binding a
// certificate wrote only the user-side thumbprint and `auth login --method mtls`
// answered "factor not found". The factor must resolve under BOTH keys the
// login path sends.
func TestCertLoginFactorEnroller_EnrollMakesTheCertificateLoginResolvable(t *testing.T) {
	enroller, factors := newTestEnroller(t)
	cert := newTestCert(t, "admin", nil)
	ctx := context.Background()

	err := enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{
		UserID:      testAdminQRDBUserID,
		Certificate: cert,
		Label:       "admin certificate (admin)",
		Metadata:    map[string]string{"binding_source": "bootstrap_admin_cert"},
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	// The renewal-stable SPKI key is the one FinishLogin sends first.
	bySPKI, err := factors.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS, iam.MTLSSPKICredentialKey(cert))
	if err != nil {
		t.Fatalf("lookup by SPKI key: %v (a failure here means mtls login would answer 'factor not found')", err)
	}
	if bySPKI.UserID != testAdminQRDBUserID {
		t.Fatalf("factor bound to user %d, want %d", bySPKI.UserID, testAdminQRDBUserID)
	}
	if bySPKI.Type != qrdbsdk.AuthFactorMTLS {
		t.Fatalf("factor type = %q, want %q", bySPKI.Type, qrdbsdk.AuthFactorMTLS)
	}
	if bySPKI.CreatedBy != qrdbsdk.FactorCreatedByAdmin {
		t.Fatalf("factor provenance = %q, want %q", bySPKI.CreatedBy, qrdbsdk.FactorCreatedByAdmin)
	}
	if bySPKI.Label != "admin certificate (admin)" {
		t.Fatalf("factor label = %q, want the operator-visible label", bySPKI.Label)
	}
	if bySPKI.Metadata["binding_source"] != "bootstrap_admin_cert" {
		t.Fatalf("factor metadata lost the binding provenance: %v", bySPKI.Metadata)
	}

	// The legacy cert-DER key is the alternate key FinishLogin sends.
	byCert, err := factors.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS, iam.MTLSCredentialKey(cert))
	if err != nil {
		t.Fatalf("lookup by cert-DER key: %v", err)
	}
	if byCert.ID != bySPKI.ID {
		t.Fatalf("the two lookup keys resolved different factors (%q vs %q)", byCert.ID, bySPKI.ID)
	}
}

// TestCertLoginFactorEnroller_EnrollIsIdempotent ensures re-binding the SAME
// certificate to the SAME user succeeds without a duplicate. The bootstrap path
// re-runs across restarts and ceremonies, so a second bind must not fail.
func TestCertLoginFactorEnroller_EnrollIsIdempotent(t *testing.T) {
	enroller, factors := newTestEnroller(t)
	cert := newTestCert(t, "admin", nil)
	ctx := context.Background()

	enrollment := iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID, Certificate: cert}
	if err := enroller.Enroll(ctx, enrollment); err != nil {
		t.Fatalf("first Enroll: %v", err)
	}
	if err := enroller.Enroll(ctx, enrollment); err != nil {
		t.Fatalf("second Enroll must be idempotent, got: %v", err)
	}

	list, err := factors.List(ctx, testAdminQRDBUserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d factors after re-enrolling the same certificate, want exactly 1", len(list))
	}
}

// TestCertLoginFactorEnroller_EnrollRejectsCertificateOwnedByAnotherUser is the
// security guard on the idempotency path: accepting a duplicate blindly would
// report a successful binding for a credential that authenticates a DIFFERENT
// principal.
func TestCertLoginFactorEnroller_EnrollRejectsCertificateOwnedByAnotherUser(t *testing.T) {
	enroller, _ := newTestEnroller(t)
	cert := newTestCert(t, "admin", nil)
	ctx := context.Background()

	if err := enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{UserID: 1001, Certificate: cert}); err != nil {
		t.Fatalf("first Enroll: %v", err)
	}

	err := enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{UserID: 2002, Certificate: cert})
	var mismatch *iam.CertLoginFactorOwnerMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Enroll error = %v (%T), want CertLoginFactorOwnerMismatchError", err, err)
	}
	if mismatch.RequestedUserID != 2002 || mismatch.EnrolledUserID != 1001 {
		t.Fatalf("mismatch = %+v, want requested 2002 / enrolled 1001", mismatch)
	}
}

// TestCertLoginFactorEnroller_EnrollAcceptsRenewedCertificateForSameUser covers
// the renewal case: a new certificate over the same keypair collides on the
// SPKI index, which must be recognised as the same user's credential rather
// than reported as a foreign one.
func TestCertLoginFactorEnroller_EnrollAcceptsRenewedCertificateForSameUser(t *testing.T) {
	enroller, _ := newTestEnroller(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	original := newTestCert(t, "admin", key)
	renewed := newTestCert(t, "admin", key)
	if bytes.Equal(original.Raw, renewed.Raw) {
		t.Fatal("the renewal fixture must produce a different DER")
	}
	ctx := context.Background()

	if err := enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID, Certificate: original}); err != nil {
		t.Fatalf("Enroll original: %v", err)
	}
	if err := enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID, Certificate: renewed}); err != nil {
		t.Fatalf("Enroll renewed certificate for the same user must succeed, got: %v", err)
	}
}

// TestCertLoginFactorEnroller_EnrollFailsClosedOnStoreError ensures a store
// failure surfaces as a typed error and never reports a successful enrollment —
// the caller relies on that to abort BEFORE persisting the binding.
func TestCertLoginFactorEnroller_EnrollFailsClosedOnStoreError(t *testing.T) {
	enroller, factors := newTestEnroller(t)
	storeErr := errors.New("kv unavailable")
	factors.AddErr = storeErr

	err := enroller.Enroll(context.Background(), iam.CertLoginFactorEnrollment{
		UserID:      testAdminQRDBUserID,
		Certificate: newTestCert(t, "admin", nil),
	})
	var enrollErr *iam.CertLoginFactorEnrollError
	if !errors.As(err, &enrollErr) {
		t.Fatalf("Enroll error = %v (%T), want CertLoginFactorEnrollError", err, err)
	}
	if !errors.Is(err, storeErr) {
		t.Fatal("CertLoginFactorEnrollError must unwrap to the underlying store failure")
	}
	if enrollErr.UserID != testAdminQRDBUserID {
		t.Fatalf("error user id = %d, want %d", enrollErr.UserID, testAdminQRDBUserID)
	}
}

// TestCertLoginFactorEnroller_EnrollFailsClosedOnUnreadableDuplicate covers the
// duplicate-but-unresolvable case (qrdb keeps the index entry of a REVOKED
// factor). Reporting success there would leave a binding whose factor login
// cannot use — precisely the defect this code prevents.
func TestCertLoginFactorEnroller_EnrollFailsClosedOnUnreadableDuplicate(t *testing.T) {
	enroller, factors := newTestEnroller(t)
	cert := newTestCert(t, "admin", nil)
	ctx := context.Background()

	if err := enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID, Certificate: cert}); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	list, err := factors.List(ctx, testAdminQRDBUserID)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v; want exactly one factor", list, err)
	}
	if err := factors.Remove(ctx, list[0].ID, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	err = enroller.Enroll(ctx, iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID, Certificate: cert})
	var enrollErr *iam.CertLoginFactorEnrollError
	if !errors.As(err, &enrollErr) {
		t.Fatalf("Enroll error = %v (%T), want CertLoginFactorEnrollError for a revoked duplicate", err, err)
	}
	if !errors.Is(err, qrdbsdk.ErrFactorRevoked) {
		t.Fatalf("error must unwrap to the revoked-factor cause, got %v", err)
	}
}

// TestCertLoginFactorEnroller_EnrollRejectsInvalidInput covers the validation
// guards: an unresolved user or an unusable certificate must fail before any
// store call.
func TestCertLoginFactorEnroller_EnrollRejectsInvalidInput(t *testing.T) {
	cert := newTestCert(t, "admin", nil)
	tests := []struct {
		name       string
		enrollment iam.CertLoginFactorEnrollment
		wantField  string
	}{
		{
			name:       "zero user id",
			enrollment: iam.CertLoginFactorEnrollment{Certificate: cert},
			wantField:  "UserID",
		},
		{
			name:       "nil certificate",
			enrollment: iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID},
			wantField:  "Certificate",
		},
		{
			name:       "certificate with no DER",
			enrollment: iam.CertLoginFactorEnrollment{UserID: testAdminQRDBUserID, Certificate: &x509.Certificate{}},
			wantField:  "Certificate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enroller, factors := newTestEnroller(t)
			err := enroller.Enroll(context.Background(), tt.enrollment)
			var cfgErr *iam.CertLoginFactorConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("Enroll error = %v (%T), want CertLoginFactorConfigError", err, err)
			}
			if cfgErr.Field != tt.wantField {
				t.Fatalf("error field = %q, want %q", cfgErr.Field, tt.wantField)
			}
			if list, _ := factors.List(context.Background(), testAdminQRDBUserID); len(list) != 0 {
				t.Fatal("invalid input must not reach the factor store")
			}
		})
	}
}

// TestNewCertLoginFactorEnroller_RejectsNilServicer ensures the split-store bug
// cannot be reintroduced by wiring a nil factor service: the failure surfaces at
// construction time, not silently at bind time.
func TestNewCertLoginFactorEnroller_RejectsNilServicer(t *testing.T) {
	enroller, err := iam.NewCertLoginFactorEnroller(nil)
	var cfgErr *iam.CertLoginFactorConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v (%T), want CertLoginFactorConfigError", err, err)
	}
	if enroller != nil {
		t.Fatal("a rejected construction must not return an enroller")
	}
}

// TestCertLoginFactorEnroller_NilReceiverFailsClosed guards the zero-value
// enroller: it must report a configuration error rather than panic.
func TestCertLoginFactorEnroller_NilReceiverFailsClosed(t *testing.T) {
	var enroller *iam.CertLoginFactorEnroller
	err := enroller.Enroll(context.Background(), iam.CertLoginFactorEnrollment{
		UserID:      testAdminQRDBUserID,
		Certificate: newTestCert(t, "admin", nil),
	})
	var cfgErr *iam.CertLoginFactorConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v (%T), want CertLoginFactorConfigError", err, err)
	}
}

// TestCertLoginFactorErrors_Format covers the operator-facing messages and the
// errors.Is/As plumbing of the typed errors.
func TestCertLoginFactorErrors_Format(t *testing.T) {
	t.Run("enroll error carries the cause", func(t *testing.T) {
		cause := errors.New("boom")
		err := &iam.CertLoginFactorEnrollError{UserID: 7, Cause: cause}
		if got := err.Error(); got == "" {
			t.Fatal("Error() must not be empty")
		}
		if !errors.Is(err, cause) {
			t.Fatal("Unwrap must expose the cause")
		}
	})

	t.Run("nil errors degrade gracefully", func(t *testing.T) {
		var enrollErr *iam.CertLoginFactorEnrollError
		if enrollErr.Error() == "" {
			t.Fatal("nil CertLoginFactorEnrollError must still render a message")
		}
		if enrollErr.Unwrap() != nil {
			t.Fatal("nil CertLoginFactorEnrollError must unwrap to nil")
		}
		var cfgErr *iam.CertLoginFactorConfigError
		if cfgErr.Error() == "" {
			t.Fatal("nil CertLoginFactorConfigError must still render a message")
		}
		var mismatch *iam.CertLoginFactorOwnerMismatchError
		if mismatch.Error() == "" {
			t.Fatal("nil CertLoginFactorOwnerMismatchError must still render a message")
		}
	})

	t.Run("owner mismatch names both users", func(t *testing.T) {
		err := &iam.CertLoginFactorOwnerMismatchError{RequestedUserID: 2, EnrolledUserID: 1, FactorID: "f-1"}
		msg := err.Error()
		for _, want := range []string{"f-1", "1", "2"} {
			if !bytes.Contains([]byte(msg), []byte(want)) {
				t.Fatalf("message %q must mention %q", msg, want)
			}
		}
	})
}
