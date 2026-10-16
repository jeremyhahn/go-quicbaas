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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsPrompts declares the Prompts service on the MCP transport.
func (t *Transport) SupportsPrompts() bool { return true }

var _ prompt.PromptsCapableTransport = (*Transport)(nil)

// isMCPPromptsService reports whether the service name is the Prompts service.
func isMCPPromptsService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Prompts"
}

// promptVerb binds one SDK method to the prompt_* tool that serves it. The
// SDK request's JSON names are the tool's argument names, so args encodes
// the request as-is; decode reads the result into the SDK answer type.
type promptVerb struct {
	tool   string
	args   func(op string, req interface{}) (map[string]interface{}, error)
	decode func(op string, raw []byte) (interface{}, error)
}

// newPromptVerb builds the verb for a request type Req answered by Resp.
func newPromptVerb[Req, Resp any](tool string) promptVerb {
	return promptVerb{tool: tool, args: promptArgs[Req], decode: decodePromptResult[Resp]}
}

// promptVerbs is the closed Prompts tool set.
var promptVerbs = map[string]promptVerb{
	"List":      newPromptVerb[prompt.ListRequest, prompt.ListResponse]("prompt_list"),
	"Get":       newPromptVerb[prompt.GetRequest, prompt.Record]("prompt_get"),
	"Effective": newPromptVerb[prompt.Address, prompt.EffectivePrompt]("prompt_effective"),
	"Diff":      newPromptVerb[prompt.Address, prompt.Diff]("prompt_diff"),
	"History":   newPromptVerb[prompt.Address, prompt.History]("prompt_history"),
	"Put":       newPromptVerb[prompt.PutRequest, prompt.Record]("prompt_put"),
	"Rollback":  newPromptVerb[prompt.RollbackRequest, prompt.Record]("prompt_rollback"),
	"Reset":     newPromptVerb[prompt.ResetRequest, prompt.Record]("prompt_reset"),
}

// promptArgs asserts the request type and encodes it as tool arguments.
func promptArgs[Req any](op string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*Req)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to encode the tool arguments")
	}
	var arguments map[string]interface{}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to encode the tool arguments")
	}
	return arguments, nil
}

// decodePromptResult reads a re-encoded tool result into Resp.
func decodePromptResult[Resp any](op string, raw []byte) (interface{}, error) {
	var out Resp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode the tool result")
	}
	return &out, nil
}

// requestPrompt calls the tool behind a Prompts method and decodes its answer.
func (t *Transport) requestPrompt(ctx context.Context, method string, req interface{}) (interface{}, error) {
	op := "mcp.prompt." + method
	verb, ok := promptVerbs[method]
	if !ok {
		return nil, errors.NewUnimplemented(op, "prompt method is not exposed by the MCP tool set")
	}
	arguments, err := verb.args(op, req)
	if err != nil {
		return nil, err
	}
	result, err := t.callTool(ctx, verb.tool, arguments)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.NewInternal(op, "empty tool result")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to re-encode the tool result")
	}
	return verb.decode(op, raw)
}
