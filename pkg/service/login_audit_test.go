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

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// recordingAuditor captures every rejection the facade reports.
type recordingAuditor struct {
	rejections []LoginRejection
}

func (r *recordingAuditor) RecordLoginRejection(_ context.Context, rejection LoginRejection) {
	r.rejections = append(r.rejections, rejection)
}

// TestLoginService_FinishLogin_ReportsRejectionWithVerifierReason is the
// regression guard for go-quicbaas#484: a refused FIDO2 login must leave a
// server-side record naming the factor and the verifier's reason, even though
// the caller only ever sees a uniform 401.
func TestLoginService_FinishLogin_ReportsRejectionWithVerifierReason(t *testing.T) {
	auditor := &recordingAuditor{}
	fake := &fakeLoginClient{finishErr: &qrdbtransport.FactorVerificationError{
		FactorID: "factor-7",
		Reason:   "webauthn assertion verification failed: validate login",
	}}
	svc := NewLoginService(fake)
	svc.SetCeremonyAuditor(auditor)

	_, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		SessionID: "ceremony-1",
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte("QBWMLoIzncNDpMLLKDQrpkWIXd_ZYaRHP7wpWhFjA3s"),
		MethodMetadata: map[string]string{
			"session_id":    "rp-1",
			"credential_id": "QBWMLoIzncNDpMLLKDQrpkWIXd_ZYaRHP7wpWhFjA3s",
		},
	})
	if err == nil {
		t.Fatal("expected the rejection to be returned")
	}
	if len(auditor.rejections) != 1 {
		t.Fatalf("expected exactly one rejection record, got %d", len(auditor.rejections))
	}

	got := auditor.rejections[0]
	if got.Stage != LoginStageFinish {
		t.Errorf("stage = %q, want %q", got.Stage, LoginStageFinish)
	}
	if got.FactorType != string(qrdbtransport.FactorTypeFIDO2) {
		t.Errorf("factor type = %q, want fido2", got.FactorType)
	}
	if got.SessionID != "ceremony-1" {
		t.Errorf("session id = %q, want ceremony-1", got.SessionID)
	}
	if got.FactorID != "factor-7" {
		t.Errorf("factor id = %q, want factor-7 (resolution succeeded, verification failed)", got.FactorID)
	}
	if !strings.Contains(got.Reason, "webauthn assertion verification failed") {
		t.Errorf("reason = %q, want the verifier's own reason", got.Reason)
	}
	if got.LookupKeyLen != 43 {
		t.Errorf("lookup key len = %d, want 43 (the base64url credential-id string)", got.LookupKeyLen)
	}
	wantDigest := sha256.Sum256([]byte("QBWMLoIzncNDpMLLKDQrpkWIXd_ZYaRHP7wpWhFjA3s"))
	if got.LookupKeyDigest != hex.EncodeToString(wantDigest[:]) {
		t.Errorf("lookup key digest = %q, want the sha256 hex of the presented key", got.LookupKeyDigest)
	}
	if len(got.MetadataKeys) != 2 || got.MetadataKeys[0] != "credential_id" || got.MetadataKeys[1] != "session_id" {
		t.Errorf("metadata keys = %v, want the sorted key names", got.MetadataKeys)
	}
}

// TestLoginService_FinishLogin_RejectionNeverCarriesProofMaterial asserts that
// no part of the caller's proof — the signature bytes or any metadata VALUE —
// reaches the rejection record. The record is written to logs and to the audit
// store; a credential in either is a disclosure, and the assertion blob would
// be the largest one.
func TestLoginService_FinishLogin_RejectionNeverCarriesProofMaterial(t *testing.T) {
	auditor := &recordingAuditor{}
	fake := &fakeLoginClient{finishErr: errors.New("rejected")}
	svc := NewLoginService(fake)
	svc.SetCeremonyAuditor(auditor)

	const secretAssertion = "SECRET-ASSERTION-BLOB"
	const secretSession = "SECRET-RP-SESSION"
	_, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		SessionID:      "ceremony-2",
		Type:           qrdbtransport.FactorTypeFIDO2,
		LookupKey:      []byte("lookup"),
		Signature:      []byte(secretAssertion),
		MethodMetadata: map[string]string{"session_id": secretSession},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(auditor.rejections) != 1 {
		t.Fatalf("expected one rejection, got %d", len(auditor.rejections))
	}

	got := auditor.rejections[0]
	for _, field := range []string{got.Reason, got.LookupKeyDigest, got.SessionID, got.FactorID, got.Subject} {
		if strings.Contains(field, secretAssertion) || strings.Contains(field, secretSession) {
			t.Fatalf("rejection record leaked proof material in %q", field)
		}
	}
	for _, key := range got.MetadataKeys {
		if key == secretSession {
			t.Fatal("rejection record recorded a metadata VALUE, not just its key")
		}
	}
	// The presented lookup key itself must not appear verbatim either.
	if got.LookupKeyDigest == "lookup" {
		t.Fatal("rejection record carried the raw lookup key instead of its digest")
	}
}

// TestLoginService_FinishLogin_SuccessReportsNothing asserts the reporting path
// fires ONLY on rejection. A record per successful login would drown the signal
// this seam exists to produce.
func TestLoginService_FinishLogin_SuccessReportsNothing(t *testing.T) {
	auditor := &recordingAuditor{}
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "b1"}}
	svc := NewLoginService(fake)
	svc.SetCeremonyAuditor(auditor)

	if _, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte("k"),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auditor.rejections) != 0 {
		t.Fatalf("expected no rejection records on success, got %d", len(auditor.rejections))
	}
}

// TestLoginService_FinishLogin_RefreshFailureReportsVerifiedRejection asserts
// the one rejection that happens AFTER the proof passed names the subject and
// the factor that authorised it — the record cannot be built from the request
// alone, so it is built from the response.
func TestLoginService_FinishLogin_RefreshFailureReportsVerifiedRejection(t *testing.T) {
	auditor := &recordingAuditor{}
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{
		Bearer:   "b1",
		FactorID: "factor-9",
		Identity: &qrdbtransport.Identity{Subject: "alice@example.com", TenantID: "acme"},
	}}
	svc := NewLoginService(fake)
	svc.SetCeremonyAuditor(auditor)
	svc.SetSessionRefreshIssuer(&fakeRefreshIssuer{err: errors.New("refresh store unavailable")})

	if _, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte("k"),
	}); err == nil {
		t.Fatal("expected the refresh failure to reject the login")
	}
	if len(auditor.rejections) != 1 {
		t.Fatalf("expected one rejection, got %d", len(auditor.rejections))
	}
	got := auditor.rejections[0]
	if got.Subject != "alice@example.com" {
		t.Errorf("subject = %q, want the authenticated subject", got.Subject)
	}
	if got.TenantID != "acme" {
		t.Errorf("tenant = %q, want the authenticated tenant", got.TenantID)
	}
	if got.FactorID != "factor-9" {
		t.Errorf("factor id = %q, want the factor that authorised the login", got.FactorID)
	}
}

// TestLoginService_BeginLogin_ReportsRejectionWithSubject asserts the begin
// half reports too, and that it carries the caller's hint — the only stage
// where the ceremony knows who was being asked for.
func TestLoginService_BeginLogin_ReportsRejectionWithSubject(t *testing.T) {
	auditor := &recordingAuditor{}
	fake := &fakeLoginClient{beginErr: errors.New("relying party unavailable")}
	svc := NewLoginService(fake)
	svc.SetCeremonyAuditor(auditor)

	if _, err := svc.BeginLogin(context.Background(), &qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorTypeFIDO2,
		Hint:     "alice@example.com",
		TenantID: "acme",
	}); err == nil {
		t.Fatal("expected an error")
	}
	if len(auditor.rejections) != 1 {
		t.Fatalf("expected one rejection, got %d", len(auditor.rejections))
	}
	got := auditor.rejections[0]
	if got.Stage != LoginStageBegin {
		t.Errorf("stage = %q, want begin", got.Stage)
	}
	if got.Subject != "alice@example.com" {
		t.Errorf("subject = %q, want the caller's hint", got.Subject)
	}
	if got.TenantID != "acme" {
		t.Errorf("tenant = %q, want acme", got.TenantID)
	}
	if got.Reason != "relying party unavailable" {
		t.Errorf("reason = %q, want the underlying error", got.Reason)
	}
}

// TestLoginService_ReportsNilRequestRejection asserts the malformed-call path
// still reports rather than returning silently, and does not panic on the nil
// request it is describing.
func TestLoginService_ReportsNilRequestRejection(t *testing.T) {
	auditor := &recordingAuditor{}
	svc := NewLoginService(&fakeLoginClient{})
	svc.SetCeremonyAuditor(auditor)

	if _, err := svc.FinishLogin(context.Background(), nil); err == nil {
		t.Fatal("expected LoginRequestNilError")
	}
	if _, err := svc.BeginLogin(context.Background(), nil); err == nil {
		t.Fatal("expected LoginRequestNilError")
	}
	if len(auditor.rejections) != 2 {
		t.Fatalf("expected both nil-request rejections to be reported, got %d", len(auditor.rejections))
	}
}

// TestLoginService_RejectionWithoutAuditorIsSafe asserts an unwired facade
// still rejects normally. The reporting seam must never be able to turn a 401
// into a panic.
func TestLoginService_RejectionWithoutAuditorIsSafe(t *testing.T) {
	svc := NewLoginService(&fakeLoginClient{finishErr: errors.New("rejected")})

	if _, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: []byte("k"),
	}); err == nil {
		t.Fatal("expected the rejection to be returned")
	}

	// An explicitly cleared auditor is the same case, reached differently.
	svc.SetCeremonyAuditor(nil)
	if _, err := svc.BeginLogin(context.Background(), nil); err == nil {
		t.Fatal("expected LoginRequestNilError")
	}
}

// TestDescribeFinishRejection_NilRequest asserts the describer tolerates the
// request being nil — the LoginRequestNilError path has nothing to read.
func TestDescribeFinishRejection_NilRequest(t *testing.T) {
	got := describeFinishRejection(nil, &LoginRequestNilError{Op: "FinishLogin"})
	if got.Stage != LoginStageFinish {
		t.Errorf("stage = %q, want finish", got.Stage)
	}
	if got.Reason == "" {
		t.Error("reason must describe the failure even with no request")
	}
	if got.LookupKeyDigest != "" || got.LookupKeyLen != 0 || got.MetadataKeys != nil {
		t.Error("no request means no request-derived fields")
	}
}

// TestLookupKeyDigest_EmptyStaysEmpty asserts an absent lookup key reads as
// absent rather than as "the digest of nothing", which would otherwise be a
// constant an operator could mistake for a real value.
func TestLookupKeyDigest_EmptyStaysEmpty(t *testing.T) {
	if digest := lookupKeyDigest(nil); digest != "" {
		t.Errorf("digest of no key = %q, want empty", digest)
	}
	if digest := lookupKeyDigest([]byte{}); digest != "" {
		t.Errorf("digest of empty key = %q, want empty", digest)
	}
}

// TestRejectionFactorID_OnlyWhenTheErrorNamesOne asserts the factor id is
// reported when — and only when — the error actually carries one. Inventing
// one would erase the distinction between "not enrolled" and "enrolled but the
// proof failed", which is the whole diagnostic value of the field.
func TestRejectionFactorID_OnlyWhenTheErrorNamesOne(t *testing.T) {
	if got := rejectionFactorID(errors.New("generic")); got != "" {
		t.Errorf("generic error yielded factor id %q, want empty", got)
	}
	if got := rejectionFactorID(&qrdbtransport.FactorVerificationError{FactorID: "f1"}); got != "f1" {
		t.Errorf("verification error yielded %q, want f1", got)
	}
	if got := rejectionFactorID(&qrdbtransport.FactorNotFoundError{FactorID: "f2"}); got != "f2" {
		t.Errorf("not-found error yielded %q, want f2", got)
	}
	// The live go-quicbaas#484 case: a not-found error with no id at all.
	if got := rejectionFactorID(&qrdbtransport.FactorNotFoundError{}); got != "" {
		t.Errorf("anonymous not-found yielded %q, want empty", got)
	}
}

// TestRejectionRecordBoundsClientControlledStrings asserts an unauthenticated
// caller cannot choose how much gets written to the log and the audit store.
//
// The session id and the metadata key names come straight off the wire, and
// qrdb's LoginSessionNotFoundError renders the session id into its message —
// so a single POST could otherwise write an arbitrarily large record. This is
// the bound on that, and it is the reason the reporting seam is not itself a
// log-amplification primitive.
func TestRejectionRecordBoundsClientControlledStrings(t *testing.T) {
	huge := strings.Repeat("A", 100_000)
	metadata := map[string]string{}
	for i := 0; i < 200; i++ {
		metadata[huge+string(rune('a'+i%26))] = "v"
	}

	got := describeFinishRejection(&qrdbtransport.FinishLoginRequest{
		SessionID:      huge,
		Type:           qrdbtransport.FactorType(huge),
		MethodMetadata: metadata,
	}, &qrdbtransport.LoginSessionNotFoundError{SessionID: huge})

	if len(got.SessionID) > maxRecordedFieldLen+len(truncationMarker) {
		t.Errorf("session id length %d exceeds the bound", len(got.SessionID))
	}
	if len(got.FactorType) > maxRecordedFieldLen+len(truncationMarker) {
		t.Errorf("factor type length %d exceeds the bound", len(got.FactorType))
	}
	if len(got.Reason) > maxRecordedFieldLen+len(truncationMarker) {
		t.Errorf("reason length %d exceeds the bound", len(got.Reason))
	}
	if !strings.HasSuffix(got.SessionID, truncationMarker) {
		t.Error("a truncated value must say so, or a reader will read it as complete")
	}
	if len(got.MetadataKeys) > maxRecordedMetadataKeys {
		t.Errorf("kept %d metadata keys, want at most %d", len(got.MetadataKeys), maxRecordedMetadataKeys)
	}
	for _, key := range got.MetadataKeys {
		if len(key) > maxRecordedFieldLen+len(truncationMarker) {
			t.Errorf("metadata key length %d exceeds the bound", len(key))
		}
	}
}

// TestRejectionRecordLeavesNormalValuesIntact asserts the bound never fires on
// a real ceremony. A truncation marker on a legitimate session id would send
// an operator chasing a corruption that is not there.
func TestRejectionRecordLeavesNormalValuesIntact(t *testing.T) {
	const sessionID = "6892026b-4463-4e7a-ab7e-cf58d5766a42"
	const credentialID = "QBWMLoIzncNDpMLLKDQrpkWIXd_ZYaRHP7wpWhFjA3s"

	got := describeFinishRejection(&qrdbtransport.FinishLoginRequest{
		SessionID:      sessionID,
		Type:           qrdbtransport.FactorTypeFIDO2,
		MethodMetadata: map[string]string{"session_id": "rp-1", "credential_id": credentialID},
	}, &qrdbtransport.FactorNotFoundError{})

	if got.SessionID != sessionID {
		t.Errorf("session id = %q, want it untouched", got.SessionID)
	}
	if got.FactorType != "fido2" {
		t.Errorf("factor type = %q, want fido2 untouched", got.FactorType)
	}
	if strings.Contains(got.Reason, truncationMarker) {
		t.Errorf("reason = %q, want it untouched", got.Reason)
	}
}
