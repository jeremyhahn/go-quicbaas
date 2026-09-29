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

func TestClient_OperatorVerbsRouteUnderTheService(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true, resp: &ForgeIssue{Number: 88, Title: "Follow-up"}}
	c := NewClient(tr)

	issue, err := c.FileIssue(ctx, &FileIssueRequest{TenantID: "t", ProjectID: "p1", Title: "Follow-up", Labels: []string{"task:ready"}, Parent: 12})
	if err != nil || issue.Number != 88 || tr.service != serviceName || tr.method != "FileIssue" {
		t.Fatalf("FileIssue: %+v %v (%s/%s)", issue, err, tr.service, tr.method)
	}
	if r := tr.req.(*FileIssueRequest); r.Parent != 12 || r.Labels[0] != "task:ready" {
		t.Fatalf("FileIssueRequest = %+v", r)
	}

	tr.resp = &CollaboratorsResponse{Users: []ForgeUser{{Login: "acme"}}, Count: 1}
	users, err := c.Collaborators(ctx, "t", "p1")
	if err != nil || users.Count != 1 || tr.method != "Collaborators" {
		t.Fatalf("Collaborators: %+v %v (%s)", users, err, tr.method)
	}
	if r := tr.req.(*CollaboratorsRequest); r.TenantID != "t" || r.ProjectID != "p1" {
		t.Fatalf("CollaboratorsRequest = %+v", r)
	}
}

func TestClient_OperatorVerbsRefuseBadInputAndBadAnswers(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true}
	c := NewClient(tr)
	var nilCtx context.Context
	for name, call := range map[string]func() error{
		"nil request": func() error { _, err := c.FileIssue(ctx, nil); return err },
		"nil context": func() error { _, err := c.FileIssue(nilCtx, &FileIssueRequest{ProjectID: "p", Title: "t"}); return err },
		"no project":  func() error { _, err := c.FileIssue(ctx, &FileIssueRequest{Title: "t"}); return err },
		"no title":    func() error { _, err := c.FileIssue(ctx, &FileIssueRequest{ProjectID: "p", Title: " "}); return err },
		"a negative parent": func() error {
			_, err := c.FileIssue(ctx, &FileIssueRequest{ProjectID: "p", Title: "t", Parent: -1})
			return err
		},
		"collaborators without a project": func() error { _, err := c.Collaborators(ctx, "t", ""); return err },
		"collaborators without a context": func() error { _, err := c.Collaborators(nilCtx, "t", "p"); return err },
	} {
		if err := call(); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s: want invalid input, got %v", name, err)
		}
	}
	if tr.method != "" {
		t.Fatalf("a refused call reached the transport: %s", tr.method)
	}

	tr.resp = "not an issue"
	if _, err := c.FileIssue(ctx, &FileIssueRequest{ProjectID: "p", Title: "t"}); err == nil {
		t.Fatal("wrong response type accepted")
	}
	if _, err := c.Collaborators(ctx, "t", "p"); err == nil {
		t.Fatal("wrong response type accepted")
	}
	tr.resp, tr.err = nil, errors.New("boom")
	if _, err := c.FileIssue(ctx, &FileIssueRequest{ProjectID: "p", Title: "t"}); err == nil {
		t.Fatal("transport error swallowed")
	}
}
