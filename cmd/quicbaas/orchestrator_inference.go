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

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// Inference command flags
// =============================================================================

var (
	inferBackend      string
	inferModel        string
	inferPrompt       string
	inferSystemPrompt string
	inferHostAffinity string
	inferStream       bool
	inferOutputFmt    string
)

// =============================================================================
// Command constructor
// =============================================================================

// newOrchInferenceCommand creates the `orchestrator inference` subcommand.
func newOrchInferenceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inference",
		Short: "Run a stateless fleet-routed inference call",
		Long: `Run a stateless prompt against a named backend on a fleet-resident agent
daemon without requiring a prior session spawn. Designed for backends like
Ollama where each call is self-contained.

The prompt is routed through the orchestrator's RemoteBackendResolver which
applies host-affinity matching and tenant scoping before forwarding the call
to the daemon's local backend over the existing mTLS-authenticated gRPC
connection.

Examples:
  quicbaas orchestrator inference \
    --backend ollama \
    --host-affinity agent-01 \
    --model qwen3:1.7b \
    --prompt "write a haiku about mTLS"

  quicbaas orchestrator inference \
    --backend ollama \
    --model qwen3:1.7b \
    --prompt "explain QUIC in three sentences" \
    --stream`,
		RunE: runOrchInference,
	}

	cmd.Flags().StringVar(&inferBackend, "backend", "", "Backend to route the inference to (e.g., ollama). Required.")
	cmd.Flags().StringVar(&inferModel, "model", "", "Model to use (default: backend's default model)")
	cmd.Flags().StringVar(&inferPrompt, "prompt", "", "Prompt text to send to the backend. Required.")
	cmd.Flags().StringVar(&inferSystemPrompt, "system-prompt", "", "Optional system prompt prepended to the conversation")
	cmd.Flags().StringVar(&inferHostAffinity, "host-affinity", "", "Pin the call to a specific daemon by Name or Host (e.g., 'agent-01')")
	cmd.Flags().BoolVar(&inferStream, "stream", false, "Stream response deltas as they arrive instead of waiting for the full response")
	cmd.Flags().StringVar(&inferOutputFmt, "output", "text", "Output format: text (default) or json")

	_ = cmd.MarkFlagRequired("backend")
	_ = cmd.MarkFlagRequired("prompt")

	return cmd
}

// runOrchInference dispatches the inference request. When --stream is set it
// prints each delta as it arrives (flushing stdout per delta); otherwise it
// waits for the aggregated response and prints the full text.
func runOrchInference(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.InferenceRequest{
		Backend:      inferBackend,
		Model:        inferModel,
		Prompt:       inferPrompt,
		SystemPrompt: inferSystemPrompt,
		HostAffinity: inferHostAffinity,
	}

	if inferStream {
		return runOrchInferenceStream(ctx, client, req)
	}
	return runOrchInferenceAggregate(ctx, client, req)
}

// runOrchInferenceAggregate waits for the full response and prints it.
func runOrchInferenceAggregate(ctx context.Context, client OrchestratorClient, req *orchestratorSDK.InferenceRequest) error {
	resp, err := client.Inference(ctx, req)
	if err != nil {
		return formatError(err)
	}

	if inferOutputFmt == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	// text mode: concatenate text blocks
	fmt.Print(extractText(resp.Content))
	// ensure a trailing newline
	if !strings.HasSuffix(extractText(resp.Content), "\n") {
		fmt.Println()
	}
	return nil
}

// runOrchInferenceStream prints each delta as it arrives.
func runOrchInferenceStream(ctx context.Context, client OrchestratorClient, req *orchestratorSDK.InferenceRequest) error {
	chunks, err := client.InferenceStream(ctx, req)
	if err != nil {
		return formatError(err)
	}

	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	for chunk := range chunks {
		if chunk == nil {
			continue
		}
		if inferOutputFmt == "json" {
			line, jErr := json.Marshal(chunk)
			if jErr != nil {
				return formatError(jErr)
			}
			_, _ = bw.Write(line)
			_, _ = bw.WriteString("\n")
		} else {
			_, _ = bw.WriteString(inferenceBlockText(chunk.Delta))
		}
		_ = bw.Flush()
	}

	return nil
}

// extractText concatenates the text from InferenceContentBlock slices.
func extractText(blocks []orchestratorSDK.InferenceContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// inferenceBlockText concatenates text from InferenceContentBlock delta slices.
func inferenceBlockText(blocks []orchestratorSDK.InferenceContentBlock) string {
	return extractText(blocks)
}
