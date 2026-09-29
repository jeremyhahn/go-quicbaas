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
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// quicbaas sdlc reviewer — which reviewer profile judges one pull request
// =============================================================================
//
//   quicbaas sdlc reviewer <project-id> <pull-request>
//   quicbaas sdlc reviewer set <project-id> <pull-request> <profile>
//   quicbaas sdlc reviewer clear <project-id> <pull-request>
//
// The override is an operator's record, written here and nowhere a forge can
// reach: never a label, a comment or a webhook payload.

// PullRequestNumberError reports a pull request argument that is not a
// positive integer.
type PullRequestNumberError struct {
	Value string
}

// Error implements the error interface.
func (e *PullRequestNumberError) Error() string {
	return "sdlc reviewer: pull request " + strconv.Quote(e.Value) + " is not a positive pull request number"
}

func newSDLCReviewerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reviewer <project-id> <pull-request>",
		Short: "Show, set or clear which reviewer profile reviews a pull request",
		Long: `Show which of the binding's reviewer profiles reviews a pull request: the
operator's override when one is set, the binding's default otherwise.

A profile that counts toward merge publishes under the required review status
context and its verdict decides whether the change may land. An ADVISORY
profile publishes under a context of its own and its verdict is shown but never
counted: it can neither approve nor block a change.

Setting or clearing an override never cancels anything. A review already
running for the pull request finishes and publishes normally; every new review
dispatch uses the chosen reviewer, and one starts at once when none is running.

Examples:
  quicbaas sdlc reviewer go-quicbaas 1075
  quicbaas sdlc reviewer set go-quicbaas 1075 qwen
  quicbaas sdlc reviewer clear go-quicbaas 1075`,
		Args: cobra.ExactArgs(2),
		RunE: runSDLCReviewer,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "set <project-id> <pull-request> <profile>",
			Short: "Review one pull request with a named profile of the binding",
			Args:  cobra.ExactArgs(3),
			RunE:  runSDLCReviewerSet,
		},
		&cobra.Command{
			Use:   "clear <project-id> <pull-request>",
			Short: "Clear a pull request's override, so the binding's default reviews it",
			Args:  cobra.ExactArgs(2),
			RunE:  runSDLCReviewerClear,
		},
	)
	return cmd
}

// parsePullRequest reads a pull request number argument.
func parsePullRequest(arg string) (int, error) {
	number, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || number <= 0 {
		return 0, &PullRequestNumberError{Value: arg}
	}
	return number, nil
}

// reviewerVerb runs one reviewer-override call and prints the selection.
//
// note, when set, is a line printed under the table saying what the call did;
// the JSON and YAML formats carry the same fact as fields.
func reviewerVerb(args []string, note func(*sdlcSDK.ReviewerSelection) string, call func(ctx context.Context, client sdlcSDK.Client, projectID string, number int) (*sdlcSDK.ReviewerSelection, error)) error {
	number, err := parsePullRequest(args[1])
	if err != nil {
		return formatError(err)
	}
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	selection, err := call(ctx, client, args[0], number)
	if err != nil {
		return formatError(err)
	}
	if err := formatSDLCReviewerSelection(selection, sdlcOutputFmt, os.Stdout); err != nil {
		return err
	}
	outputFmt, err := cli.ParseOutputFormat(sdlcOutputFmt)
	if err != nil {
		return err
	}
	if note != nil && outputFmt == cli.OutputFormatTable {
		fmt.Fprintln(os.Stdout, note(selection))
	}
	return nil
}

// reviewerSwitchNote says what switching the reviewer did. It never cancels
// anything: either no review was running and one by the chosen reviewer
// started, or the running one finishes and the next dispatch is the chosen
// reviewer's.
func reviewerSwitchNote(selection *sdlcSDK.ReviewerSelection) string {
	if selection.Redispatched {
		return "A review by " + selection.Profile + " started now."
	}
	return "Nothing was cancelled: a review already running finishes, and the next review dispatch uses " + selection.Profile + "."
}

func runSDLCReviewer(_ *cobra.Command, args []string) error {
	return reviewerVerb(args, nil, func(ctx context.Context, client sdlcSDK.Client, projectID string, number int) (*sdlcSDK.ReviewerSelection, error) {
		return client.Reviewer(ctx, globalTenantID, projectID, number)
	})
}

func runSDLCReviewerSet(_ *cobra.Command, args []string) error {
	return reviewerVerb(args, reviewerSwitchNote, func(ctx context.Context, client sdlcSDK.Client, projectID string, number int) (*sdlcSDK.ReviewerSelection, error) {
		return client.SetReviewer(ctx, &sdlcSDK.SetReviewerRequest{
			TenantID: globalTenantID, ProjectID: projectID, PullRequest: number, Profile: args[2],
		})
	})
}

func runSDLCReviewerClear(_ *cobra.Command, args []string) error {
	return reviewerVerb(args, nil, func(ctx context.Context, client sdlcSDK.Client, projectID string, number int) (*sdlcSDK.ReviewerSelection, error) {
		return client.ClearReviewer(ctx, globalTenantID, projectID, number)
	})
}

// formatSDLCReviewerSelection renders a selection.
func formatSDLCReviewerSelection(selection *sdlcSDK.ReviewerSelection, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(selection)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(selection)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Project:\t%s\n", selection.ProjectID)
		fmt.Fprintf(tw, "Pull request:\t#%d\n", selection.PullRequest)
		fmt.Fprintf(tw, "Reviewer:\t%s\n", selectionSourceLabel(selection))
		fmt.Fprintf(tw, "Counts toward merge:\t%s\n", mergeRoleLabel(selection.CountsTowardMerge))
		fmt.Fprintf(tw, "Status context:\t%s\n", orDash(selection.StatusContext))
		if !selection.UpdatedAt.IsZero() {
			fmt.Fprintf(tw, "Override set:\t%s\n", selection.UpdatedAt.Format(time.RFC3339))
		}
		return tw.Flush()
	}
}

// selectionSourceLabel names the profile and says who chose it.
func selectionSourceLabel(selection *sdlcSDK.ReviewerSelection) string {
	if selection.Overridden {
		return selection.Profile + " (operator override)"
	}
	return selection.Profile + " (project default)"
}

// mergeRoleLabel says in words what an advisory review is, because "false"
// would not tell an operator that the verdict cannot land or hold the change.
func mergeRoleLabel(counts bool) string {
	if counts {
		return "yes"
	}
	return "no (advisory: shown, never counted)"
}

// alternatesLabel renders the binding's alternate reviewer profiles as one
// row, so the binding table shows every reviewer an override could name.
func alternatesLabel(alternates []sdlcSDK.ReviewerConfig) string {
	if len(alternates) == 0 {
		return "-"
	}
	labels := make([]string, 0, len(alternates))
	for _, alternate := range alternates {
		role := "advisory"
		if alternate.CountsTowardMerge != nil && *alternate.CountsTowardMerge {
			role = "counts toward merge"
		}
		labels = append(labels, alternate.Name+": "+reviewerLabel(alternate)+" ["+role+"]")
	}
	return strings.Join(labels, "; ")
}
