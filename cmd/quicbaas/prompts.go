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
	"bufio"
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
	promptSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
)

// promptsFlags holds the flags of the prompts command group.
type promptsFlags struct {
	project         string
	scope           string
	output          string
	raw             bool
	file            string
	note            string
	expectedVersion int64
	version         int64
	yes             bool
}

var promptsOpts promptsFlags

// PromptsClientFactory creates the SDK prompt client the commands use.
type PromptsClientFactory func() (promptSDK.Client, error)

var promptsClientFactory PromptsClientFactory = createSDKPromptsClient

func createSDKPromptsClient() (promptSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Prompts(), nil
}

// PromptContentRequiredError is returned when put is given no content.
type PromptContentRequiredError struct{}

// Error implements the error interface.
func (e *PromptContentRequiredError) Error() string {
	return "prompts put: -f is required (a file holding the new body, or - for stdin)"
}

// PromptContentFileError is returned when the new body cannot be read.
type PromptContentFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *PromptContentFileError) Error() string {
	return fmt.Sprintf("prompts put: read %s: %v", e.Path, e.Cause)
}

// Unwrap returns the underlying error.
func (e *PromptContentFileError) Unwrap() error { return e.Cause }

// PromptConfirmError is returned when the confirmation cannot be read.
type PromptConfirmError struct {
	Cause error
}

// Error implements the error interface.
func (e *PromptConfirmError) Error() string {
	return fmt.Sprintf("prompts: read confirmation: %v", e.Cause)
}

// Unwrap returns the underlying error.
func (e *PromptConfirmError) Unwrap() error { return e.Cause }

func newPromptsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompts",
		Short: "Read and edit the prompt sets the Software Factory's agents are given",
		Long: `Read and edit the prompt sets the worker, the reviewer and the lead are given.

A prompt is addressed by its set and name and resolved through four scopes:
system, tenant, project and the build's embedded default. Reads take the
scope from --scope, the tenant from the global --tenant flag and the project
from --project. Writes override a prompt at the tenant or project scope; every
write is a new revision, so rollback and reset never lose history.

Writes take --expected-version, the version the caller last read, and are
refused when the record has moved on. Put creates a record with 0.

Examples:
  quicbaas prompts list sdlc --tenant acme
  quicbaas prompts effective sdlc worker --tenant acme --project web --raw
  quicbaas prompts diff sdlc worker --tenant acme --scope tenant
  quicbaas prompts put sdlc worker --tenant acme -f worker.md --note "tighten scope" --expected-version 2
  quicbaas prompts rollback sdlc worker --tenant acme --version 1 --expected-version 3 --yes
  quicbaas prompts reset sdlc worker --tenant acme --expected-version 4`,
	}
	cmd.PersistentFlags().StringVar(&promptsOpts.project, "project", "", "Project ID, for the project scope")
	cmd.PersistentFlags().StringVar(&promptsOpts.scope, "scope", "", "Scope (system, tenant, project); inferred from --project when empty")
	cmd.PersistentFlags().StringVar(&promptsOpts.output, "output", "table", "Output format (table, json, yaml)")

	get := &cobra.Command{Use: "get <set> <name>", Short: "Show the record overridden at a scope, or one of its revisions", Args: cobra.ExactArgs(2), RunE: runPromptsGet}
	get.Flags().Int64Var(&promptsOpts.version, "version", 0, "Revision to show (default: the current one)")
	get.Flags().BoolVar(&promptsOpts.raw, "raw", false, "Print only the body")

	effective := &cobra.Command{Use: "effective <set> <name>", Short: "Show the body a job at a scope runs with, and where it came from", Args: cobra.ExactArgs(2), RunE: runPromptsEffective}
	effective.Flags().BoolVar(&promptsOpts.raw, "raw", false, "Print only the body")

	put := &cobra.Command{Use: "put <set> <name>", Short: "Override a prompt at the tenant or project scope", Args: cobra.ExactArgs(2), RunE: runPromptsPut}
	put.Flags().StringVarP(&promptsOpts.file, "file", "f", "", "File holding the new body, or - for stdin")
	addPromptWriteFlags(put)

	rollback := &cobra.Command{Use: "rollback <set> <name>", Short: "Write a new revision carrying an earlier revision's body", Args: cobra.ExactArgs(2), RunE: runPromptsRollback}
	rollback.Flags().Int64Var(&promptsOpts.version, "version", 0, "Revision whose body to restore")
	rollback.Flags().BoolVar(&promptsOpts.yes, "yes", false, "Skip the confirmation prompt")
	addPromptWriteFlags(rollback)

	reset := &cobra.Command{Use: "reset <set> <name>", Short: "Make a scope follow its parent again", Args: cobra.ExactArgs(2), RunE: runPromptsReset}
	reset.Flags().BoolVar(&promptsOpts.yes, "yes", false, "Skip the confirmation prompt")
	addPromptWriteFlags(reset)

	cmd.AddCommand(
		&cobra.Command{Use: "list <set>", Short: "List a set's prompts at a scope", Args: cobra.ExactArgs(1), RunE: runPromptsList},
		get,
		effective,
		&cobra.Command{Use: "diff <set> <name>", Short: "Compare a scope's body with its parent scope's", Args: cobra.ExactArgs(2), RunE: runPromptsDiff},
		&cobra.Command{Use: "history <set> <name>", Short: "List a record's revisions", Args: cobra.ExactArgs(2), RunE: runPromptsHistory},
		put,
		rollback,
		reset,
	)
	return cmd
}

// addPromptWriteFlags adds the flags every write takes.
func addPromptWriteFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&promptsOpts.note, "note", "", "Why the change was made; kept in the history")
	cmd.Flags().Int64Var(&promptsOpts.expectedVersion, "expected-version", 0, "Version the caller last read; the write is refused if the record has moved on")
}

// promptAddress is the address args name at the flags' scope.
func promptAddress(args []string) promptSDK.Address {
	return promptSDK.Address{
		TenantID: globalTenantID, Scope: promptsOpts.scope, ProjectID: promptsOpts.project, Set: args[0], Name: args[1],
	}
}

// promptsCall runs one SDK call with the command timeout and writes its answer.
func promptsCall[T any](cmd *cobra.Command, call func(context.Context, promptSDK.Client) (T, error), write func(io.Writer, T) error) error {
	client, err := promptsClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	out, err := call(ctx, client)
	if err != nil {
		return formatError(err)
	}
	return write(cmd.OutOrStdout(), out)
}

func runPromptsList(cmd *cobra.Command, args []string) error {
	req := &promptSDK.ListRequest{TenantID: globalTenantID, Scope: promptsOpts.scope, ProjectID: promptsOpts.project, Set: args[0]}
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.ListResponse, error) {
		return c.List(ctx, req)
	}, formatPromptList)
}

func runPromptsGet(cmd *cobra.Command, args []string) error {
	req := &promptSDK.GetRequest{Address: promptAddress(args), Version: promptsOpts.version}
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.Record, error) {
		return c.Get(ctx, req)
	}, formatPromptRecord)
}

func runPromptsEffective(cmd *cobra.Command, args []string) error {
	addr := promptAddress(args)
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.EffectivePrompt, error) {
		return c.Effective(ctx, &addr)
	}, formatPromptEffective)
}

func runPromptsDiff(cmd *cobra.Command, args []string) error {
	addr := promptAddress(args)
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.Diff, error) {
		return c.Diff(ctx, &addr)
	}, formatPromptDiff)
}

func runPromptsHistory(cmd *cobra.Command, args []string) error {
	addr := promptAddress(args)
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.History, error) {
		return c.History(ctx, &addr)
	}, formatPromptHistory)
}

func runPromptsPut(cmd *cobra.Command, args []string) error {
	content, err := readPromptContent(promptsOpts.file, cmd.InOrStdin())
	if err != nil {
		return formatError(err)
	}
	req := &promptSDK.PutRequest{Address: promptAddress(args), Content: content, Note: promptsOpts.note, ExpectedVersion: promptsOpts.expectedVersion}
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.Record, error) {
		return c.Put(ctx, req)
	}, formatPromptRecord)
}

func runPromptsRollback(cmd *cobra.Command, args []string) error {
	question := fmt.Sprintf("Roll %s/%s back to version %d? (y/N): ", args[0], args[1], promptsOpts.version)
	if confirmed, err := confirmPromptWrite(cmd, question); err != nil || !confirmed {
		return err
	}
	req := &promptSDK.RollbackRequest{
		Address: promptAddress(args), ToVersion: promptsOpts.version, Note: promptsOpts.note, ExpectedVersion: promptsOpts.expectedVersion,
	}
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.Record, error) {
		return c.Rollback(ctx, req)
	}, formatPromptRecord)
}

func runPromptsReset(cmd *cobra.Command, args []string) error {
	question := fmt.Sprintf("Reset %s/%s to follow its parent scope? (y/N): ", args[0], args[1])
	if confirmed, err := confirmPromptWrite(cmd, question); err != nil || !confirmed {
		return err
	}
	req := &promptSDK.ResetRequest{Address: promptAddress(args), Note: promptsOpts.note, ExpectedVersion: promptsOpts.expectedVersion}
	return promptsCall(cmd, func(ctx context.Context, c promptSDK.Client) (*promptSDK.Record, error) {
		return c.Reset(ctx, req)
	}, formatPromptRecord)
}

// readPromptContent reads the new body from path, or from stdin for "-".
func readPromptContent(path string, stdin io.Reader) (string, error) {
	if path == "" {
		return "", &PromptContentRequiredError{}
	}
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", &PromptContentFileError{Path: "stdin", Cause: err}
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", &PromptContentFileError{Path: path, Cause: err}
	}
	return string(data), nil
}

// confirmPromptWrite asks before a rollback or reset unless --yes was given.
// The question goes to stderr so a declined write leaves stdout empty.
func confirmPromptWrite(cmd *cobra.Command, question string) (bool, error) {
	if promptsOpts.yes {
		return true, nil
	}
	fmt.Fprint(cmd.ErrOrStderr(), question)
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, &PromptConfirmError{Cause: err}
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		fmt.Fprintln(cmd.ErrOrStderr(), "Cancelled")
		return false, nil
	}
}

// writePromptOutput writes v as JSON or YAML, or calls table for the table
// format.
func writePromptOutput(w io.Writer, v interface{}, table func(io.Writer) error) error {
	format, err := cli.ParseOutputFormat(promptsOpts.output)
	if err != nil {
		return err
	}
	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(v)
	default:
		return table(w)
	}
}

// writePromptBody writes a body as-is, ending it with a newline.
func writePromptBody(w io.Writer, content string) error {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	_, err := io.WriteString(w, content)
	return err
}

func formatPromptList(w io.Writer, list *promptSDK.ListResponse) error {
	return writePromptOutput(w, list, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tOVERRIDDEN\tVERSION\tFOLLOWS PARENT\tEFFECTIVE\tDESCRIPTION")
		for _, entry := range list.Prompts {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", entry.Name, yesNo(entry.Overridden), promptVersionLabel(entry.Version),
				yesNo(entry.FollowParent), promptRefLabel(entry.Effective), orDash(entry.Description))
		}
		return tw.Flush()
	})
}

func formatPromptRecord(w io.Writer, record *promptSDK.Record) error {
	if promptsOpts.raw {
		return writePromptBody(w, record.Content)
	}
	return writePromptOutput(w, record, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Prompt:\t%s/%s\n", record.Set, record.Name)
		fmt.Fprintf(tw, "Scope:\t%s\n", promptScopeLabel(record.Scope, record.TenantID, record.ProjectID))
		fmt.Fprintf(tw, "Version:\t%d\n", record.Version)
		fmt.Fprintf(tw, "Follows parent:\t%s\n", yesNo(record.FollowParent))
		fmt.Fprintf(tw, "Hash:\t%s\n", orDash(record.ContentHash))
		fmt.Fprintf(tw, "Updated:\t%s\n", promptTimeLabel(record.UpdatedAt))
		if err := tw.Flush(); err != nil {
			return err
		}
		if record.FollowParent {
			return nil
		}
		fmt.Fprintln(w)
		return writePromptBody(w, record.Content)
	})
}

func formatPromptEffective(w io.Writer, effective *promptSDK.EffectivePrompt) error {
	if promptsOpts.raw {
		return writePromptBody(w, effective.Content)
	}
	return writePromptOutput(w, effective, func(w io.Writer) error {
		fmt.Fprintf(w, "Prompt: %s/%s at %s scope, from %s\n\n", effective.Set, effective.Name, effective.Scope, promptRefLabel(effective.Origin))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SCOPE\tPRESENT\tVERSION\tFOLLOWS PARENT\tANSWERED")
		for _, layer := range effective.Chain {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", promptScopeLabel(layer.Scope, layer.ScopeID, ""), yesNo(layer.Present),
				promptVersionLabel(layer.Version), yesNo(layer.FollowParent), yesNo(layer.Answered))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
		return writePromptBody(w, effective.Content)
	})
}

// diffOpPrefix is the line prefix of each diff operation.
var diffOpPrefix = map[string]string{
	promptSDK.DiffEqual:  " ",
	promptSDK.DiffDelete: "-",
	promptSDK.DiffInsert: "+",
}

func formatPromptDiff(w io.Writer, diff *promptSDK.Diff) error {
	return writePromptOutput(w, diff, func(w io.Writer) error {
		fmt.Fprintf(w, "--- %s\n+++ %s\n", promptRefLabel(diff.ParentOrigin), promptRefLabel(diff.Origin))
		if diff.Same {
			_, err := fmt.Fprintln(w, "(no difference from the parent scope)")
			return err
		}
		for _, line := range diff.Lines {
			prefix, ok := diffOpPrefix[line.Op]
			if !ok {
				prefix = "?"
			}
			if _, err := fmt.Fprintf(w, "%s%s\n", prefix, strings.TrimSuffix(line.Text, "\n")); err != nil {
				return err
			}
		}
		return nil
	})
}

func formatPromptHistory(w io.Writer, history *promptSDK.History) error {
	return writePromptOutput(w, history, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "VERSION\tAUTHOR\tCREATED\tFOLLOWS PARENT\tNOTE")
		for _, rev := range history.Revisions {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", rev.Version, orDash(rev.Author), promptTimeLabel(rev.CreatedAt),
				yesNo(rev.FollowParent), orDash(rev.Note))
		}
		return tw.Flush()
	})
}

// promptRefLabel names a body's origin, such as "tenant acme v3".
func promptRefLabel(ref promptSDK.VersionRef) string {
	if ref.Scope == "" {
		return "-"
	}
	label := promptScopeLabel(ref.Scope, ref.ScopeID, "")
	if ref.Version > 0 {
		label += fmt.Sprintf(" v%d", ref.Version)
	}
	return label
}

// promptScopeLabel names a scope with the identifier that selects it.
func promptScopeLabel(scope, id, projectID string) string {
	if projectID != "" {
		id = projectID
	}
	if id == "" {
		return orDash(scope)
	}
	return scope + " " + id
}

func promptVersionLabel(version int64) string {
	if version == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", version)
}

func promptTimeLabel(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
