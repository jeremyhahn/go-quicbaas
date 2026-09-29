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
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPAccountService reports whether service names the pooled provider
// Account service.
func isMCPAccountService(service string) bool {
	return trimServiceName(service) == "Account"
}

// accountMethodTools maps SDK Account methods to the MCP tool names the server
// registers (pkg/api/mcp/account_tools.go). The tools are named after the
// operation (agent_account_rotate), not the RPC (RotateCredential), so the
// transport's generic service_method derivation would ask for tools that do
// not exist.
var accountMethodTools = map[string]string{
	"Create":           "quicbaas_agent_account_create",
	"Get":              "quicbaas_agent_account_get",
	"List":             "quicbaas_agent_account_list",
	"Update":           "quicbaas_agent_account_update",
	"Delete":           "quicbaas_agent_account_delete",
	"RotateCredential": "quicbaas_agent_account_rotate",
	"Revoke":           "quicbaas_agent_account_revoke",
	"SetDefault":       "quicbaas_agent_account_set_default",
	"Usage":            "quicbaas_agent_account_usage",
	"Materialize":      "quicbaas_agent_account_materialize",
	"ReserveSession":   "quicbaas_agent_account_reserve_session",
	"ReleaseSession":   "quicbaas_agent_account_release_session",
}

// accountMCPHandler converts MCP tool results back into the typed SDK account
// responses. The tools return the pkg/service response envelopes verbatim, so
// the wire shapes below mirror service.*Response.
//
// SECURITY INVARIANT: no envelope here has a credential field, because no
// service response type has one — with EXACTLY ONE deliberate exception,
// Materialize, whose whole purpose is to release one (go-quicbaas#734).
type accountMCPHandler struct{}

// newAccountMCPHandler creates a new account MCP response handler.
func newAccountMCPHandler() *accountMCPHandler {
	return &accountMCPHandler{}
}

// accountMCPTransform decodes one MCP tool result into its SDK type.
type accountMCPTransform func(raw []byte) (interface{}, error)

// accountMCPTransforms gives O(1) dispatch from method to result decoder.
var accountMCPTransforms = map[string]accountMCPTransform{
	"Create":           decodeMCPAccountEnvelope,
	"Get":              decodeMCPAccountEnvelope,
	"Update":           decodeMCPAccountEnvelope,
	"Revoke":           decodeMCPAccountEnvelope,
	"List":             decodeMCPAccountList,
	"Delete":           decodeMCPAccountDelete,
	"RotateCredential": decodeMCPAccountAck,
	"SetDefault":       decodeMCPAccountAck,
	"Usage":            decodeMCPAccountUsage,
	"Materialize":      decodeMCPAccountMaterialize,
	"ReserveSession":   decodeMCPSessionReservation,
	"ReleaseSession":   decodeMCPSessionReservation,
}

// decodeMCPSessionReservation decodes a reserve or release result; both
// service responses carry the reservation's ID.
func decodeMCPSessionReservation(raw []byte) (interface{}, error) {
	var env account.SessionReservation
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode session reservation result")
	}
	return &env, nil
}

// transformResponse converts the raw MCP tool result to the SDK type for
// method.
func (h *accountMCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	transform, ok := accountMCPTransforms[method]
	if !ok {
		return nil, errors.New(errors.CodeUnimplemented, "mcp.account."+method,
			"account method is not supported by the MCP transport")
	}
	if result == nil {
		return nil, errors.NewInternal("mcp.account."+method, "empty tool result")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account."+method,
			"failed to re-encode tool result")
	}
	return transform(raw)
}

func decodeMCPAccountEnvelope(raw []byte) (interface{}, error) {
	var env struct {
		Account *account.AccountInfo `json:"account"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode account result")
	}
	if env.Account == nil {
		return nil, errors.NewInternal("mcp.account", "tool returned no account")
	}
	return env.Account, nil
}

func decodeMCPAccountList(raw []byte) (interface{}, error) {
	var env struct {
		Accounts []*account.AccountInfo `json:"accounts"`
		Total    int                    `json:"total"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode account list result")
	}
	if env.Accounts == nil {
		env.Accounts = []*account.AccountInfo{}
	}
	return &account.ListAccountsResponse{Accounts: env.Accounts, Total: env.Total}, nil
}

func decodeMCPAccountDelete(raw []byte) (interface{}, error) {
	var env struct {
		AccountID string `json:"account_id"`
		Deleted   bool   `json:"deleted"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode account delete result")
	}
	return &account.DeleteAccountResponse{AccountID: env.AccountID, Deleted: env.Deleted}, nil
}

func decodeMCPAccountAck(raw []byte) (interface{}, error) {
	var env struct {
		AccountID string `json:"account_id"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode account acknowledgement result")
	}
	return &account.AccountAckResponse{AccountID: env.AccountID}, nil
}

func decodeMCPAccountUsage(raw []byte) (interface{}, error) {
	var env struct {
		Usage        *account.UsageReport   `json:"usage"`
		LiveSessions int                    `json:"live_sessions"`
		Spend        *account.SpendHeadroom `json:"spend,omitempty"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode account usage result")
	}
	if env.Usage == nil {
		return nil, errors.NewInternal("mcp.account", "tool returned no usage report")
	}
	env.Usage.LiveSessions = env.LiveSessions
	env.Usage.Spend = env.Spend
	return env.Usage, nil
}

// decodeMCPAccountMaterialize decodes a released credential envelope.
//
// The service response it mirrors carries no refresh grant, so neither does
// this. A result with no envelope in it is a protocol fault rather than an
// empty answer: the caller would otherwise spawn an agent with no credential.
func decodeMCPAccountMaterialize(raw []byte) (interface{}, error) {
	var env account.MaterializedCredential
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.account",
			"failed to decode materialized credential result")
	}
	if env.Credential == nil {
		return nil, errors.NewInternal("mcp.account", "tool released no credential envelope")
	}
	return &env, nil
}
