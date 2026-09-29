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
// quicbaas sdlc issue create / quicbaas sdlc collaborators
// =============================================================================
//
//   quicbaas sdlc issue create <project-id> --title T [--body B | --body-file F] [--label L]... [--parent N]
//   quicbaas sdlc collaborators <project-id>
//
// Both act through the project's binding and its forge token, so the factory
// is operated through QuicBaaS rather than around it.

// sdlcIssueFlags are the flags of "sdlc issue create".
type sdlcIssueFlags struct {
	title    string
	body     string
	bodyFile string
	labels   []string
	parent   int
}

var sdlcIssue sdlcIssueFlags

// IssueTitleRequiredError reports "sdlc issue create" without --title.
type IssueTitleRequiredError struct{}

// Error implements the error interface.
func (e *IssueTitleRequiredError) Error() string {
	return "sdlc issue create: --title is required"
}

// IssueBodyConflictError reports both --body and --body-file.
type IssueBodyConflictError struct{}

// Error implements the error interface.
func (e *IssueBodyConflictError) Error() string {
	return "sdlc issue create: give the body with --body or --body-file, not both"
}

// IssueParentError reports a --parent that is not a positive issue number.
type IssueParentError struct {
	Value int
}

// Error implements the error interface.
func (e *IssueParentError) Error() string {
	return "sdlc issue create: --parent " + strconv.Itoa(e.Value) + " is not a positive issue number"
}

// IssueBodyFileError reports a --body-file that could not be read.
type IssueBodyFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *IssueBodyFileError) Error() string {
	return "sdlc issue create: " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the cause for errors.Is/As support.
func (e *IssueBodyFileError) Unwrap() error { return e.Cause }

func newSDLCIssueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "File issues on a bound project's repository",
	}
	create := &cobra.Command{
		Use:   "create <project-id>",
		Short: "File an issue on the project's repository through its binding",
		Long: `File an issue on the bound repository with the binding's own forge token,
for example to split an issue into follow-ups. Labels must already exist on the
repository; task:ready hands the new issue straight to the Software Factory.
--parent links the issue it was split out of: the body gains a "Parent: #N"
line, which the forge shows on the parent as a cross-reference.

Every filing is audited. Requires sdlc:issue_create.

Examples:
  quicbaas sdlc issue create go-quicbaas --title "Split: codec half" --label task:ready --parent 1075
  quicbaas sdlc issue create go-quicbaas --title "Follow-up" --body-file follow-up.md`,
		Args: cobra.ExactArgs(1),
		RunE: runSDLCIssueCreate,
	}
	create.Flags().StringVar(&sdlcIssue.title, "title", "", "Issue title (required, one line)")
	create.Flags().StringVar(&sdlcIssue.body, "body", "", "Issue body")
	create.Flags().StringVar(&sdlcIssue.bodyFile, "body-file", "", "Read the issue body from a file")
	create.Flags().StringArrayVar(&sdlcIssue.labels, "label", nil, "Label to apply; repeat for several")
	create.Flags().IntVar(&sdlcIssue.parent, "parent", 0, "The issue this one was split out of")
	cmd.AddCommand(create)
	return cmd
}

func newSDLCCollaboratorsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "collaborators <project-id>",
		Short: "List the accounts that may rule on a bound project's repository",
		Long: `List the bound repository's owner and every collaborator: the accounts a
binding's rulers may name. A ruler's comment on a parked issue is an owner
ruling that readmits the task.`,
		Args: cobra.ExactArgs(1),
		RunE: runSDLCCollaborators,
	}
}

// issueFilingRequest builds the request from the flags, refusing bad input
// before any client is built.
func issueFilingRequest(projectID string, flags sdlcIssueFlags) (*sdlcSDK.FileIssueRequest, error) {
	if strings.TrimSpace(flags.title) == "" {
		return nil, &IssueTitleRequiredError{}
	}
	if flags.parent < 0 {
		return nil, &IssueParentError{Value: flags.parent}
	}
	body := flags.body
	if flags.bodyFile != "" {
		if flags.body != "" {
			return nil, &IssueBodyConflictError{}
		}
		data, err := os.ReadFile(flags.bodyFile)
		if err != nil {
			return nil, &IssueBodyFileError{Path: flags.bodyFile, Cause: err}
		}
		body = string(data)
	}
	return &sdlcSDK.FileIssueRequest{
		TenantID: globalTenantID, ProjectID: projectID, Title: flags.title, Body: body,
		Labels: flags.labels, Parent: flags.parent,
	}, nil
}

func runSDLCIssueCreate(_ *cobra.Command, args []string) error {
	req, err := issueFilingRequest(args[0], sdlcIssue)
	if err != nil {
		return formatError(err)
	}
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	issue, err := client.FileIssue(ctx, req)
	if err != nil {
		return formatError(err)
	}
	return formatSDLCIssue(issue, sdlcOutputFmt, os.Stdout)
}

func runSDLCCollaborators(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	users, err := client.Collaborators(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}
	return formatSDLCCollaborators(users, sdlcOutputFmt, os.Stdout)
}

// encodeStructured writes v as JSON or YAML, and reports whether it did.
func encodeStructured(v any, format cli.OutputFormat, w io.Writer) (bool, error) {
	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return true, encoder.Encode(v)
	case cli.OutputFormatYAML:
		return true, yaml.NewEncoder(w).Encode(v)
	default:
		return false, nil
	}
}

// formatSDLCIssue renders a filed issue.
func formatSDLCIssue(issue *sdlcSDK.ForgeIssue, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if done, err := encodeStructured(issue, outputFmt, w); done {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Issue:\t#%d\n", issue.Number)
	fmt.Fprintf(tw, "Title:\t%s\n", issue.Title)
	fmt.Fprintf(tw, "Labels:\t%s\n", orDash(strings.Join(issue.Labels, ", ")))
	fmt.Fprintf(tw, "URL:\t%s\n", orDash(issue.URL))
	return tw.Flush()
}

// formatSDLCCollaborators renders the collaborator listing.
func formatSDLCCollaborators(users *sdlcSDK.CollaboratorsResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if done, err := encodeStructured(users, outputFmt, w); done {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LOGIN\tNAME")
	for _, user := range users.Users {
		fmt.Fprintf(tw, "%s\t%s\n", user.Login, orDash(user.FullName))
	}
	return tw.Flush()
}

// rulersLabel names who rules on the project's parked tasks beyond the node's
// configured rulers: the repository's owner, then the binding's own.
func rulersLabel(binding *sdlcSDK.ProjectBinding) string {
	rulers := append([]string{binding.Forge.Owner + " (owner)"}, binding.Rulers...)
	return strings.Join(rulers, ", ") + " + node rulers"
}
