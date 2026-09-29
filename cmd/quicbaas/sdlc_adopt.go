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
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// quicbaas sdlc adopt — hand an issue back to the factory
// =============================================================================
//
//   quicbaas sdlc adopt <project-id> <issue> [--pull-request N] [--ruling TEXT]

var (
	sdlcAdoptPullRequest int
	sdlcAdoptRuling      string
)

// IssueNumberError reports an issue argument that is not a positive integer.
type IssueNumberError struct {
	Value string
}

// Error implements the error interface.
func (e *IssueNumberError) Error() string {
	return "sdlc adopt: issue " + strconv.Quote(e.Value) + " is not a positive issue number"
}

func newSDLCAdoptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adopt <project-id> <issue>",
		Short: "Hand an issue back to the factory, optionally resuming its open pull request",
		Long: `Create the task for one forge issue that no live task holds — one whose task
completed, or that triage holds — and hand the issue back to the factory: its
holds (needs-info, escalated, blocked, the portfolio hold) come off and the
trigger label goes on.

Name an open pull request with --pull-request to resume it: the task works on
that pull request's own head branch, never a new branch cut from the issue
title. A --ruling is appended to the task's contract and posted on the issue.

Refused while a live task still holds the issue; readmit that task instead.

Examples:
  quicbaas sdlc adopt go-luks2 209 --pull-request 210
  quicbaas sdlc adopt go-luks2 141 --pull-request 194 --ruling "Land #194 as it stands."`,
		Args: cobra.ExactArgs(2),
		RunE: runSDLCAdopt,
	}
	cmd.Flags().IntVar(&sdlcAdoptPullRequest, "pull-request", 0, "An open pull request the task resumes, by number")
	cmd.Flags().StringVar(&sdlcAdoptRuling, "ruling", "", "The operator's instruction, appended to the task's contract and posted on the issue")
	return cmd
}

func runSDLCAdopt(_ *cobra.Command, args []string) error {
	issue, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil || issue <= 0 {
		return formatError(&IssueNumberError{Value: args[1]})
	}
	if sdlcAdoptPullRequest < 0 {
		return formatError(&PullRequestNumberError{Value: strconv.Itoa(sdlcAdoptPullRequest)})
	}
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	adoption, err := client.AdoptIssue(ctx, &sdlcSDK.AdoptIssueRequest{
		TenantID: globalTenantID, ProjectID: args[0], Issue: issue,
		PullRequest: sdlcAdoptPullRequest, Ruling: sdlcAdoptRuling,
	})
	if err != nil {
		return formatError(err)
	}
	return formatSDLCAdoption(adoption, sdlcOutputFmt, os.Stdout)
}

// formatSDLCAdoption renders what an adoption did.
func formatSDLCAdoption(adoption *sdlcSDK.Adoption, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(adoption)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(adoption)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Task:\t%s\n", adoption.TaskID)
		fmt.Fprintf(tw, "Issue:\t#%d\n", adoption.Issue)
		if adoption.PullRequest > 0 {
			fmt.Fprintf(tw, "Pull request:\t#%d on %s\n", adoption.PullRequest, adoption.Branch)
		} else {
			fmt.Fprintf(tw, "Pull request:\t-\n")
		}
		fmt.Fprintf(tw, "Replaces:\t%s\n", orDash(adoption.Superseded))
		return tw.Flush()
	}
}
