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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

func TestSDLCIssueAuditor_RecordsAFilingWithItsActorAndIssue(t *testing.T) {
	logger := &recordingAuditLogger{}
	auditor := newSDLCIssueAuditor(func() iam.AuditLogger { return logger })

	auditor.RecordIssueFiled(context.Background(), sdlc.IssueFiledEvent{
		TenantID: "tenant-a", ProjectID: "project-1", Actor: "lead",
		Repo:   sdlc.RepoRef{Owner: "acme", Repo: "api"},
		Filing: sdlc.IssueFiling{Title: "Follow-up", Labels: []string{"task:ready"}, Parent: 12},
		Number: 88,
	})

	events := logger.recorded()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	event := events[0]
	if event.Action != sdlcIssueFiledAction || event.Outcome != iam.AuditOutcomeSuccess || event.TenantID != "tenant-a" ||
		event.Actor == nil || event.Actor.Username != "lead" || event.Resource.ID != "acme/api#88" {
		t.Fatalf("event = %+v", event)
	}
	if event.Details["parent"] != 12 || event.Details["labels"] != "task:ready" || event.Details["issue"] != 88 {
		t.Fatalf("details = %+v", event.Details)
	}
}

func TestSDLCIssueAuditor_RecordsAFailureAndToleratesNoLogger(t *testing.T) {
	logger := &recordingAuditLogger{}
	auditor := newSDLCIssueAuditor(func() iam.AuditLogger { return logger })
	auditor.RecordIssueFiled(context.Background(), sdlc.IssueFiledEvent{
		TenantID: "tenant-a", Repo: sdlc.RepoRef{Owner: "acme", Repo: "api"},
		Filing: sdlc.IssueFiling{Title: "t"}, Err: errors.New("forge down"),
	})
	events := logger.recorded()
	if len(events) != 1 || events[0].Outcome != iam.AuditOutcomeFailure || events[0].Details["error"] != "forge down" ||
		events[0].Actor != nil || events[0].Resource.ID != "acme/api" {
		t.Fatalf("events = %+v, want one failure with no issue number", events)
	}

	newSDLCIssueAuditor(func() iam.AuditLogger { return nil }).RecordIssueFiled(context.Background(), sdlc.IssueFiledEvent{})
	if newSDLCIssueAuditor(nil) != nil {
		t.Fatal("a nil resolver must yield no auditor")
	}
}
