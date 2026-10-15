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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

var (
	sdlcOnboardRepo           string
	sdlcOnboardLike           string
	sdlcOnboardMaxConcurrency int
	sdlcOnboardPaused         bool
	sdlcOnboardPreflight      bool
)

// newSDLCProjectCommand is `quicbaas sdlc project`. It carries only the
// repository onboarding verb; the project record itself is managed with
// "quicbaas project".
func newSDLCProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Onboard a forge repository as a project shaped like an existing one",
	}
	onboard := &cobra.Command{
		Use:   "onboard --repo <repo> --like <project>",
		Short: "Create, provision and bind a repository's project from a template project",
		Long: `Onboard a forge repository the way an already bound project is onboarded:
create its project from the template's, create its webhook secret, provision
its webhook and labels, check its branch protection, signing key and forge
token, and bind it with the template's binding retargeted at the repository.

Idempotent — every step checks before it acts and reports "created",
"unchanged" or "finding", so rerunning after a repair picks up where the last
run stopped. Nothing is imported: a missing forge token or SSH signing key is a
finding naming its remedy, never a prompt. Exits non-zero when any step ends
in a finding.

With --preflight the project binds paused and its verification commands run
once on the default branch in the worker image, with no agent turn. Autonomy
switches on only when every command passes; a red preflight is a finding that
names each failing command and its failing lines.

Examples:
  quicbaas sdlc project onboard --repo go-truststrap --like go-quicbaas
  quicbaas sdlc project onboard --repo go-truststrap --like go-quicbaas --max-concurrency 2 --paused
  quicbaas sdlc project onboard --repo go-truststrap --like go-quicbaas --preflight`,
		Args: cobra.NoArgs,
		RunE: runSDLCProjectOnboard,
	}
	onboard.Flags().StringVar(&sdlcOnboardRepo, "repo", "", "Forge repository to onboard (required)")
	onboard.Flags().StringVar(&sdlcOnboardLike, "like", "", "Bound project to copy the project and binding from (required)")
	onboard.Flags().IntVar(&sdlcOnboardMaxConcurrency, "max-concurrency", 0, "The new binding's concurrency cap (0 keeps the template's)")
	onboard.Flags().BoolVar(&sdlcOnboardPaused, "paused", false, "Bind without switching the project's autonomy on")
	onboard.Flags().BoolVar(&sdlcOnboardPreflight, "preflight", false, "Run the verification commands once on the default branch; switch autonomy on only when they pass")
	cobra.CheckErr(onboard.MarkFlagRequired("repo"))
	cobra.CheckErr(onboard.MarkFlagRequired("like"))
	cmd.AddCommand(onboard)
	return cmd
}

func runSDLCProjectOnboard(_ *cobra.Command, _ []string) error {
	client, err := sdlcClientFactory()
	if err != nil {
		return formatError(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()
	report, err := client.OnboardRepo(ctx, &sdlcSDK.RepoOnboardRequest{
		TenantID:       globalTenantID,
		Repo:           sdlcOnboardRepo,
		Like:           sdlcOnboardLike,
		MaxConcurrency: sdlcOnboardMaxConcurrency,
		Paused:         sdlcOnboardPaused,
		Preflight:      sdlcOnboardPreflight,
	})
	if err != nil {
		return formatError(err)
	}
	if err := formatSDLCRepoOnboarding(report, sdlcOutputFmt, os.Stdout); err != nil {
		return err
	}
	if findings := repoOnboardingFindings(report); findings > 0 {
		return &SDLCRepoNotOnboardedError{Repo: sdlcOnboardRepo, Findings: findings}
	}
	return nil
}

// repoOnboardingFindings counts the steps that ended in a finding.
func repoOnboardingFindings(report *sdlcSDK.RepoOnboardingReport) int {
	findings := 0
	for _, step := range report.Steps {
		if step.Outcome == sdlcSDK.OnboardFinding {
			findings++
		}
	}
	return findings
}

// SDLCRepoNotOnboardedError reports a repository whose onboarding stopped at
// one or more findings, so the command exits non-zero after printing them.
type SDLCRepoNotOnboardedError struct {
	Repo     string
	Findings int
}

// Error implements the error interface.
func (e *SDLCRepoNotOnboardedError) Error() string {
	return "repository " + e.Repo + " is not fully onboarded: " + strconv.Itoa(e.Findings) + " finding(s) above"
}

func formatSDLCRepoOnboarding(report *sdlcSDK.RepoOnboardingReport, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Repository:\t%s\n", orDash(report.Repo))
		fmt.Fprintf(tw, "Project:\t%s\n\n", orDash(report.ProjectID))
		fmt.Fprintln(tw, "STEP\tOUTCOME\tDETAIL\tREMEDY")
		for _, step := range report.Steps {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", step.Step, step.Outcome, orDash(step.Detail), orDash(step.Remedy))
		}
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
