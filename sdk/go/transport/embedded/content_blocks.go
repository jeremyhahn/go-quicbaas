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
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// promptContentBlocks converts a service turn's content blocks to the SDK's,
// tool metadata included.
func promptContentBlocks(blocks []orchestration.ContentBlock) []orchestratorSDK.PromptContentBlock {
	out := make([]orchestratorSDK.PromptContentBlock, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, orchestratorSDK.PromptContentBlock{
			Type:      b.Type,
			Text:      b.Text,
			ToolName:  b.ToolName,
			ToolUseID: b.ToolUseID,
			IsError:   b.IsError,
			Bytes:     b.Bytes,
		})
	}
	return out
}

// inferenceContentBlocks converts inference content blocks to the SDK's,
// tool metadata included.
func inferenceContentBlocks(blocks []orchestration.ContentBlock) []orchestratorSDK.InferenceContentBlock {
	out := make([]orchestratorSDK.InferenceContentBlock, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, orchestratorSDK.InferenceContentBlock{
			Type:      b.Type,
			Text:      b.Text,
			ToolName:  b.ToolName,
			ToolUseID: b.ToolUseID,
			IsError:   b.IsError,
			Bytes:     b.Bytes,
		})
	}
	return out
}
