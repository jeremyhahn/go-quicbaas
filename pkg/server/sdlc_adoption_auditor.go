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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// sdlcAdoptionSkippedAction is the audit action of an issue the SDLC issue
// source declined to adopt because a pull request already closes it.
const sdlcAdoptionSkippedAction = "sdlc:adoption_skipped"

// sdlcAdoptionResourceType is the audit resource type of a forge issue.
const sdlcAdoptionResourceType = "forge_issue"

// sdlcAdoptionAuditor puts the issue source's skips on the platform audit
// trail, the same one orchestration store mutations land on.
//
// The logger is resolved per event, for the reason orchestrationStoreAuditor
// gives: the source is wired before IAM initialization publishes the logger.
type sdlcAdoptionAuditor struct {
	resolve func() iam.AuditLogger
}

// Compile-time interface satisfaction check.
var _ sdlc.AdoptionAuditor = (*sdlcAdoptionAuditor)(nil)

// newSDLCAdoptionAuditor binds skips to the logger resolve returns. A nil
// resolver yields a nil auditor, which the source treats as auditing disabled.
func newSDLCAdoptionAuditor(resolve func() iam.AuditLogger) sdlc.AdoptionAuditor {
	if resolve == nil {
		return nil
	}
	return &sdlcAdoptionAuditor{resolve: resolve}
}

// RecordAdoptionSkip appends one skip to the audit trail, asynchronously so a
// sweep never waits on audit I/O. The pull request's number is in the details:
// it is the fact a reader needs to see why the issue was not worked.
func (a *sdlcAdoptionAuditor) RecordAdoptionSkip(_ context.Context, skip sdlc.AdoptionSkip) {
	logger := a.resolve()
	if logger == nil {
		return
	}
	logger.LogAsync(&iam.AuditEvent{
		EventType: iam.AuditEventAppEvent,
		Category:  iam.AuditCategoryAdministration,
		TenantID:  skip.TenantID,
		Action:    sdlcAdoptionSkippedAction,
		Outcome:   iam.AuditOutcomeSuccess,
		Resource:  &iam.AuditResource{Type: sdlcAdoptionResourceType, ID: skip.Issue},
		Details: map[string]interface{}{
			"project_id":   skip.ProjectID,
			"pull_request": skip.PullRequest,
			"reason":       "an open pull request already closes this issue",
		},
	})
}
