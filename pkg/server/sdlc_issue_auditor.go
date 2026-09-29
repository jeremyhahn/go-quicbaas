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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// sdlcIssueFiledAction is the audit action of an operator filing an issue on
// a bound project's forge.
const sdlcIssueFiledAction = "sdlc:issue_create"

// sdlcIssueAuditor puts operator issue filings on the platform audit trail,
// the same one every other administrative act lands on. The logger is
// resolved per event, because the SDLC module is wired before IAM publishes
// it.
type sdlcIssueAuditor struct {
	resolve func() iam.AuditLogger
}

// Compile-time interface satisfaction check.
var _ sdlc.IssueAuditor = (*sdlcIssueAuditor)(nil)

// newSDLCIssueAuditor binds filings to the logger resolve returns. A nil
// resolver yields a nil auditor, which the service treats as unaudited and
// refuses to file through.
func newSDLCIssueAuditor(resolve func() iam.AuditLogger) sdlc.IssueAuditor {
	if resolve == nil {
		return nil
	}
	return &sdlcIssueAuditor{resolve: resolve}
}

// RecordIssueFiled appends one filing, successful or not, to the audit trail.
func (a *sdlcIssueAuditor) RecordIssueFiled(_ context.Context, event sdlc.IssueFiledEvent) {
	logger := a.resolve()
	if logger == nil {
		return
	}
	outcome := iam.AuditOutcomeSuccess
	resourceID := event.Repo.String()
	details := map[string]interface{}{
		"project_id": event.ProjectID,
		"title":      event.Filing.Title,
	}
	if event.Number > 0 {
		resourceID += "#" + strconv.Itoa(event.Number)
		details["issue"] = event.Number
	}
	if event.Filing.Parent > 0 {
		details["parent"] = event.Filing.Parent
	}
	if len(event.Filing.Labels) > 0 {
		details["labels"] = strings.Join(event.Filing.Labels, ",")
	}
	if event.Err != nil {
		outcome = iam.AuditOutcomeFailure
		details["error"] = event.Err.Error()
	}
	var actor *iam.AuditActor
	if event.Actor != "" {
		actor = &iam.AuditActor{Username: event.Actor}
	}
	logger.LogAsync(&iam.AuditEvent{
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategoryAdministration,
		TenantID:  event.TenantID,
		Actor:     actor,
		Action:    sdlcIssueFiledAction,
		Outcome:   outcome,
		Resource:  &iam.AuditResource{Type: sdlcAdoptionResourceType, ID: resourceID},
		Details:   details,
	})
}
