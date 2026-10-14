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
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	agentsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// quicbaas sdlc — the SDLC module of the Software Factory
// =============================================================================
//
//   quicbaas sdlc bind <project-id> --file binding.yaml
//   quicbaas sdlc unbind <project-id>
//   quicbaas sdlc binding <project-id>
//   quicbaas sdlc delivery <task-id>
//
// Tasks, jobs and sessions are `quicbaas orchestrator`; readmitting a
// blocked task is `quicbaas orchestrator task readmit`.

var (
	sdlcOutputFmt string
	sdlcFile      string
	sdlcPaused    bool
)

// SDLCClientFactory creates the SDLC client; tests substitute a fake.
type SDLCClientFactory func() (sdlcSDK.Client, error)

var sdlcClientFactory SDLCClientFactory = createSDKSDLCClient

func createSDKSDLCClient() (sdlcSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.SDLC(), nil
}

// BindingFileRequiredError reports a bind without a binding file.
type BindingFileRequiredError struct{}

// Error implements the error interface.
func (e *BindingFileRequiredError) Error() string {
	return "sdlc bind: --file is required (the project binding as YAML or JSON)"
}

// BindingFileError reports a binding file that could not be read or parsed.
type BindingFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *BindingFileError) Error() string {
	return "sdlc bind: " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the cause for errors.Is/As support.
func (e *BindingFileError) Unwrap() error { return e.Cause }

func newSDLCCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sdlc",
		Short: "SDLC module of the Software Factory: bind a forge repository to a project",
		Long: `Bind a forge repository and its review policy to an orchestration project,
so that issues labelled with the binding's trigger label become tasks the
Software Factory branches, implements, reviews and merges.

Bindings are tenant-scoped: every command operates within the tenant named by
the global --tenant flag. The project itself (repository URL, default workflow,
commit signing) is managed with "quicbaas project"; its tasks, jobs and
sessions with "quicbaas orchestrator".

Binding a project also ONBOARDS its repository: the webhook that delivers to
this node and the label vocabulary the binding declares are provisioned on
the forge, idempotently, with nothing committed to the repository itself.
"conformance" reports whether a repository is still in that state and names
exactly what is not; "onboard" repairs it.

Examples:
  quicbaas sdlc bind go-truststrap --file go-truststrap-binding.yaml
  quicbaas sdlc binding go-truststrap --output json
  quicbaas sdlc conformance go-truststrap
  quicbaas sdlc open-work go-truststrap
  quicbaas sdlc onboard go-truststrap
  quicbaas sdlc delivery 1788567351228350294
  quicbaas sdlc reviewer set go-truststrap 42 qwen
  quicbaas sdlc issue create go-truststrap --title "Follow-up" --label task:ready --parent 41
  quicbaas sdlc collaborators go-truststrap
  quicbaas sdlc unbind go-truststrap
  quicbaas sdlc worker-image refresh --wait`,
	}
	cmd.PersistentFlags().StringVar(&sdlcOutputFmt, "output", "table", "Output format (json, yaml, table)")

	bind := &cobra.Command{
		Use:   "bind <project-id>",
		Short: "Attach a forge binding to a project from a file",
		Args:  cobra.ExactArgs(1),
		RunE:  runSDLCBind,
	}
	bind.Flags().StringVarP(&sdlcFile, "file", "f", "", "Project binding (YAML or JSON)")
	bind.Flags().BoolVar(&sdlcPaused, "paused", false, "Bind without switching the project's autonomy on; never turns it off")

	cmd.AddCommand(bind,
		&cobra.Command{Use: "unbind <project-id>", Short: "Detach a project's forge binding", Args: cobra.ExactArgs(1), RunE: runSDLCUnbind},
		&cobra.Command{Use: "binding <project-id>", Short: "Show a project's forge binding", Args: cobra.ExactArgs(1), RunE: runSDLCBinding},
		&cobra.Command{Use: "delivery <task-id>", Short: "Show what a task's attempt delivered", Args: cobra.ExactArgs(1), RunE: runSDLCDelivery},
		&cobra.Command{
			Use:   "onboard <project-id>",
			Short: "Provision the bound repository's webhook and labels on its forge",
			Long: `Provision the bound repository's forge configuration: the webhook that
delivers to this node and the label vocabulary the binding declares.

Idempotent — a repository already in the desired state is left alone, and one
whose webhook was deleted by hand gets it back — so onboarding a fleet is a
loop nobody has to keep notes about. Existing labels are never renamed,
recoloured or removed: they carry the history of every pull request that ever
wore one.

This is the repair for a "conformance" finding. Conformance itself only
reports.`,
			Args: cobra.ExactArgs(1),
			RunE: runSDLCOnboard,
		},
		&cobra.Command{
			Use:   "conformance <project-id>",
			Short: "Report whether a project is correctly onboarded, and what is wrong",
			Long: `Report whether QuicBaaS can actually drive this repository, naming each
failure distinctly: a missing, disabled or misdirected webhook; a label the
binding declares and the repository does not define; an orchestration workflow
still committed to the repository; a protected branch that does not require
the status contexts QuicBaaS writes.

READ-ONLY. It changes nothing — repair is "quicbaas sdlc onboard". Exits
non-zero when the project is not conformant, so it can gate a script.`,
			Args: cobra.ExactArgs(1),
			RunE: runSDLCConformance,
		},
		&cobra.Command{
			Use:   "open-work <project-id>",
			Short: "List a bound project's open factory pull requests and labelled issues",
			Long: `List what the factory has in flight on a bound project's forge: the open
pull requests the project's agent opened, each with its head branch, CI state,
review state and age, and the open issues carrying a label the factory writes
(its display labels, its trigger label and task:blocked).

READ-ONLY. It is read from the forge, so it shows work whose task is gone too.`,
			Args: cobra.ExactArgs(1),
			RunE: runSDLCOpenWork,
		},
		newSDLCReviewerCommand(),
		newSDLCIssueCommand(),
		newSDLCCollaboratorsCommand(),
		newSDLCAdoptCommand(),
		newSDLCWorkerImageCommand(),
	)
	return cmd
}

// readBindingFile reads a binding record as YAML or JSON.
func readBindingFile(path string) (*sdlcSDK.ProjectBinding, error) {
	if path == "" {
		return nil, &BindingFileRequiredError{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &BindingFileError{Path: path, Cause: err}
	}
	var binding sdlcSDK.ProjectBinding
	if err := yaml.Unmarshal(data, &binding); err != nil {
		return nil, &BindingFileError{Path: path, Cause: err}
	}
	return &binding, nil
}

func runSDLCBind(_ *cobra.Command, args []string) error {
	binding, err := readBindingFile(sdlcFile)
	if err != nil {
		return formatError(err)
	}
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	bound, err := client.Bind(ctx, &sdlcSDK.BindRequest{TenantID: globalTenantID, ProjectID: args[0], Binding: *binding, Paused: sdlcPaused})
	if err != nil {
		return formatError(err)
	}
	return formatSDLCBinding(bound, sdlcOutputFmt, os.Stdout)
}

func runSDLCUnbind(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	if err := client.Unbind(ctx, globalTenantID, args[0]); err != nil {
		return formatError(err)
	}
	return formatSDLCUnbound(args[0], sdlcOutputFmt, os.Stdout)
}

func runSDLCBinding(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	binding, err := client.Binding(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}
	return formatSDLCBinding(binding, sdlcOutputFmt, os.Stdout)
}

func runSDLCDelivery(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	delivery, err := client.Delivery(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}
	return formatSDLCDelivery(delivery, sdlcOutputFmt, os.Stdout)
}

func runSDLCOnboard(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	report, err := client.Onboard(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}
	return formatSDLCOnboarding(report, sdlcOutputFmt, os.Stdout)
}

// runSDLCConformance reports and EXITS NON-ZERO when the project is not
// conformant, so `quicbaas sdlc conformance <project>` can gate a script the
// way a test command does. The report is still printed either way: an exit
// code that says "something is wrong" without saying what is the thing this
// verb exists to replace.
func runSDLCConformance(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	report, err := client.Conformance(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}
	if err := formatSDLCConformance(report, sdlcOutputFmt, os.Stdout); err != nil {
		return err
	}
	if !report.Conformant {
		return &SDLCNotConformantError{ProjectID: args[0], Findings: len(report.Findings)}
	}
	return nil
}

func runSDLCOpenWork(_ *cobra.Command, args []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	work, err := client.OpenWork(ctx, globalTenantID, args[0])
	if err != nil {
		return formatError(err)
	}
	return formatSDLCOpenWork(work, sdlcOutputFmt, os.Stdout)
}

func formatSDLCOpenWork(work *sdlcSDK.OpenWork, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(work)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(work)
	default:
		return writeOpenWorkTable(work, w)
	}
}

// writeOpenWorkTable prints the project's open pull requests, then its
// labelled issues, each section present even when empty so "none" is said
// rather than implied.
func writeOpenWorkTable(work *sdlcSDK.OpenWork, w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Project: %s  Repository: %s  Agent: %s\n\nPull requests (%d):\n",
		work.ProjectID, orDash(work.Repo), orDash(work.Agent), len(work.PullRequests)); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  #\tHEAD\tCI\tREVIEW\tAGE\tTITLE")
	for _, pr := range work.PullRequests {
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\t%s\n", pr.Number, pr.HeadRef, pr.CIState, pr.ReviewState, ageLabel(pr.AgeSeconds), pr.Title)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\nIssues (%d):\n", len(work.Issues)); err != nil {
		return err
	}
	itw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(itw, "  #\tLABELS\tAGE\tTITLE")
	for _, issue := range work.Issues {
		fmt.Fprintf(itw, "  %d\t%s\t%s\t%s\n", issue.Number, strings.Join(issue.Labels, ","), ageLabel(issue.AgeSeconds), issue.Title)
	}
	return itw.Flush()
}

// ageLabel renders an age in seconds as days and hours, hours and minutes,
// or minutes: the precision an operator triaging stale work reads.
func ageLabel(seconds int64) string {
	age := time.Duration(seconds) * time.Second
	days := int64(age / (24 * time.Hour))
	hours := int64(age % (24 * time.Hour) / time.Hour)
	minutes := int64(age % time.Hour / time.Minute)
	switch {
	case days > 0:
		return strconv.FormatInt(days, 10) + "d" + strconv.FormatInt(hours, 10) + "h"
	case hours > 0:
		return strconv.FormatInt(hours, 10) + "h" + strconv.FormatInt(minutes, 10) + "m"
	default:
		return strconv.FormatInt(minutes, 10) + "m"
	}
}

// SDLCNotConformantError reports a project that is not correctly onboarded,
// so the command exits non-zero after printing what is wrong.
type SDLCNotConformantError struct {
	ProjectID string
	Findings  int
}

// Error implements the error interface.
func (e *SDLCNotConformantError) Error() string {
	return "project " + e.ProjectID + " is not conformant: " + strconv.Itoa(e.Findings) + " finding(s) above"
}

func formatSDLCOnboarding(report *sdlcSDK.OnboardingReport, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(report)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Project:\t%s\n", report.ProjectID)
		fmt.Fprintf(tw, "Repository:\t%s\n", orDash(report.Repo))
		fmt.Fprintf(tw, "Webhook:\t%s\n", webhookLabel(report))
		fmt.Fprintf(tw, "Labels created:\t%s\n", orDash(strings.Join(report.LabelsCreated, ", ")))
		if err := tw.Flush(); err != nil {
			return err
		}
		if report.Conformance == nil {
			return nil
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		return writeConformanceTable(report.Conformance, w)
	}
}

// webhookLabel says what happened to the delivery, distinguishing the
// poll-only project — which has no webhook BY DECLARATION — from one whose
// provisioning did nothing.
func webhookLabel(report *sdlcSDK.OnboardingReport) string {
	switch {
	case report.WebhookEnsured:
		return "provisioned → " + report.WebhookURL
	case report.WebhookURL == "":
		return "- (poll-only: the binding names no webhook secret)"
	default:
		return "not provisioned"
	}
}

func formatSDLCConformance(report *sdlcSDK.ConformanceReport, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(report)
	default:
		return writeConformanceTable(report, w)
	}
}

// writeConformanceTable prints every check's verdict and then every finding
// with its remedy.
//
// THE CHECKS ARE PRINTED EVEN WHEN THEY PASS. A report that listed only
// failures could not be told apart from one whose checks never ran, which is
// exactly the confusion an unwired check creates.
func writeConformanceTable(report *sdlcSDK.ConformanceReport, w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Project:\t%s\n", report.ProjectID)
	fmt.Fprintf(tw, "Repository:\t%s\n", orDash(report.Repo))
	fmt.Fprintf(tw, "Conformant:\t%s\n", conformantWord(report.Conformant))
	for _, check := range report.Checks {
		fmt.Fprintf(tw, "  %s\t%s\n", check.Name, check.Status)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(report.Findings) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\nFindings (%d):\n", len(report.Findings)); err != nil {
		return err
	}
	ftw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, finding := range report.Findings {
		fmt.Fprintf(ftw, "  %s\t%s\n", finding.Class, orDash(finding.Subject))
		fmt.Fprintf(ftw, "    what\t%s\n", finding.Detail)
		fmt.Fprintf(ftw, "    fix\t%s\n", finding.Remedy)
	}
	return ftw.Flush()
}

func conformantWord(conformant bool) string {
	if conformant {
		return "yes"
	}
	return "NO"
}

func formatSDLCBinding(binding *sdlcSDK.ProjectBinding, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(binding)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(binding)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Project:\t%s\n", binding.ProjectID)
		fmt.Fprintf(tw, "Forge:\t%s %s/%s (%s)\n", binding.Forge.Type, binding.Forge.Owner, binding.Forge.Repo, orDash(binding.Forge.BaseURL))
		fmt.Fprintf(tw, "Trigger label:\t%s\n", orDash(binding.Forge.TriggerLabel))
		fmt.Fprintf(tw, "Strategy:\t%s\n", orDash(binding.Strategy))
		fmt.Fprintf(tw, "Worker:\t%s\n", workerLabel(binding.Worker))
		fmt.Fprintf(tw, "Worker prompt append:\t%s\n", promptAppendLabel(binding.WorkerPromptAppend))
		fmt.Fprintf(tw, "Verification:\t%s\n", orDash(strings.Join(binding.Verification, "; ")))
		fmt.Fprintf(tw, "Review:\t%d approval(s), independent=%v, green-checks=%v\n",
			binding.Review.RequiredApprovals, binding.Review.RequireIndependentReviewer, binding.Review.RequireGreenChecks)
		fmt.Fprintf(tw, "Triage:\t%s\n", triageLabel(binding.Triage, binding.Worker))
		fmt.Fprintf(tw, "Triage prompt append:\t%s\n", promptAppendLabel(binding.TriagePromptAppend))
		fmt.Fprintf(tw, "Triage may close:\t%s\n", yesNo(binding.TriageMayClose))
		fmt.Fprintf(tw, "Reviewer:\t%s\n", reviewerLabel(binding.Reviewer))
		fmt.Fprintf(tw, "Reviewer alternates:\t%s\n", alternatesLabel(binding.Reviewers))
		fmt.Fprintf(tw, "Review rounds:\t%s\n", reviewRoundsLabel(binding.MaxReviewRounds))
		fmt.Fprintf(tw, "Merge train:\t%s\n", mergeTrainLabel(binding.MergeTrain, binding.Reviewer, binding.Reviewers))
		fmt.Fprintf(tw, "Issue clustering:\t%s\n", clusteringLabel(binding.Clustering))
		fmt.Fprintf(tw, "Discovery:\t%s\n", discoveryLabel(binding.Discovery))
		fmt.Fprintf(tw, "Rulers:\t%s\n", rulersLabel(binding))
		fmt.Fprintf(tw, "Webhook:\t%s\n", boolWord(binding.Forge.WebhookSecretRef != ""))
		fmt.Fprintf(tw, "SSH host key:\t%s\n", sshHostKeyLabel(binding.Forge.SSHHostKey))
		if !binding.UpdatedAt.IsZero() {
			fmt.Fprintf(tw, "Updated:\t%s\n", binding.UpdatedAt.Format(time.RFC3339))
		}
		return tw.Flush()
	}
}

// promptAppendLabel sizes a slot's appended prompt material rather than
// printing it: it may run to kilobytes, and the JSON and YAML formats carry
// the text itself.
func promptAppendLabel(text string) string {
	if strings.TrimSpace(text) == "" {
		return "-"
	}
	return strconv.Itoa(len(text)) + " bytes"
}

func formatSDLCDelivery(delivery *sdlcSDK.Delivery, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(delivery)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(delivery)
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Task:\t%s (attempt %d)\n", delivery.TaskID, delivery.Attempt)
		fmt.Fprintf(tw, "Branch:\t%s → %s\n", delivery.Branch, orDash(delivery.Base))
		fmt.Fprintf(tw, "Head:\t%s\n", orDash(delivery.HeadSHA))
		if delivery.PullRequest.Number > 0 {
			fmt.Fprintf(tw, "Pull request:\t#%d %s (%s)\n", delivery.PullRequest.Number, orDash(delivery.PullRequest.URL), delivery.PullRequest.State)
		} else {
			fmt.Fprintf(tw, "Pull request:\t-\n")
		}
		fmt.Fprintf(tw, "Reviews:\t%s\n", strconv.Itoa(len(delivery.Reviews)))
		for _, review := range delivery.Reviews {
			fmt.Fprintf(tw, "  %s\t%s on %s (round %d)\n", review.Reviewer, review.Verdict, review.HeadSHA, review.Round)
		}
		fmt.Fprintf(tw, "Checks:\t%s\n", strconv.Itoa(len(delivery.Checks)))
		for _, check := range delivery.Checks {
			fmt.Fprintf(tw, "  %s\t%s\n", check.Name, check.Status)
		}
		return tw.Flush()
	}
}

func formatSDLCUnbound(projectID, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	switch outputFmt {
	case cli.OutputFormatJSON:
		return json.NewEncoder(w).Encode(map[string]any{"project_id": projectID, "unbound": true})
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(map[string]any{"project_id": projectID, "unbound": true})
	default:
		_, err := fmt.Fprintf(w, "Unbound project %s\n", projectID)
		return err
	}
}

func boolWord(b bool) string {
	if b {
		return "configured"
	}
	return "-"
}

// sshHostKeyLabel renders a pinned forge host key as its type and comment
// only — "ssh-ed25519 (forge.example.com)". The key itself is public, but a
// full base64 blob in a status table is noise nobody reads, and the two things
// an operator checks are that one is pinned at all and which algorithm it is.
func sshHostKeyLabel(hostKey string) string {
	fields := strings.Fields(hostKey)
	if len(fields) < 2 {
		return "-"
	}
	if len(fields) > 2 {
		return fields[0] + " (" + strings.Join(fields[2:], " ") + ")"
	}
	return fields[0]
}

// workerLabel renders the binding's worker: the Agent or pool it references.
func workerLabel(worker agentsSDK.AgentRef) string {
	if worker.IsZero() {
		return "-"
	}
	return worker.String()
}

// reviewRoundsLabel names the configured bound, saying so when the binding
// leaves it to the module's default.
// yesNo renders a plain boolean. It is not boolWord, which answers
// "configured"/"not configured" — right for a webhook that either exists or
// does not, and wrong for a permission, where "configured" reads as "somebody
// set this field" rather than "this project may close issues".
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// triageLabel renders the triage row, saying plainly when it falls back to the
// worker — a project paying a code-writing model to read issues should be able
// to see that it is.
func triageLabel(triage, worker agentsSDK.AgentRef) string {
	if triage.IsZero() {
		return workerLabel(worker) + " (the node's default triage agent where defined, else the worker)"
	}
	return workerLabel(triage)
}

// reviewerLabel renders the reviewer row of the binding table.
//
// AN UNCONFIGURED REVIEWER IS NAMED, NOT LEFT BLANK. A project with no
// reviewer is never dispatched for review and never swept for one, silently —
// so "none (not self-reviewed)" is the one line that tells an operator why a
// green pull request is sitting there, and a bare dash would read as a
// cosmetic gap rather than the reason.
func reviewerLabel(r sdlcSDK.ReviewerConfig) string {
	if strings.TrimSpace(r.Agent) == "" {
		return "none (not self-reviewed)"
	}
	label := "agent:" + r.Agent
	if r.Login != "" {
		label += " as " + r.Login
	}
	return label
}

// discoveryLabel renders the discovery row, naming the default explicitly.
func discoveryLabel(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "both (default)"
	}
	return mode
}

// mergeTrainLabel renders the merge-train row. It says why a train that is
// not disabled is still off: a train's pull request lands only on a verdict
// from a reviewer profile that counts toward merge, so a project with none
// merges one change at a time.
func mergeTrainLabel(policy sdlcSDK.MergeTrainPolicy, reviewer sdlcSDK.ReviewerConfig, alternates []sdlcSDK.ReviewerConfig) string {
	switch {
	case policy.Disabled:
		return "off"
	case !hasCountingReviewer(reviewer, alternates):
		return "off (no reviewer profile that counts toward merge to review a train)"
	case policy.MaxSize <= 0:
		return "on, default size"
	default:
		return "on, up to " + strconv.Itoa(policy.MaxSize) + " change(s)"
	}
}

// hasCountingReviewer reports whether the binding carries a reviewer profile
// whose verdict counts toward merge: a configured default counts unless it
// says otherwise, an alternate only when it says so.
func hasCountingReviewer(reviewer sdlcSDK.ReviewerConfig, alternates []sdlcSDK.ReviewerConfig) bool {
	if strings.TrimSpace(reviewer.Agent) == "" {
		return false
	}
	if reviewer.CountsTowardMerge == nil || *reviewer.CountsTowardMerge {
		return true
	}
	for _, alternate := range alternates {
		if alternate.CountsTowardMerge != nil && *alternate.CountsTowardMerge {
			return true
		}
	}
	return false
}

// clusteringLabel renders the issue-clustering row.
func clusteringLabel(policy sdlcSDK.ClusteringPolicy) string {
	switch {
	case policy.Disabled:
		return "off"
	case policy.MaxSize <= 0:
		return "on, default size"
	default:
		return "on, up to " + strconv.Itoa(policy.MaxSize) + " issue(s)"
	}
}

func reviewRoundsLabel(rounds int) string {
	if rounds <= 0 {
		return "default"
	}
	return strconv.Itoa(rounds)
}
