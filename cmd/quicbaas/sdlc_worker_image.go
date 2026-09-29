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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

var (
	workerImageWait         bool
	workerImageWaitTimeout  time.Duration
	workerImagePollInterval = 10 * time.Second
)

// newSDLCWorkerImageCommand is `quicbaas sdlc worker-image`.
func newSDLCWorkerImageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker-image",
		Short: "The Software Factory worker image refresh: status and on-demand refresh",
		Long: `The daemon keeps the Software Factory worker image current by itself: once a
day (sdlc.worker_image.refresh_interval) it reads the latest
@anthropic-ai/claude-code and @openai/codex from npm and, when the bound image
is behind, rebuilds it, gates it (the image's probes plus a real session per
agent the bindings run), publishes it and rebinds every binding naming a stale
image. "refresh" runs that pass now. See docs/software-factory/worker-image.md.

These are HOST verbs: they need the cross-tenant worker_images permissions a
system-tenant operator holds, and take no --tenant.

Examples:
  quicbaas sdlc worker-image status
  quicbaas sdlc worker-image refresh
  quicbaas sdlc worker-image refresh --wait --wait-timeout 45m`,
	}
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Queue a full refresh pass now",
		Long: `Queue a full refresh pass on the daemon's refresh loop. The command returns
as soon as the pass is queued; with --wait it follows the pass to its end and
exits non-zero unless the bound images end up current or promoted.`,
		Args: cobra.NoArgs,
		RunE: runSDLCWorkerImageRefresh,
	}
	refresh.Flags().BoolVar(&workerImageWait, "wait", false, "Follow the pass until it finishes")
	refresh.Flags().DurationVar(&workerImageWaitTimeout, "wait-timeout", time.Hour, "How long --wait follows the pass")
	cmd.AddCommand(refresh, &cobra.Command{
		Use:   "status",
		Short: "Show the refresh loop's state and what its last pass did",
		Args:  cobra.NoArgs,
		RunE:  runSDLCWorkerImageStatus,
	})
	return cmd
}

// WorkerImageRefreshFailedError is a followed pass that did not leave the
// bound images current.
type WorkerImageRefreshFailedError struct {
	Outcome string
	Detail  string
}

// Error implements the error interface.
func (e *WorkerImageRefreshFailedError) Error() string {
	return "worker image refresh " + e.Outcome + ": " + e.Detail
}

// WorkerImageWaitTimeoutError is a --wait that outlived its deadline.
type WorkerImageWaitTimeoutError struct {
	Timeout time.Duration
}

// Error implements the error interface.
func (e *WorkerImageWaitTimeoutError) Error() string {
	return "worker image refresh still running after " + e.Timeout.String() +
		"; it continues on the daemon — check `quicbaas sdlc worker-image status`"
}

func runSDLCWorkerImageStatus(_ *cobra.Command, _ []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	status, err := client.WorkerImageStatus(ctx)
	if err != nil {
		return formatError(err)
	}
	return formatWorkerImageStatus(status, sdlcOutputFmt, os.Stdout)
}

func runSDLCWorkerImageRefresh(_ *cobra.Command, _ []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	requested := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	status, err := client.RefreshWorkerImage(ctx)
	cancel()
	if err != nil {
		return formatError(err)
	}
	if !workerImageWait {
		return formatWorkerImageStatus(status, sdlcOutputFmt, os.Stdout)
	}
	final, err := waitForWorkerImagePass(client, requested, workerImageWaitTimeout, workerImagePollInterval)
	if err != nil {
		return formatError(err)
	}
	if err := formatWorkerImageStatus(final, sdlcOutputFmt, os.Stdout); err != nil {
		return err
	}
	if outcome := final.Last.Outcome; outcome != "current" && outcome != "promoted" {
		return formatError(&WorkerImageRefreshFailedError{Outcome: outcome, Detail: final.Last.Error})
	}
	return nil
}

// waitForWorkerImagePass polls until a pass that started after requested has
// finished, and returns the status that reports it.
func waitForWorkerImagePass(client sdlcSDK.Client, requested time.Time, timeout, every time.Duration) (*sdlcSDK.WorkerImageStatus, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			return nil, &WorkerImageWaitTimeoutError{Timeout: timeout}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			status, err := client.WorkerImageStatus(ctx)
			cancel()
			if err != nil {
				return nil, err
			}
			if workerImagePassFinished(status, requested) {
				return status, nil
			}
		}
	}
}

// workerImagePassFinished reports whether status shows the requested pass
// finished: none running or queued, and the last report started after the
// request (to the second, the resolution every transport carries).
func workerImagePassFinished(status *sdlcSDK.WorkerImageStatus, requested time.Time) bool {
	return !status.Running && !status.Queued && status.Last != nil &&
		!status.Last.StartedAt.Before(requested.Truncate(time.Second))
}

func formatWorkerImageStatus(status *sdlcSDK.WorkerImageStatus, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(status)
	default:
		return writeWorkerImageTable(status, w)
	}
}

func writeWorkerImageTable(status *sdlcSDK.WorkerImageStatus, w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	state := "idle"
	switch {
	case !status.Serving:
		state = "not serving on this node"
	case status.Running:
		state = "running since " + status.RunningSince.Format(time.RFC3339)
	case status.Queued:
		state = "queued"
	}
	fmt.Fprintf(tw, "Refresh loop:\t%s\n", state)
	last := status.Last
	if last == nil {
		fmt.Fprintf(tw, "Last pass:\t-\n")
		return tw.Flush()
	}
	fmt.Fprintf(tw, "Last pass:\t%s (%s), finished %s\n", last.Outcome, last.Trigger, last.FinishedAt.Format(time.RFC3339))
	fmt.Fprintf(tw, "Registry latest:\tclaude-code %s, codex %s\n", orDash(last.Latest.ClaudeCode), orDash(last.Latest.Codex))
	for _, b := range last.Bound {
		state := "current"
		if b.Stale {
			state = "stale"
		}
		fmt.Fprintf(tw, "Bound image:\t%s (claude-code %s, codex %s; %d uses; %s)\n",
			b.Image, orDash(b.Agents.ClaudeCode), orDash(b.Agents.Codex), b.Uses, state)
	}
	fmt.Fprintf(tw, "Stale images:\t%s\n", orDash(strings.Join(last.Stale, ", ")))
	fmt.Fprintf(tw, "Candidate:\t%s\n", orDash(last.Candidate))
	fmt.Fprintf(tw, "Bound to:\t%s\n", orDash(last.Reference))
	for _, c := range append(append([]sdlcSDK.WorkerImageCheck{}, last.Probes...), last.Smokes...) {
		fmt.Fprintf(tw, "  %s\t%s\n", passLabel(c.Passed), c.Name)
	}
	for _, p := range last.Promotions {
		result := "repointed"
		if p.Error != "" {
			result = "NOT repointed: " + p.Error
		}
		fmt.Fprintf(tw, "  %s/agent:%s\t%s\n", p.TenantID, p.AgentID, result)
	}
	for _, s := range last.Skipped {
		fmt.Fprintf(tw, "  skipped\t%s\n", s)
	}
	if last.Error != "" {
		fmt.Fprintf(tw, "Error:\t%s\n", last.Error)
	}
	return tw.Flush()
}

// passLabel renders a check's verdict.
func passLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}
