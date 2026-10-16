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
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const promptsServiceName = "quicbaas.Prompts"

// SupportsPrompts declares the Prompts service on the embedded transport.
func (t *Transport) SupportsPrompts() bool { return true }

var _ prompt.PromptsCapableTransport = (*Transport)(nil)

// promptFaultCodes is the SDK code of each prompt refusal, the classes every
// wire transport answers.
var promptFaultCodes = map[service.PromptFault]errors.ErrorCode{
	service.PromptFaultInvalid:     errors.CodeInvalidInput,
	service.PromptFaultNotFound:    errors.CodeNotFound,
	service.PromptFaultConflict:    errors.CodeConflict,
	service.PromptFaultUnavailable: errors.CodeUnavailable,
}

// promptError codes a prompt service error; one the service does not
// classify is a fault of the server.
func promptError(method string, err error) error {
	code := errors.CodeInternal
	if fault, ok := service.ClassifyPromptError(err); ok {
		code = promptFaultCodes[fault]
	}
	return errors.Wrap(err, code, "embedded."+promptsServiceName+"."+method, "prompt request failed")
}

// promptServiceOf resolves the mounted prompt service or answers Unavailable.
func promptServiceOf(svc service.QuicBaaS, method string) (*service.PromptService, error) {
	if svc == nil {
		return nil, promptError(method, service.ErrPromptsUnavailable)
	}
	prompts := svc.Prompts()
	if prompts == nil {
		return nil, promptError(method, service.ErrPromptsUnavailable)
	}
	return prompts, nil
}

// promptRequestOf asserts the SDK request type of a verb.
func promptRequestOf[T any](method string, req interface{}) (*T, error) {
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(promptsServiceName, method, fmt.Sprintf("%T", (*T)(nil)), fmt.Sprintf("%T", req))
	}
	return r, nil
}

// promptAddress maps an SDK address to the service's, in the tenant
// promptTenant resolves.
func promptAddress(ctx context.Context, method string, addr *prompt.Address) (service.PromptAddress, error) {
	tenantID, err := promptTenant(ctx, method, addr.TenantID)
	if err != nil {
		return service.PromptAddress{}, err
	}
	return service.PromptAddress{
		TenantID: tenantID, Scope: addr.Scope, ProjectID: addr.ProjectID, Set: addr.Set, Name: addr.Name,
	}, nil
}

// promptTenant resolves the tenant a prompt call acts in. An omitted tenant
// is the one the call is bound to (inProcessTenant), as on the wire
// transports; a named tenant is passed on unchanged.
func promptTenant(ctx context.Context, method, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	return tenantctx.FromContext(ctx, "embedded."+promptsServiceName+"."+method)
}

func handlePromptList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := promptRequestOf[prompt.ListRequest]("List", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "List")
	if err != nil {
		return nil, err
	}
	tenantID, err := promptTenant(ctx, "List", r.TenantID)
	if err != nil {
		return nil, err
	}
	list, err := promptSvc.List(ctx, &service.ListPromptsRequest{
		TenantID: tenantID, Scope: r.Scope, ProjectID: r.ProjectID, Set: r.Set,
	})
	if err != nil {
		return nil, promptError("List", err)
	}
	out := &prompt.ListResponse{Set: string(list.Set), Scope: string(list.Scope), Prompts: make([]prompt.ListEntry, 0, len(list.Prompts))}
	for _, entry := range list.Prompts {
		out.Prompts = append(out.Prompts, prompt.ListEntry{
			Name: entry.Name, Description: entry.Description, Overridden: entry.Overridden,
			Version: entry.Version, FollowParent: entry.FollowParent, Effective: promptRefFromDomain(entry.Effective),
		})
	}
	return out, nil
}

func handlePromptGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := promptRequestOf[prompt.GetRequest]("Get", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "Get")
	if err != nil {
		return nil, err
	}
	addr, err := promptAddress(ctx, "Get", &r.Address)
	if err != nil {
		return nil, err
	}
	view, err := promptSvc.Get(ctx, &service.GetPromptRequest{PromptAddress: addr, Version: r.Version})
	if err != nil {
		return nil, promptError("Get", err)
	}
	return promptRecordFromDomain(view), nil
}

func handlePromptEffective(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	addr, err := promptRequestOf[prompt.Address]("Effective", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "Effective")
	if err != nil {
		return nil, err
	}
	domainAddr, err := promptAddress(ctx, "Effective", addr)
	if err != nil {
		return nil, err
	}
	effective, err := promptSvc.Effective(ctx, &domainAddr)
	if err != nil {
		return nil, promptError("Effective", err)
	}
	out := &prompt.EffectivePrompt{
		Set: string(effective.Set), Name: effective.Name, Scope: string(effective.Scope), Content: effective.Content,
		Origin: promptRefFromDomain(effective.Origin), Chain: make([]prompt.Layer, 0, len(effective.Chain)),
	}
	for _, layer := range effective.Chain {
		out.Chain = append(out.Chain, prompt.Layer{
			Scope: string(layer.Scope), ScopeID: layer.ScopeID, Present: layer.Present, Version: layer.Version,
			Hash: layer.Hash, FollowParent: layer.FollowParent, Answered: layer.Answered,
		})
	}
	return out, nil
}

func handlePromptDiff(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	addr, err := promptRequestOf[prompt.Address]("Diff", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "Diff")
	if err != nil {
		return nil, err
	}
	domainAddr, err := promptAddress(ctx, "Diff", addr)
	if err != nil {
		return nil, err
	}
	diff, err := promptSvc.Diff(ctx, &domainAddr)
	if err != nil {
		return nil, promptError("Diff", err)
	}
	out := &prompt.Diff{
		Set: string(diff.Set), Name: diff.Name, Scope: string(diff.Scope), Same: diff.Same,
		Origin: promptRefFromDomain(diff.Origin), ParentOrigin: promptRefFromDomain(diff.ParentOrigin),
		Lines: make([]prompt.DiffLine, 0, len(diff.Lines)),
	}
	for _, line := range diff.Lines {
		out.Lines = append(out.Lines, prompt.DiffLine{Op: string(line.Op), Text: line.Text})
	}
	return out, nil
}

func handlePromptHistory(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	addr, err := promptRequestOf[prompt.Address]("History", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "History")
	if err != nil {
		return nil, err
	}
	domainAddr, err := promptAddress(ctx, "History", addr)
	if err != nil {
		return nil, err
	}
	history, err := promptSvc.History(ctx, &domainAddr)
	if err != nil {
		return nil, promptError("History", err)
	}
	out := &prompt.History{
		Set: string(history.Set), Name: history.Name, Scope: string(history.Scope),
		Revisions: make([]prompt.RevisionSummary, 0, len(history.Revisions)),
	}
	for _, rev := range history.Revisions {
		out.Revisions = append(out.Revisions, prompt.RevisionSummary{
			Version: rev.Version, ContentHash: rev.ContentHash, Author: rev.Author, Note: rev.Note,
			FollowParent: rev.FollowParent, CreatedAt: rev.CreatedAt,
		})
	}
	return out, nil
}

func handlePromptPut(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := promptRequestOf[prompt.PutRequest]("Put", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "Put")
	if err != nil {
		return nil, err
	}
	addr, err := promptAddress(ctx, "Put", &r.Address)
	if err != nil {
		return nil, err
	}
	view, err := promptSvc.Put(ctx, &service.PutPromptRequest{
		PromptAddress: addr, Content: r.Content, Note: r.Note, ExpectedVersion: r.ExpectedVersion,
	})
	if err != nil {
		return nil, promptError("Put", err)
	}
	return promptRecordFromDomain(view), nil
}

func handlePromptRollback(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := promptRequestOf[prompt.RollbackRequest]("Rollback", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "Rollback")
	if err != nil {
		return nil, err
	}
	addr, err := promptAddress(ctx, "Rollback", &r.Address)
	if err != nil {
		return nil, err
	}
	view, err := promptSvc.Rollback(ctx, &service.RollbackPromptRequest{
		PromptAddress: addr, ToVersion: r.ToVersion, Note: r.Note, ExpectedVersion: r.ExpectedVersion,
	})
	if err != nil {
		return nil, promptError("Rollback", err)
	}
	return promptRecordFromDomain(view), nil
}

func handlePromptReset(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, err := promptRequestOf[prompt.ResetRequest]("Reset", req)
	if err != nil {
		return nil, err
	}
	promptSvc, err := promptServiceOf(svc, "Reset")
	if err != nil {
		return nil, err
	}
	addr, err := promptAddress(ctx, "Reset", &r.Address)
	if err != nil {
		return nil, err
	}
	view, err := promptSvc.Reset(ctx, &service.ResetPromptRequest{
		PromptAddress: addr, Note: r.Note, ExpectedVersion: r.ExpectedVersion,
	})
	if err != nil {
		return nil, promptError("Reset", err)
	}
	return promptRecordFromDomain(view), nil
}

func promptRecordFromDomain(v *service.PromptView) *prompt.Record {
	return &prompt.Record{
		TenantID: v.TenantID, ProjectID: v.ProjectID, Scope: string(v.Scope), Set: string(v.Set), Name: v.Name,
		Version: v.Version, ContentHash: v.ContentHash, Content: v.Content, FollowParent: v.FollowParent, UpdatedAt: v.UpdatedAt,
	}
}

func promptRefFromDomain(r prompts.VersionRef) prompt.VersionRef {
	return prompt.VersionRef{
		Set: string(r.Set), Name: r.Name, Scope: string(r.Scope), ScopeID: r.ScopeID, Version: r.Version, Hash: r.Hash,
	}
}
