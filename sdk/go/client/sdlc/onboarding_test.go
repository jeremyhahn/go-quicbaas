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

package sdlc

import (
	"context"
	"errors"
	"testing"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestClient_RoutesTheOnboardingVerbs pins that both verbs route under the
// SDLC service with the request types the transports dispatch on.
func TestClient_RoutesTheOnboardingVerbs(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)

	tr.resp = &OnboardingReport{ProjectID: "p1", WebhookEnsured: true, LabelsCreated: []string{"task:ready"}}
	report, err := c.Onboard(ctx, "t", "p1")
	if err != nil || tr.service != serviceName || tr.method != "Onboard" {
		t.Fatalf("Onboard: %v (%s/%s)", err, tr.service, tr.method)
	}
	if !report.WebhookEnsured || len(report.LabelsCreated) != 1 {
		t.Fatalf("OnboardingReport = %+v", report)
	}
	if r, ok := tr.req.(*OnboardRequest); !ok || r.TenantID != "t" || r.ProjectID != "p1" {
		t.Fatalf("Onboard sent %T = %+v", tr.req, tr.req)
	}

	tr.resp = &ConformanceReport{ProjectID: "p1", Conformant: false,
		Findings: []ConformanceFinding{{Class: "webhook_missing", Check: "webhook", Remedy: "onboard the project"}}}
	conformance, err := c.Conformance(ctx, "t", "p1")
	if err != nil || tr.method != "Conformance" {
		t.Fatalf("Conformance: %v (%s)", err, tr.method)
	}
	if conformance.Conformant || len(conformance.Findings) != 1 {
		t.Fatalf("ConformanceReport = %+v", conformance)
	}
	if r, ok := tr.req.(*ConformanceRequest); !ok || r.ProjectID != "p1" {
		t.Fatalf("Conformance sent %T = %+v", tr.req, tr.req)
	}
}

// TestClient_OnboardingVerbsRefuseBadInputAndBadAnswers is the error half.
func TestClient_OnboardingVerbsRefuseBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)

	if _, err := c.Onboard(ctx, "t", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("empty project on Onboard: %v", err)
	}
	if _, err := c.Conformance(ctx, "t", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("empty project on Conformance: %v", err)
	}
	var nilCtx context.Context
	if _, err := c.Onboard(nilCtx, "t", "p1"); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("nil ctx on Onboard: %v", err)
	}
	if _, err := c.Conformance(nilCtx, "t", "p1"); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("nil ctx on Conformance: %v", err)
	}

	tr.resp = "not a report"
	if _, err := c.Onboard(ctx, "t", "p1"); err == nil {
		t.Error("Onboard accepted a wrong response type")
	}
	if _, err := c.Conformance(ctx, "t", "p1"); err == nil {
		t.Error("Conformance accepted a wrong response type")
	}

	tr.resp, tr.err = nil, errors.New("boom")
	if _, err := c.Onboard(ctx, "t", "p1"); err == nil {
		t.Error("Onboard swallowed a transport error")
	}
	if _, err := c.Conformance(ctx, "t", "p1"); err == nil {
		t.Error("Conformance swallowed a transport error")
	}
}

// TestClient_OnboardingVerbsFailClosedOnAnUnwiredTransport.
//
// A transport that does not declare the service must refuse by NAME rather
// than surface a routing miss — the "BaasTransportNotSupportedError anywhere
// is a regression" rule only works if the refusal is legible.
func TestClient_OnboardingVerbsFailClosedOnAnUnwiredTransport(t *testing.T) {
	c := NewClient(&fakeTransport{capable: false})

	if _, err := c.Onboard(context.Background(), "t", "p1"); err == nil {
		t.Error("Onboard was attempted over a transport without the service")
	}
	if _, err := c.Conformance(context.Background(), "t", "p1"); err == nil {
		t.Error("Conformance was attempted over a transport without the service")
	}
}
