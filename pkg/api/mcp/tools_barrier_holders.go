//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"sort"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Barrier share-holder MCP tools (go-qrdb v0.47).
//
// The holder verbs are pass-throughs: each tool's arguments decode onto the
// go-qrdb transport request and the transport response is the result, the
// same contract as the REST routes in pkg/api/rest/handlers_barrier_holders.go.
// Tool names and argument shapes are go-qrdb's own MCP tools', so a client
// talks to either server the same way.
// =============================================================================

// holderJSONFields are the structured holder arguments. Each travels as a
// JSON-encoded string, go-qrdb's MCP convention for the same tools.
var holderJSONFields = []string{"factors", "response_metadata"}

// HolderArgsError reports tool arguments that do not map onto the holder
// verb's request.
type HolderArgsError struct {
	// Field is the offending argument, or empty when the whole set failed.
	Field string
	// Err is the decoding error.
	Err error
}

// Error implements the error interface.
func (e *HolderArgsError) Error() string {
	if e.Field == "" {
		return "holder tool arguments: " + e.Err.Error()
	}
	return "holder tool argument " + e.Field + ": " + e.Err.Error()
}

// Unwrap returns the decoding error.
func (e *HolderArgsError) Unwrap() error { return e.Err }

// decodeHolderArgs maps tool arguments onto req by their JSON field names.
// []byte fields arrive base64-encoded, encoding/json's own rendering.
func decodeHolderArgs(args map[string]interface{}, req interface{}) error {
	merged := make(map[string]interface{}, len(args))
	for name, value := range args {
		merged[name] = value
	}
	for _, field := range holderJSONFields {
		s, ok := args[field].(string)
		if !ok {
			continue
		}
		if s == "" {
			delete(merged, field)
			continue
		}
		var parsed json.RawMessage
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return holderArgsError(field, err)
		}
		merged[field] = parsed
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return holderArgsError("", err)
	}
	if err := json.Unmarshal(raw, req); err != nil {
		return holderArgsError("", err)
	}
	return nil
}

// holderArgsError wraps a decoding failure as an invalid-input QBError so the
// MCP error mapper classifies it as the caller's own fault.
func holderArgsError(field string, err error) error {
	return &qberrors.QBError{
		Code: qberrors.ErrInvalidInput,
		Op:   "mcp.barrier_holder",
		Err:  &HolderArgsError{Field: field, Err: err},
	}
}

// holderVerb decodes args onto a Req and passes it to call.
func holderVerb[Req any, Resp any](ctx context.Context, args map[string]interface{}, call func(context.Context, *Req) (Resp, error)) (interface{}, error) {
	var req Req
	if err := decodeHolderArgs(args, &req); err != nil {
		return nil, err
	}
	resp, err := call(ctx, &req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// HolderClaimCertRequiredError reports a holder share-claim tool call whose
// MCP connection carries no verified mTLS client certificate. go-qrdb binds a
// claim to the caller's certificate, so a call without one is refused rather
// than forwarded unbound.
type HolderClaimCertRequiredError struct{}

// Error implements the error interface.
func (e *HolderClaimCertRequiredError) Error() string {
	return "share claim requires a verified mTLS client certificate"
}

// ErrorCode classifies the refusal as an authentication failure.
func (e *HolderClaimCertRequiredError) ErrorCode() qberrors.ErrorCode {
	return qberrors.ErrUnauthorized
}

// holderClaim runs a share-claim verb only for a caller whose connection
// presented a verified client certificate. go-qrdb's MCP listener installs
// the connection's TLS state on ctx; the embedded SDK reads a context without
// it as an in-process claim and substitutes the holder's enrolled
// certificate, so an unverified call must never reach the service.
func holderClaim[Req any, Resp any](ctx context.Context, args map[string]interface{}, call func(context.Context, *Req) (Resp, error)) (interface{}, error) {
	state := qrdbsdk.TLSStateFromContext(ctx)
	if state == nil || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return nil, &HolderClaimCertRequiredError{}
	}
	return holderVerb(ctx, args, call)
}

// holderOfficers is the result of the officer-list tools, the REST body's
// shape.
type holderOfficers struct {
	Officers []*qrdbtransport.OfficerStatus `json:"officers"`
}

// holderArg declares one tool argument.
type holderArg struct {
	name        string
	kind        string
	description string
	required    bool
}

// holderSchema builds a tool's input schema from its arguments.
func holderSchema(args []holderArg) map[string]interface{} {
	properties := make(map[string]interface{}, len(args))
	required := []string{}
	for _, a := range args {
		properties[a.name] = map[string]interface{}{"type": a.kind, "description": a.description}
		if a.required {
			required = append(required, a.name)
		}
	}
	schema := map[string]interface{}{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// Argument declarations shared by the holder tools.
var (
	argHolderTenantID = holderArg{"tenant_id", "string", "The tenant whose barrier the call is for.", true}
	argHolderUsername = holderArg{"username", "string", "The share holder's username.", true}
	argHolderDomain   = holderArg{"domain", "string", "The key the round is over: unseal or recovery.", true}
	argHolderOp       = holderArg{"op", "string", "The operation: unseal, seal, root-token, rekey or recover.", true}
	argHolderPolicy   = holderArg{"policy", "string", "Factor policy of the new share set: designated (default) or any-enrolled.", false}
	argHolderSOPin    = holderArg{"so_pin", "string", "SO PIN verified against the node's PIN backend.", false}

	holderRegisterArgs = []holderArg{
		argHolderUsername,
		{"role", "string", "Holder role: so (default) or custodian.", false},
		{"csr_pem", "string", "PEM certificate signing request. Only its public key is used: the node's CA assigns the certificate identity.", true},
		{"encryption_kem", "string", "HPKE suite of a separate share-envelope key: dhkem-p256, dhkem-p384, dhkem-x25519 or mlkem768-x25519.", false},
		{"encryption_key", "string", "Base64 of the separate share-envelope public key in that suite's encoding.", false},
		{"factors", "string", `JSON array of additional factor enrolments: [{"name":...,"params":{...}}].`, false},
		{"designated", "string", "The factor fixed at enrolment under the designated policy; mtls when empty.", false},
	}
	holderClaimCompleteArgs = []holderArg{
		argHolderUsername,
		{"nonce_hex", "string", "Nonce hex from the claim-begin response.", true},
		{"signature", "string", "Base64 signature over the nonce by the holder's certificate key.", true},
	}
	holderQuorumBeginArgs = []holderArg{
		argHolderOp,
		argHolderUsername,
		{"factor", "string", "The factor to answer with; the designated factor when empty.", false},
	}
	holderQuorumSubmitArgs = []holderArg{
		argHolderOp,
		argHolderUsername,
		{"challenge_id", "string", "The challenge id from quorum-begin.", true},
		{"factor", "string", "The factor that answered the challenge.", true},
		{"response", "string", "Base64 factor response to the challenge.", true},
		{"response_metadata", "string", "JSON object of factor response metadata.", false},
		{"share", "string", "Base64 share recovered from the holder's envelope.", true},
	}
	holderThresholdArg = holderArg{"threshold", "number", "Threshold of the new share set.", true}
	holderRevokeArgs   = []holderArg{
		argHolderUsername,
		{"threshold", "number", "Threshold of the share set left after the revocation; unchanged when 0.", false},
	}
)

// withArgs concatenates argument groups.
func withArgs(groups ...[]holderArg) []holderArg {
	var out []holderArg
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// holderTool is one share-holder tool over a service S.
type holderTool[S any] struct {
	description string
	args        []holderArg
	exec        func(context.Context, S, map[string]interface{}) (interface{}, error)
}

// barrierHolderTools are the system barrier's share-holder tools.
var barrierHolderTools = map[string]holderTool[service.BarrierServicer]{
	"qrdb_barrier_cluster_unseal": {
		description: "Report the cluster-wide unseal state of the system barrier.",
		exec: func(ctx context.Context, svc service.BarrierServicer, _ map[string]interface{}) (interface{}, error) {
			return svc.BarrierClusterUnseal(ctx)
		},
	},
	"qrdb_barrier_officer_list": {
		description: "List the unseal key's share holders and their claim state.",
		exec: func(ctx context.Context, svc service.BarrierServicer, _ map[string]interface{}) (interface{}, error) {
			officers, err := svc.OfficerList(ctx)
			if err != nil {
				return nil, err
			}
			return holderOfficers{Officers: officers}, nil
		},
	},
	"qrdb_barrier_officer_register": {
		description: "Enrol a share holder (Security Officer or key custodian) of the unseal key.",
		args:        holderRegisterArgs,
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.OfficerRegister)
		},
	},
	"qrdb_barrier_recovery_officer_register": {
		description: "Enrol a holder of the recovery key.",
		args:        holderRegisterArgs,
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.RecoveryOfficerRegister)
		},
	},
	"qrdb_barrier_officer_revoke": {
		description: "Revoke a holder through a quorum-gated rekey that drops it atomically.",
		args:        withArgs([]holderArg{argHolderDomain}, holderRevokeArgs, []holderArg{argHolderSOPin}),
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.BarrierOfficerRevoke)
		},
	},
	"qrdb_barrier_share_claim_begin": {
		description: "Issue a claim nonce to an unseal-key holder. Requires the holder's mTLS client certificate.",
		args:        []holderArg{argHolderUsername},
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderClaim(ctx, args, svc.ShareClaimBegin)
		},
	},
	"qrdb_barrier_share_claim_complete": {
		description: "Verify the holder's nonce signature and deliver its sealed share envelope, once. Requires the holder's mTLS client certificate.",
		args:        holderClaimCompleteArgs,
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderClaim(ctx, args, svc.ShareClaimComplete)
		},
	},
	"qrdb_barrier_recovery_share_claim_begin": {
		description: "Issue a claim nonce to a recovery-key holder. Requires the holder's mTLS client certificate.",
		args:        []holderArg{argHolderUsername},
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderClaim(ctx, args, svc.RecoveryShareClaimBegin)
		},
	},
	"qrdb_barrier_recovery_share_claim_complete": {
		description: "Deliver a recovery-key holder's sealed share envelope, once. Requires the holder's mTLS client certificate.",
		args:        holderClaimCompleteArgs,
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderClaim(ctx, args, svc.RecoveryShareClaimComplete)
		},
	},
	"qrdb_barrier_quorum_begin": {
		description: "Issue the factor challenge a holder answers to submit a share. Reachable while sealed.",
		args:        withArgs([]holderArg{argHolderDomain}, holderQuorumBeginArgs),
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.BarrierQuorumBegin)
		},
	},
	"qrdb_barrier_quorum_submit": {
		description: "Submit one authenticated holder's share toward the open round. Each holder counts once. Reachable while sealed.",
		args:        withArgs([]holderArg{argHolderDomain}, holderQuorumSubmitArgs),
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, func(ctx context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
				defer secure.Wipe(req.Share)
				defer secure.Wipe(req.Response)
				return svc.BarrierQuorumSubmit(ctx, req)
			})
		},
	},
	"qrdb_barrier_rekey_start": {
		description: "Open a rekey round; it completes when a fresh M-of-N holder quorum submits to the rekey op.",
		args:        []holderArg{argHolderDomain, holderThresholdArg, argHolderPolicy, argHolderSOPin},
		exec: func(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.BarrierRekeyStart)
		},
	},
}

// tenantBarrierHolderTools are a tenant barrier's share-holder tools.
var tenantBarrierHolderTools = map[string]holderTool[service.TenantBarrierServicer]{
	"qrdb_tenant_barrier_officer_list": {
		description: "List a tenant barrier's share holders and their claim state.",
		args:        []holderArg{argHolderTenantID},
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			tenantID := getString(args, "tenant_id")
			if tenantID == "" {
				return nil, &TenantBarrierTenantIDRequiredError{}
			}
			officers, err := svc.TenantBarrierOfficerList(ctx, tenantID)
			if err != nil {
				return nil, err
			}
			return holderOfficers{Officers: officers}, nil
		},
	},
	"qrdb_tenant_barrier_officer_register": {
		description: "Enrol a share holder of a tenant barrier.",
		args:        withArgs([]holderArg{argHolderTenantID}, holderRegisterArgs),
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.TenantBarrierOfficerRegister)
		},
	},
	"qrdb_tenant_barrier_officer_revoke": {
		description: "Revoke a tenant holder through a quorum-gated rekey that drops it atomically.",
		args:        withArgs([]holderArg{argHolderTenantID}, holderRevokeArgs),
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.TenantBarrierOfficerRevoke)
		},
	},
	"qrdb_tenant_barrier_share_claim_begin": {
		description: "Issue a claim nonce to a tenant holder. Requires the holder's mTLS client certificate.",
		args:        []holderArg{argHolderTenantID, argHolderUsername},
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderClaim(ctx, args, svc.TenantBarrierShareClaimBegin)
		},
	},
	"qrdb_tenant_barrier_share_claim_complete": {
		description: "Deliver a tenant holder's sealed share envelope, once. Requires the holder's mTLS client certificate.",
		args:        withArgs([]holderArg{argHolderTenantID}, holderClaimCompleteArgs),
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderClaim(ctx, args, svc.TenantBarrierShareClaimComplete)
		},
	},
	"qrdb_tenant_barrier_quorum_begin": {
		description: "Issue the factor challenge a tenant holder answers to submit a share.",
		args:        withArgs([]holderArg{argHolderTenantID}, holderQuorumBeginArgs),
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.TenantBarrierQuorumBegin)
		},
	},
	"qrdb_tenant_barrier_quorum_submit": {
		description: "Submit one authenticated tenant holder's share toward the open round.",
		args:        withArgs([]holderArg{argHolderTenantID}, holderQuorumSubmitArgs),
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, func(ctx context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
				defer secure.Wipe(req.Share)
				defer secure.Wipe(req.Response)
				return svc.TenantBarrierQuorumSubmit(ctx, req)
			})
		},
	},
	"qrdb_tenant_barrier_rekey_start": {
		description: "Open a rekey round on a tenant barrier.",
		args:        []holderArg{argHolderTenantID, holderThresholdArg, argHolderPolicy},
		exec: func(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
			return holderVerb(ctx, args, svc.TenantBarrierRekeyStart)
		},
	},
}

// init routes the holder tools through their surfaces' dispatch tables.
func init() {
	for name, t := range barrierHolderTools {
		barrierToolNames[name] = struct{}{}
		barrierExecutors[name] = t.exec
	}
	for name, t := range tenantBarrierHolderTools {
		tenantBarrierToolNames[name] = struct{}{}
		tenantBarrierExecutors[name] = t.exec
	}
}

// registerHolderTools adds every tool of tools to the registry, in name
// order so tools/list is stable.
func registerHolderTools[S any](r *ToolRegistry, tools map[string]holderTool[S]) {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := tools[name]
		r.addTool(ToolDefinition{Name: name, Description: t.description, InputSchema: holderSchema(t.args)})
	}
}
