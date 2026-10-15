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

func TestClient_OnboardRepoRoutesTheRequest(t *testing.T) {
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	req := &RepoOnboardRequest{TenantID: "t", Repo: "web", Like: "p1", MaxConcurrency: 2, Paused: true, Preflight: true}
	tr.resp = &RepoOnboardingReport{Repo: "acme/web", Steps: []RepoOnboardStep{{Step: "project", Outcome: OnboardCreated}}}

	report, err := c.OnboardRepo(context.Background(), req)

	if err != nil || tr.service != serviceName || tr.method != "OnboardRepo" {
		t.Fatalf("OnboardRepo: %v (%s/%s)", err, tr.service, tr.method)
	}
	if tr.req != req {
		t.Fatalf("the transport carried %+v, not the request", tr.req)
	}
	if report.Repo != "acme/web" || len(report.Steps) != 1 {
		t.Fatalf("RepoOnboardingReport = %+v", report)
	}
}

func TestClient_OnboardRepoRefusesBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)

	for name, req := range map[string]*RepoOnboardRequest{
		"nil request": nil,
		"no repo":     {Like: "p1"},
		"no template": {Repo: "web"},
	} {
		if _, err := c.OnboardRepo(ctx, req); !sdkerrors.IsInvalidInput(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var nilCtx context.Context
	if _, err := c.OnboardRepo(nilCtx, &RepoOnboardRequest{Repo: "web", Like: "p1"}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("nil ctx: %v", err)
	}

	tr.resp = "not a report"
	if _, err := c.OnboardRepo(ctx, &RepoOnboardRequest{Repo: "web", Like: "p1"}); err == nil {
		t.Error("OnboardRepo accepted a wrong response type")
	}
	tr.resp, tr.err = nil, errors.New("boom")
	if _, err := c.OnboardRepo(ctx, &RepoOnboardRequest{Repo: "web", Like: "p1"}); err == nil {
		t.Error("OnboardRepo swallowed a transport error")
	}
	if _, err := NewClient(&fakeTransport{}).OnboardRepo(ctx, &RepoOnboardRequest{Repo: "web", Like: "p1"}); err == nil {
		t.Error("OnboardRepo was attempted over a transport without the service")
	}
}
