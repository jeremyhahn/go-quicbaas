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

func TestClient_AdoptIssueRoutesUnderTheService(t *testing.T) {
	tr := &fakeTransport{capable: true, resp: &Adoption{TaskID: "task-1", Issue: 141, PullRequest: 194, Branch: "issue/141-x"}}
	got, err := NewClient(tr).AdoptIssue(context.Background(), &AdoptIssueRequest{TenantID: "t", ProjectID: "p1", Issue: 141, PullRequest: 194})
	if err != nil || got.TaskID != "task-1" || tr.service != serviceName || tr.method != "AdoptIssue" {
		t.Fatalf("AdoptIssue: %+v %v (%s/%s)", got, err, tr.service, tr.method)
	}
	if r := tr.req.(*AdoptIssueRequest); r.Issue != 141 || r.PullRequest != 194 {
		t.Fatalf("request = %+v", r)
	}
}

func TestClient_AdoptIssueRefusesBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	var nilCtx context.Context
	for name, call := range map[string]func() error{
		"nil request": func() error { _, err := c.AdoptIssue(ctx, nil); return err },
		"nil context": func() error { _, err := c.AdoptIssue(nilCtx, &AdoptIssueRequest{ProjectID: "p", Issue: 1}); return err },
		"no project":  func() error { _, err := c.AdoptIssue(ctx, &AdoptIssueRequest{Issue: 1}); return err },
		"no issue":    func() error { _, err := c.AdoptIssue(ctx, &AdoptIssueRequest{ProjectID: "p"}); return err },
		"negative pull": func() error {
			_, err := c.AdoptIssue(ctx, &AdoptIssueRequest{ProjectID: "p", Issue: 1, PullRequest: -1})
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
	tr.resp = "not an adoption"
	if _, err := c.AdoptIssue(ctx, &AdoptIssueRequest{ProjectID: "p", Issue: 1}); err == nil {
		t.Fatal("wrong response type accepted")
	}
	tr.resp, tr.err = nil, errors.New("boom")
	if _, err := c.AdoptIssue(ctx, &AdoptIssueRequest{ProjectID: "p", Issue: 1}); err == nil {
		t.Fatal("transport error swallowed")
	}
}
