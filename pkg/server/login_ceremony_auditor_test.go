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
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// capturingAuditLogger records the events written to the audit plane.
type capturingAuditLogger struct {
	events []*iam.AuditEvent
	err    error
}

func (c *capturingAuditLogger) Log(_ context.Context, event *iam.AuditEvent) error {
	if c.err != nil {
		return c.err
	}
	c.events = append(c.events, event)
	return nil
}

func (c *capturingAuditLogger) LogAsync(*iam.AuditEvent) {}

func (c *capturingAuditLogger) Query(context.Context, *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	return nil, 0, nil
}

func (c *capturingAuditLogger) GetEvent(context.Context, string, string) (*iam.AuditEvent, error) {
	return nil, nil
}

func (c *capturingAuditLogger) DeleteOlderThan(context.Context, string, time.Time) (int, error) {
	return 0, nil
}

func (c *capturingAuditLogger) Close() error { return nil }

// stubAuditResolver hands out one audit logger, or fails.
type stubAuditResolver struct {
	logger iam.AuditLogger
	err    error
}

func (s *stubAuditResolver) GetAuditLogger(context.Context) (iam.AuditLogger, error) {
	return s.logger, s.err
}

// testServerLogger builds a quiet logger for the auditor under test.
func testServerLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}
	return log
}

// sampleRejection is the shape go-quicbaas#484 actually produced on the live
// node: a fido2 finish whose lookup key resolved to no enrolled factor.
func sampleRejection() service.LoginRejection {
	return service.LoginRejection{
		Stage:           service.LoginStageFinish,
		FactorType:      "fido2",
		SessionID:       "ceremony-1",
		TenantID:        "system",
		FactorID:        "",
		LookupKeyDigest: "54c2a720bc2566df1bc49a9b32f01912b4bedd37b85c97d23493acd83024749e",
		LookupKeyLen:    43,
		MetadataKeys:    []string{"credential_id", "session_id"},
		Reason:          "transport: factor not found: ",
	}
}

// TestLoginCeremonyAuditor_RecordsAuthenticationFailure asserts the rejection
// reaches the audit plane as an authentication FAILURE carrying the diagnostic
// detail — the parity with the success path that go-quicbaas#484 asked for.
func TestLoginCeremonyAuditor_RecordsAuthenticationFailure(t *testing.T) {
	sink := &capturingAuditLogger{}
	auditor := newLoginCeremonyAuditor(testServerLogger(t), &stubAuditResolver{logger: sink})

	auditor.RecordLoginRejection(context.Background(), sampleRejection())

	if len(sink.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(sink.events))
	}
	event := sink.events[0]
	if event.Outcome != iam.AuditOutcomeFailure {
		t.Errorf("outcome = %q, want failure", event.Outcome)
	}
	if event.Action != "iam.login.finish" {
		t.Errorf("action = %q, want iam.login.finish (the same action the success path records)", event.Action)
	}
	if event.Category != iam.AuditCategoryAuthentication {
		t.Errorf("category = %q, want authentication", event.Category)
	}
	if event.TenantID != "system" {
		t.Errorf("tenant = %q, want system", event.TenantID)
	}
	if event.Details["reason"] != "transport: factor not found: " {
		t.Errorf("details reason = %v, want the server-side reason", event.Details["reason"])
	}
	if event.Details["factor_type"] != "fido2" {
		t.Errorf("details factor_type = %v, want fido2", event.Details["factor_type"])
	}
	if event.Details["lookup_key_len"] != 43 {
		t.Errorf("details lookup_key_len = %v, want 43", event.Details["lookup_key_len"])
	}
}

// TestLoginCeremonyAuditor_BeginUsesItsOwnAction asserts the two ceremony
// halves are distinguishable in the audit trail. An operator investigating a
// login cannot tell "the challenge was never issued" from "the proof was
// refused" if both file under one name.
func TestLoginCeremonyAuditor_BeginUsesItsOwnAction(t *testing.T) {
	sink := &capturingAuditLogger{}
	auditor := newLoginCeremonyAuditor(testServerLogger(t), &stubAuditResolver{logger: sink})

	rejection := sampleRejection()
	rejection.Stage = service.LoginStageBegin
	rejection.Subject = "alice@example.com"
	auditor.RecordLoginRejection(context.Background(), rejection)

	if len(sink.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(sink.events))
	}
	if sink.events[0].Action != "iam.login.begin" {
		t.Errorf("action = %q, want iam.login.begin", sink.events[0].Action)
	}
	if sink.events[0].Actor == nil || sink.events[0].Actor.UserID != "alice@example.com" {
		t.Errorf("actor = %+v, want the caller's hint as the subject", sink.events[0].Actor)
	}
}

// TestLoginCeremonyAuditor_UnknownStageStillFiles asserts a stage the map does
// not know still produces an event rather than being dropped. A record that
// disappears because an enum grew is the failure mode this whole seam exists
// to remove.
func TestLoginCeremonyAuditor_UnknownStageStillFiles(t *testing.T) {
	sink := &capturingAuditLogger{}
	auditor := newLoginCeremonyAuditor(testServerLogger(t), &stubAuditResolver{logger: sink})

	rejection := sampleRejection()
	rejection.Stage = service.LoginStage("renew")
	auditor.RecordLoginRejection(context.Background(), rejection)

	if len(sink.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(sink.events))
	}
	if sink.events[0].Action != "iam.login" {
		t.Errorf("action = %q, want the iam.login fallback", sink.events[0].Action)
	}
}

// TestLoginCeremonyAuditor_NoTenantSkipsTheAuditEvent asserts the auditor does
// not invent a tenant.
//
// qrdb's FinishLoginRequest carries no tenant, so a rejection before the
// principal resolves has none to file under. Stamping a default would put a
// FALSE tenant on the record in exactly the case the record exists to explain;
// the WARN log (asserted by the sibling test) is the record instead.
func TestLoginCeremonyAuditor_NoTenantSkipsTheAuditEvent(t *testing.T) {
	sink := &capturingAuditLogger{}
	auditor := newLoginCeremonyAuditor(testServerLogger(t), &stubAuditResolver{logger: sink})

	rejection := sampleRejection()
	rejection.TenantID = ""
	auditor.RecordLoginRejection(context.Background(), rejection)

	if len(sink.events) != 0 {
		t.Fatalf("expected no audit event without a tenant, got %d", len(sink.events))
	}
}

// TestLoginCeremonyAuditor_TenantFromContextIsUsed asserts a tenant carried on
// the request context is honoured, so a transport that scopes the call does
// get the audit event the tenant-less case cannot have.
func TestLoginCeremonyAuditor_TenantFromContextIsUsed(t *testing.T) {
	sink := &capturingAuditLogger{}
	auditor := newLoginCeremonyAuditor(testServerLogger(t), &stubAuditResolver{logger: sink})

	rejection := sampleRejection()
	rejection.TenantID = ""
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	auditor.RecordLoginRejection(ctx, rejection)

	if len(sink.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(sink.events))
	}
	if sink.events[0].TenantID != "acme" {
		t.Errorf("tenant = %q, want acme from the context", sink.events[0].TenantID)
	}
}

// TestLoginCeremonyAuditor_AuditFailureIsNotFatal asserts a broken audit plane
// cannot break, or even alter, the ceremony that is already failing. The
// auditor swallows its own errors by contract (service.LoginCeremonyAuditor).
func TestLoginCeremonyAuditor_AuditFailureIsNotFatal(t *testing.T) {
	cases := map[string]*stubAuditResolver{
		"resolver error": {err: errors.New("audit store unavailable")},
		"nil logger":     {logger: nil},
		"write error":    {logger: &capturingAuditLogger{err: errors.New("disk full")}},
	}
	for name, resolver := range cases {
		t.Run(name, func(t *testing.T) {
			auditor := newLoginCeremonyAuditor(testServerLogger(t), resolver)
			auditor.RecordLoginRejection(context.Background(), sampleRejection())
		})
	}
}

// TestLoginCeremonyAuditor_MissingDependenciesAreSafe asserts every degraded
// wiring still returns rather than panicking on a path that is, by definition,
// already handling a failure.
func TestLoginCeremonyAuditor_MissingDependenciesAreSafe(t *testing.T) {
	newLoginCeremonyAuditor(nil, nil).RecordLoginRejection(context.Background(), sampleRejection())
	newLoginCeremonyAuditor(testServerLogger(t), nil).RecordLoginRejection(context.Background(), sampleRejection())
	newLoginCeremonyAuditor(nil, &stubAuditResolver{logger: &capturingAuditLogger{}}).
		RecordLoginRejection(context.Background(), sampleRejection())

	var nilAuditor *loginCeremonyAuditor
	nilAuditor.RecordLoginRejection(context.Background(), sampleRejection())
}

// TestNewLoginCeremonyAuditorFor_FacadeWithoutAuditSurface asserts a facade
// that exposes no audit logger still yields a working auditor — the WARN
// record, which is the half that makes the failure diagnosable, must not
// depend on the audit plane being composed.
func TestNewLoginCeremonyAuditorFor_FacadeWithoutAuditSurface(t *testing.T) {
	auditor := newLoginCeremonyAuditorFor(testServerLogger(t), nil)
	if auditor == nil {
		t.Fatal("expected an auditor even with no facade")
	}
	if auditor.audit != nil {
		t.Error("expected no audit resolver when the facade exposes none")
	}
	auditor.RecordLoginRejection(context.Background(), sampleRejection())
}

// Compile-time proof that the concrete auditor is what the facade seam takes,
// so a signature drift fails the build rather than silently unwiring the
// rejection record.
var _ service.LoginCeremonyAuditor = newLoginCeremonyAuditor(nil, nil)
