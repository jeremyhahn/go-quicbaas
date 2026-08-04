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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

var (
	attachFromSeq  uint64
	attachNoFollow bool
	attachFormat   string
)

// newOrchAttachCommand builds the `quicbaas orchestrator attach <session-id>`
// CLI. It is the operator-facing surface for watching session events in real
// time and scrolling back through historical transcripts.
//
// Behavior:
//   - Without --from, the stream begins at sequence 1 (full replay)
//   - With --from N, it begins at sequence N (useful for resuming after
//     a disconnect: use `latest_sequence + 1` to tail only new events)
//   - --no-follow returns after the historical page is drained; omit to
//     tail live events until ctrl-c
//
// Output format:
//   - `pretty` (default): human-readable, colorized, one line per event
//   - `json`: one JSON object per line (jq-friendly for scripting)
func newOrchAttachCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <session-id>",
		Short: "Attach to a running session and stream its events",
		Long: `Attach to an agent session and stream every event it produces: prompts
submitted, assistant output (token by token), tool invocations, file
operations, operator joins, and session close.

The stream begins with a historical replay so operators joining a session
in progress see everything from the beginning, then transitions to live
tail until ctrl-c. Multiple operators can attach simultaneously to the
same session — all see identical event order.

Examples:
  # Watch a session from the beginning, following new events as they arrive
  quicbaas orchestrator attach sess_abc123

  # Resume tailing after a disconnect at sequence 412
  quicbaas orchestrator attach sess_abc123 --from 412

  # Dump the full transcript and exit
  quicbaas orchestrator attach sess_abc123 --no-follow --format json > transcript.jsonl`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchAttach,
	}
	cmd.Flags().Uint64Var(&attachFromSeq, "from", 0, "Sequence number to start from (0 = beginning)")
	cmd.Flags().BoolVar(&attachNoFollow, "no-follow", false, "Return after history is drained instead of live-tailing")
	cmd.Flags().StringVar(&attachFormat, "format", "pretty", "Output format: pretty | json")
	return cmd
}

// runOrchAttach executes the attach command.
func runOrchAttach(_ *cobra.Command, args []string) error {
	sessionID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown: ctrl-c cancels the context so the stream goroutine
	// returns and the attach exits. Double ctrl-c within 2s forces exit in
	// case the stream is wedged on the server side.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		cancel()
		// Second signal = hard exit.
		<-sigCh
		os.Exit(130)
	}()

	events, subErr := client.SubscribeSessionEvents(ctx, &orchestratorSDK.SubscribeSessionEventsRequest{
		SessionID:    sessionID,
		FromSequence: attachFromSeq,
		Follow:       !attachNoFollow,
	})
	if subErr != nil {
		return formatError(subErr)
	}

	renderer, err := pickAttachRenderer(attachFormat, os.Stdout)
	if err != nil {
		return formatError(err)
	}

	for env := range events {
		if err := renderer.Render(env); err != nil {
			return formatError(err)
		}
	}
	return nil
}

// attachRenderer is the pluggable interface that lets us swap between
// pretty and json output without special-casing the stream loop.
type attachRenderer interface {
	Render(env *orchestratorSDK.SessionEventEnvelope) error
}

// pickAttachRenderer dispatches on the --format flag value.
func pickAttachRenderer(format string, out *os.File) (attachRenderer, error) {
	switch format {
	case "", "pretty":
		return &prettyAttachRenderer{out: out}, nil
	case "json", "ndjson":
		return &jsonAttachRenderer{out: out}, nil
	default:
		return nil, &MessageError{Message: fmt.Sprintf("orchestrator attach: unknown --format %q (want pretty | json)", format)}
	}
}

// prettyAttachRenderer emits one human-readable line per event. It does
// not attempt to be terminal-aware (no colors) because the output is
// frequently piped into pagers and log collectors; add colors behind a
// --color flag when the UX demands it.
type prettyAttachRenderer struct {
	out *os.File
}

// genericEvent captures the subset of fields the renderer needs without
// tightly coupling to the full SessionEvent struct in pkg/orchestration.
// This keeps the SDK (and the CLI that imports it) free of an
// orchestration-package dependency.
type genericEvent struct {
	Sequence  uint64 `json:"sequence"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source,omitempty"`
	Kind      string `json:"kind"`
	Prompt    *struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model,omitempty"`
	} `json:"prompt,omitempty"`
	AssistantChunk *struct {
		Text       string `json:"text"`
		StopReason string `json:"stop_reason,omitempty"`
	} `json:"assistant_chunk,omitempty"`
	ToolInvocation *struct {
		Name string `json:"name"`
	} `json:"tool_invocation,omitempty"`
	ToolResult *struct {
		Content string `json:"content"`
		IsError bool   `json:"is_error,omitempty"`
	} `json:"tool_result,omitempty"`
	FileOp *struct {
		Op   string `json:"op"`
		Path string `json:"path"`
	} `json:"file_op,omitempty"`
	System *struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	} `json:"system,omitempty"`
	Closed *struct {
		Reason string `json:"reason"`
	} `json:"closed,omitempty"`
}

// Render formats one envelope as a human-readable line. Wall-clock format
// prefers ISO 8601 for machine-grep friendliness.
func (p *prettyAttachRenderer) Render(env *orchestratorSDK.SessionEventEnvelope) error {
	var ev genericEvent
	if err := json.Unmarshal(env.PayloadJSON, &ev); err != nil {
		return &OpError{Op: fmt.Sprintf("attach: decode payload for session=%s seq=%d", env.SessionID, env.Sequence), Cause: err}
	}
	ts := env.Timestamp.UTC().Format("2006-01-02T15:04:05Z")
	prefix := fmt.Sprintf("[%s] seq=%d %s · %s", ts, env.Sequence, env.SessionID, ev.Kind)

	var body string
	switch ev.Kind {
	case "prompt_submitted":
		if ev.Prompt != nil {
			body = "\n  > " + truncateForPretty(ev.Prompt.Prompt, 1024)
		}
	case "assistant_chunk":
		if ev.AssistantChunk != nil {
			body = "\n  " + truncateForPretty(ev.AssistantChunk.Text, 2048)
			if ev.AssistantChunk.StopReason != "" {
				body += "\n  (stop: " + ev.AssistantChunk.StopReason + ")"
			}
		}
	case "tool_invocation":
		if ev.ToolInvocation != nil {
			body = "  tool: " + ev.ToolInvocation.Name
		}
	case "tool_result":
		if ev.ToolResult != nil {
			label := "  → "
			if ev.ToolResult.IsError {
				label = "  → ERROR "
			}
			body = "\n" + label + truncateForPretty(ev.ToolResult.Content, 512)
		}
	case "file_op":
		if ev.FileOp != nil {
			body = "  " + ev.FileOp.Op + " " + ev.FileOp.Path
		}
	case "system_message":
		if ev.System != nil {
			body = "  [" + ev.System.Level + "] " + ev.System.Message
		}
	case "session_closed":
		reason := "unknown"
		if ev.Closed != nil {
			reason = ev.Closed.Reason
		}
		body = "  (reason: " + reason + ")"
	}

	if _, err := fmt.Fprintln(p.out, prefix+body); err != nil {
		return err
	}
	return nil
}

// truncateForPretty caps a string at n bytes with an ellipsis. Operators
// reading the terminal rarely want 100KB assistant chunks inline.
func truncateForPretty(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// jsonAttachRenderer emits one JSON object per line: the outer envelope's
// fields plus an inlined payload object. Compatible with jq piping.
type jsonAttachRenderer struct {
	out *os.File
}

// Render dumps the envelope + decoded payload as a single-line JSON object.
func (j *jsonAttachRenderer) Render(env *orchestratorSDK.SessionEventEnvelope) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(env.PayloadJSON, &payload); err != nil {
		return &OpError{Op: "attach: decode payload", Cause: err}
	}
	out := map[string]interface{}{
		"session_id":  env.SessionID,
		"tenant_id":   env.TenantID,
		"sequence":    env.Sequence,
		"timestamp":   env.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		"from_replay": env.FromReplay,
		"event":       payload,
	}
	return json.NewEncoder(j.out).Encode(out)
}
