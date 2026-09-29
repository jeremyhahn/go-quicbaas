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

// TestClient_OpenWorkRoutesUnderTheSDLCService pins the verb's service,
// method and request type, which every transport dispatches on.
func TestClient_OpenWorkRoutesUnderTheSDLCService(t *testing.T) {
	tr := &fakeTransport{capable: true}
	tr.resp = &OpenWork{ProjectID: "p1", Agent: "agent-api", PullRequests: []OpenPullRequest{{Number: 7}}}

	work, err := NewClient(tr).OpenWork(context.Background(), "t", "p1")
	if err != nil || tr.service != serviceName || tr.method != "OpenWork" {
		t.Fatalf("OpenWork: %v (%s/%s)", err, tr.service, tr.method)
	}
	if work.Agent != "agent-api" || len(work.PullRequests) != 1 {
		t.Fatalf("OpenWork = %+v", work)
	}
	if r, ok := tr.req.(*OpenWorkRequest); !ok || r.TenantID != "t" || r.ProjectID != "p1" {
		t.Fatalf("OpenWork sent %T = %+v", tr.req, tr.req)
	}
}

// TestClient_OpenWorkRefusesBadInputAndBadAnswers is the error half.
func TestClient_OpenWorkRefusesBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)

	if _, err := c.OpenWork(ctx, "t", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("empty project: %v", err)
	}
	var nilCtx context.Context
	if _, err := c.OpenWork(nilCtx, "t", "p1"); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("nil ctx: %v", err)
	}
	tr.resp = "not open work"
	if _, err := c.OpenWork(ctx, "t", "p1"); err == nil {
		t.Error("a wrong response type was accepted")
	}
	tr.resp, tr.err = nil, errors.New("boom")
	if _, err := c.OpenWork(ctx, "t", "p1"); err == nil {
		t.Error("a transport error was swallowed")
	}
	if _, err := NewClient(&fakeTransport{capable: false}).OpenWork(ctx, "t", "p1"); err == nil {
		t.Error("OpenWork was attempted over a transport without the service")
	}
}
