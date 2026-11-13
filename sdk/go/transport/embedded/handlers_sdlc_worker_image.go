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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// handleSDLCWorkerImageStatus reads the worker image refresh's state. A host
// verb: the request carries nothing and no tenant is derived.
func handleSDLCWorkerImageStatus(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	sdlcSvc, err := sdlcServiceOf(svc, "WorkerImageStatus")
	if err != nil {
		return nil, err
	}
	status, err := sdlcSvc.WorkerImageStatus(ctx)
	if err != nil {
		return nil, err
	}
	return sdlcWorkerImageStatusFromDomain(status), nil
}

// handleSDLCRefreshWorkerImage queues a refresh pass.
func handleSDLCRefreshWorkerImage(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	sdlcSvc, err := sdlcServiceOf(svc, "RefreshWorkerImage")
	if err != nil {
		return nil, err
	}
	status, err := sdlcSvc.RefreshWorkerImage(ctx)
	if err != nil {
		return nil, err
	}
	return sdlcWorkerImageStatusFromDomain(status), nil
}

func sdlcWorkerImageStatusFromDomain(st workerimage.Status) *sdlc.WorkerImageStatus {
	out := &sdlc.WorkerImageStatus{Serving: st.Serving, Running: st.Running, RunningSince: st.RunningSince, Queued: st.Queued}
	if st.Last != nil {
		out.Last = sdlcWorkerImageReportFromDomain(st.Last)
	}
	return out
}

func sdlcWorkerImageReportFromDomain(r *workerimage.Report) *sdlc.WorkerImageReport {
	out := &sdlc.WorkerImageReport{
		Trigger:    string(r.Trigger),
		Outcome:    string(r.Outcome),
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		Latest:     sdlc.WorkerImageAgents{ClaudeCode: r.Latest.ClaudeCode, Codex: r.Latest.Codex},
		Stale:      append([]string(nil), r.Stale...),
		Skipped:    append([]string(nil), r.Skipped...),
		Candidate:  r.Candidate,
		ImageID:    r.ImageID,
		Reference:  r.Reference,
		Digest:     r.Digest,
		Error:      r.Error,
	}
	for _, c := range r.Probes {
		out.Probes = append(out.Probes, sdlc.WorkerImageCheck{Name: c.Name, Passed: c.Passed, Detail: c.Detail})
	}
	for _, c := range r.Smokes {
		out.Smokes = append(out.Smokes, sdlc.WorkerImageCheck{Name: c.Name, Passed: c.Passed, Detail: c.Detail})
	}
	for _, b := range r.Bound {
		out.Bound = append(out.Bound, sdlc.WorkerImageBound{
			Image: b.Image, Agents: sdlc.WorkerImageAgents{ClaudeCode: b.Agents.ClaudeCode, Codex: b.Agents.Codex},
			Uses: b.Uses, Stale: b.Stale,
		})
	}
	for _, p := range r.Promotions {
		out.Promotions = append(out.Promotions, sdlc.WorkerImagePromotion{
			TenantID: p.TenantID, AgentID: p.AgentID, From: append([]string(nil), p.From...), To: p.To, Error: p.Error,
		})
	}
	return out
}
