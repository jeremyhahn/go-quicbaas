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

package grpc

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsPrompts declares the Prompts service on this transport, and so on
// the Unix transport that delegates to it.
func (t *Transport) SupportsPrompts() bool { return true }

var _ prompt.PromptsCapableTransport = (*Transport)(nil)

// isPromptsService reports whether the service name is the Prompts service.
func isPromptsService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Prompts"
}

type promptHandlerFunc func(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error)

// promptMethodMap dispatches Prompts methods onto quicbaas.prompt.v1.PromptService.
var promptMethodMap = map[string]promptHandlerFunc{
	"List":      handlePromptList,
	"Get":       handlePromptGet,
	"Effective": handlePromptEffective,
	"Diff":      handlePromptDiff,
	"History":   handlePromptHistory,
	"Put":       handlePromptPut,
	"Rollback":  handlePromptRollback,
	"Reset":     handlePromptReset,
}

// handlePromptRequest routes one Prompts method over conn.
func handlePromptRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := promptMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.prompt."+method, "unknown prompt method")
	}
	return handler(ctx, pb.NewPromptServiceClient(conn), req)
}

// promptRequest asserts the SDK request type of a verb.
func promptRequest[T any](op string, req interface{}) (*T, error) {
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	return r, nil
}

func promptAddressToProto(addr *prompt.Address) *pb.PromptAddress {
	return &pb.PromptAddress{
		TenantId: addr.TenantID, Scope: addr.Scope, ProjectId: addr.ProjectID, Set: addr.Set, Name: addr.Name,
	}
}

// promptTime converts a wire Unix time; zero stays the zero time.
func promptTime(unix int64) time.Time {
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0).UTC()
}

func promptRecordFromProto(r *pb.PromptRecord) *prompt.Record {
	return &prompt.Record{
		TenantID: r.GetTenantId(), ProjectID: r.GetProjectId(), Scope: r.GetScope(), Set: r.GetSet(), Name: r.GetName(),
		Version: r.GetVersion(), ContentHash: r.GetContentHash(), Content: r.GetContent(),
		FollowParent: r.GetFollowParent(), UpdatedAt: promptTime(r.GetUpdatedAtUnix()),
	}
}

func promptRefFromProto(r *pb.PromptVersionRef) prompt.VersionRef {
	return prompt.VersionRef{
		Set: r.GetSet(), Name: r.GetName(), Scope: r.GetScope(), ScopeID: r.GetScopeId(),
		Version: r.GetVersion(), Hash: r.GetHash(),
	}
}

func handlePromptList(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.List"
	r, err := promptRequest[prompt.ListRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.ListPrompts(ctx, &pb.ListPromptsRequest{
		TenantId: r.TenantID, Scope: r.Scope, ProjectId: r.ProjectID, Set: r.Set,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &prompt.ListResponse{Set: resp.GetSet(), Scope: resp.GetScope(), Prompts: make([]prompt.ListEntry, 0, len(resp.GetPrompts()))}
	for _, entry := range resp.GetPrompts() {
		out.Prompts = append(out.Prompts, prompt.ListEntry{
			Name: entry.GetName(), Description: entry.GetDescription(), Overridden: entry.GetOverridden(),
			Version: entry.GetVersion(), FollowParent: entry.GetFollowParent(), Effective: promptRefFromProto(entry.GetEffective()),
		})
	}
	return out, nil
}

func handlePromptGet(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.Get"
	r, err := promptRequest[prompt.GetRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetPrompt(ctx, &pb.GetPromptRequest{Address: promptAddressToProto(&r.Address), Version: r.Version})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return promptRecordFromProto(resp), nil
}

func handlePromptEffective(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.Effective"
	addr, err := promptRequest[prompt.Address](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetEffectivePrompt(ctx, promptAddressToProto(addr))
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &prompt.EffectivePrompt{
		Set: resp.GetSet(), Name: resp.GetName(), Scope: resp.GetScope(), Content: resp.GetContent(),
		Origin: promptRefFromProto(resp.GetOrigin()), Chain: make([]prompt.Layer, 0, len(resp.GetChain())),
	}
	for _, layer := range resp.GetChain() {
		out.Chain = append(out.Chain, prompt.Layer{
			Scope: layer.GetScope(), ScopeID: layer.GetScopeId(), Present: layer.GetPresent(), Version: layer.GetVersion(),
			Hash: layer.GetHash(), FollowParent: layer.GetFollowParent(), Answered: layer.GetAnswered(),
		})
	}
	return out, nil
}

func handlePromptDiff(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.Diff"
	addr, err := promptRequest[prompt.Address](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.DiffPrompt(ctx, promptAddressToProto(addr))
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &prompt.Diff{
		Set: resp.GetSet(), Name: resp.GetName(), Scope: resp.GetScope(), Same: resp.GetSame(),
		Origin: promptRefFromProto(resp.GetOrigin()), ParentOrigin: promptRefFromProto(resp.GetParentOrigin()),
		Lines: make([]prompt.DiffLine, 0, len(resp.GetLines())),
	}
	for _, line := range resp.GetLines() {
		out.Lines = append(out.Lines, prompt.DiffLine{Op: line.GetOp(), Text: line.GetText()})
	}
	return out, nil
}

func handlePromptHistory(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.History"
	addr, err := promptRequest[prompt.Address](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetPromptHistory(ctx, promptAddressToProto(addr))
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &prompt.History{
		Set: resp.GetSet(), Name: resp.GetName(), Scope: resp.GetScope(),
		Revisions: make([]prompt.RevisionSummary, 0, len(resp.GetRevisions())),
	}
	for _, rev := range resp.GetRevisions() {
		out.Revisions = append(out.Revisions, prompt.RevisionSummary{
			Version: rev.GetVersion(), ContentHash: rev.GetContentHash(), Author: rev.GetAuthor(), Note: rev.GetNote(),
			FollowParent: rev.GetFollowParent(), CreatedAt: promptTime(rev.GetCreatedAtUnix()),
		})
	}
	return out, nil
}

func handlePromptPut(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.Put"
	r, err := promptRequest[prompt.PutRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.PutPrompt(ctx, &pb.PutPromptRequest{
		Address: promptAddressToProto(&r.Address), Content: r.Content, Note: r.Note, ExpectedVersion: r.ExpectedVersion,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return promptRecordFromProto(resp), nil
}

func handlePromptRollback(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.Rollback"
	r, err := promptRequest[prompt.RollbackRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.RollbackPrompt(ctx, &pb.RollbackPromptRequest{
		Address: promptAddressToProto(&r.Address), ToVersion: r.ToVersion, Note: r.Note, ExpectedVersion: r.ExpectedVersion,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return promptRecordFromProto(resp), nil
}

func handlePromptReset(ctx context.Context, client pb.PromptServiceClient, req interface{}) (interface{}, error) {
	const op = "grpc.prompt.Reset"
	r, err := promptRequest[prompt.ResetRequest](op, req)
	if err != nil {
		return nil, err
	}
	resp, err := client.ResetPrompt(ctx, &pb.ResetPromptRequest{
		Address: promptAddressToProto(&r.Address), Note: r.Note, ExpectedVersion: r.ExpectedVersion,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return promptRecordFromProto(resp), nil
}
