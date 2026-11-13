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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// A skipped adoption lands on the audit trail naming the issue and the pull
// request that closes it.
func TestSDLCAdoptionAuditor_RecordsTheSkipWithItsPullRequest(t *testing.T) {
	logger := &recordingAuditLogger{}
	auditor := newSDLCAdoptionAuditor(func() iam.AuditLogger { return logger })

	auditor.RecordAdoptionSkip(context.Background(), sdlc.AdoptionSkip{
		TenantID: "tenant-a", ProjectID: "project-1", Issue: "acme/api#1048", PullRequest: 1049,
	})

	events := logger.recorded()
	if len(events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(events))
	}
	event := events[0]
	if event.TenantID != "tenant-a" || event.Action != sdlcAdoptionSkippedAction ||
		event.Resource == nil || event.Resource.Type != sdlcAdoptionResourceType || event.Resource.ID != "acme/api#1048" {
		t.Fatalf("event = %+v", event)
	}
	if event.Details["pull_request"] != 1049 || event.Details["project_id"] != "project-1" {
		t.Fatalf("details = %+v", event.Details)
	}
}

// Before IAM publishes a logger there is nowhere to write, and the sweep must
// not fail for it.
func TestSDLCAdoptionAuditor_WithoutALoggerRecordsNothing(t *testing.T) {
	auditor := newSDLCAdoptionAuditor(func() iam.AuditLogger { return nil })
	auditor.RecordAdoptionSkip(context.Background(), sdlc.AdoptionSkip{Issue: "acme/api#1"})
	if newSDLCAdoptionAuditor(nil) != nil {
		t.Fatal("a nil resolver must disable auditing")
	}
}
