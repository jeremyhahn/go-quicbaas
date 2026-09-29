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

package orchestration

import "encoding/json"

// =============================================================================
// A remote turn's usage, carried back on its Prompt response
// =============================================================================
//
// THE WORKER DAEMON HAS NO LEDGER. Usage is charged where the turn is planned,
// in agentServiceImpl.runPromptTurn, from PromptResponse.Usage. A remote
// daemon's response carried no usage, so every remote turn reached the ledger
// as "reported nothing", and the burn-in pool showed zero tokens on all three
// accounts after days of work (go-quicbaas#1046). The daemon now sends the
// figure its CLI reported, as JSON so the accounting shape is the
// orchestration package's alone, and this side decodes it through
// TokenUsage.UnmarshalJSON, which repairs negative counts at the boundary.

// PromptUsageToWire encodes a turn's usage for PromptSessionResponse.usage_json.
// Nil usage encodes to nil, so "reported nothing" survives the trip.
func PromptUsageToWire(usage *TokenUsage) ([]byte, error) {
	if usage == nil {
		return nil, nil
	}
	data, err := json.Marshal(usage)
	if err != nil {
		return nil, &TurnUsageWireError{Op: "encode", Cause: err}
	}
	return data, nil
}

// promptUsageFromWire decodes PromptSessionResponse.usage_json. Empty bytes
// are "reported nothing" and decode to nil.
func promptUsageFromWire(data []byte) (*TokenUsage, error) {
	if len(data) == 0 {
		return nil, nil
	}
	usage := &TokenUsage{}
	if err := json.Unmarshal(data, usage); err != nil {
		return nil, &TurnUsageWireError{Op: "decode", Cause: err}
	}
	return usage, nil
}

// TurnUsageWireError reports a turn's usage that could not cross the daemon
// wire.
type TurnUsageWireError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *TurnUsageWireError) Error() string {
	return "orchestration: could not " + e.Op + " a remote turn's usage: " + e.Cause.Error()
}

// Unwrap returns the codec failure.
func (e *TurnUsageWireError) Unwrap() error { return e.Cause }
