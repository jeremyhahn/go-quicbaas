//go:build quic && iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quic

import (
	"context"
)

// tryHandleIAMRequest attempts to handle IAM-specific opcodes.
// Returns nil if the opcode is not IAM-related.
func (h *Handler) tryHandleIAMRequest(ctx context.Context, req *Request) *Response {
	return h.HandleRequestIAM(ctx, req)
}

// extendOpcodeToString extends OpcodeToString with IAM-specific opcodes.
func extendOpcodeToString(opcode byte) string {
	return OpcodeToStringIAM(opcode)
}
