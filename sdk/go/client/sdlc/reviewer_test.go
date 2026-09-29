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

func TestClient_ReviewerVerbsRouteUnderTheService(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true, resp: &ReviewerSelection{ProjectID: "p1", PullRequest: 41, Profile: "qwen", Overridden: true}}
	c := NewClient(tr)

	got, err := c.Reviewer(ctx, "t", "p1", 41)
	if err != nil || got.Profile != "qwen" || tr.service != serviceName || tr.method != "Reviewer" {
		t.Fatalf("Reviewer: %+v %v (%s/%s)", got, err, tr.service, tr.method)
	}
	if r := tr.req.(*ReviewerRequest); r.TenantID != "t" || r.ProjectID != "p1" || r.PullRequest != 41 {
		t.Fatalf("ReviewerRequest = %+v", r)
	}

	if _, err := c.SetReviewer(ctx, &SetReviewerRequest{TenantID: "t", ProjectID: "p1", PullRequest: 41, Profile: "qwen"}); err != nil || tr.method != "SetReviewer" {
		t.Fatalf("SetReviewer: %v (%s)", err, tr.method)
	}
	if r := tr.req.(*SetReviewerRequest); r.Profile != "qwen" {
		t.Fatalf("SetReviewerRequest = %+v", r)
	}

	if _, err := c.ClearReviewer(ctx, "t", "p1", 41); err != nil || tr.method != "ClearReviewer" {
		t.Fatalf("ClearReviewer: %v (%s)", err, tr.method)
	}
}

func TestClient_ReviewerVerbsRefuseBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	var nilCtx context.Context
	for name, call := range map[string]func() error{
		"no project":        func() error { _, err := c.Reviewer(ctx, "t", "", 41); return err },
		"pull request zero": func() error { _, err := c.ClearReviewer(ctx, "t", "p1", 0); return err },
		"nil context":       func() error { _, err := c.Reviewer(nilCtx, "t", "p1", 41); return err },
		"nil request":       func() error { _, err := c.SetReviewer(ctx, nil); return err },
		"no profile": func() error {
			_, err := c.SetReviewer(ctx, &SetReviewerRequest{ProjectID: "p1", PullRequest: 41})
			return err
		},
	} {
		if err := call(); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s: want invalid input, got %v", name, err)
		}
	}
	if tr.method != "" {
		t.Fatalf("a refused call reached the transport: %s", tr.method)
	}

	tr.resp = "not a selection"
	if _, err := c.Reviewer(ctx, "t", "p1", 41); err == nil {
		t.Fatal("wrong response type accepted")
	}
	tr.resp, tr.err = nil, errors.New("boom")
	if _, err := c.SetReviewer(ctx, &SetReviewerRequest{ProjectID: "p1", PullRequest: 41, Profile: "qwen"}); err == nil {
		t.Fatal("transport error swallowed")
	}

	unwired := NewClient(&fakeTransport{capable: false})
	if _, err := unwired.Reviewer(ctx, "t", "p1", 41); err == nil {
		t.Fatal("a transport without the service was used")
	}
}
