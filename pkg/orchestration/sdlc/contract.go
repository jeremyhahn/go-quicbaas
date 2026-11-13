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

package sdlc

import (
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The worker charter and the task contract
// =============================================================================
//
// The charter is the worker role's system prompt in the canonical sdlc
// workflow: the standing rules every attempt runs under. The contract is the
// Task's description — the issue, its discussion and the repository — which
// the engine hands the worker as the work to do. Both are now markdown under
// promptdefaults/sdlc rather than constants of this file, so a person can
// change what a worker is told without a code change; what protects them is
// no longer that they are unreachable but that a change is validated,
// versioned and attributable. See prompt_set.go.

// WorkerCharter is the standing instruction set for an SDLC worker session.
//
// IT ENDS BY TEACHING THE WORKER THE STOP DECLARATION, and the specification
// in prompt_set.go requires that clause to name the marker and every code
// turn.go parses. A charter that named a code the parser did not know would
// be a worker stopping for a reason nothing could act on — which is the
// failure go-quicbaas#787 records, arriving through a different door — so
// the charter refuses to register rather than shipping.
//
// A FUNCTION RATHER THAN THE CONSTANT IT REPLACED, and deliberately not an
// exported variable: what a worker is told must not be reassignable by
// whoever imports this package. The editable copy lives in the prompt store,
// behind a validator and an audit trail, and reaches a worker as the job's
// resolved set — set here; see jobPrompts.
func WorkerCharter(set prompts.Renderer) string { return set.MustRender("worker_charter", nil) }

// IssueContractSpec is what the contract names for one issue.
type IssueContractSpec struct {
	Repo        RepoRef
	IssueNumber int
	Title       string
	Body        string
	Comments    []ForgeComment

	// Prompts words the contract: the project's resolved sdlc set. Nil is
	// the build's defaults.
	Prompts prompts.Renderer
}

// renderer is what the contract is worded by.
func (s IssueContractSpec) renderer() prompts.Renderer {
	if s.Prompts == nil {
		return sdlcPrompts
	}
	return s.Prompts
}

// Validate checks the spec names an issue.
func (s IssueContractSpec) Validate() error {
	if s.Repo.Owner == "" || s.Repo.Repo == "" {
		return &InvalidInputError{Field: "repo", Message: "a contract names the repository the issue is in"}
	}
	if s.IssueNumber <= 0 {
		return &InvalidInputError{Field: "issue_number", Message: "issues are numbered from one"}
	}
	if strings.TrimSpace(s.Title) == "" {
		return &InvalidInputError{Field: "title", Message: "an issue with no title states nothing to do"}
	}
	return nil
}

// IssueContract renders the Task description for a forge issue: the
// repository and issue, the title and body, and the discussion in order.
func IssueContract(spec IssueContractSpec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	set := spec.renderer()
	body := ""
	if trimmed := strings.TrimSpace(spec.Body); trimmed != "" {
		body = "\n" + trimmed + "\n"
	}
	var discussion strings.Builder
	rendered := 0
	for _, comment := range spec.Comments {
		if strings.TrimSpace(comment.Body) == "" {
			continue
		}
		// RENUMBERED, NOT INDEXED. A blank comment is skipped, and the ones
		// that follow it must still read 1, 2, 3 rather than carrying a gap
		// a worker would take for a comment it was not shown.
		rendered++
		discussion.WriteString(set.MustRender("issue_contract_comment", prompts.Vars{
			"index":       strconv.Itoa(rendered),
			"attribution": commentAttribution(comment),
			"body":        strings.TrimSpace(comment.Body),
		}))
		discussion.WriteString("\n\n")
	}
	if rendered == 0 {
		discussion.WriteString(noDiscussion + "\n")
	}
	return set.MustRender("issue_contract", prompts.Vars{
		"repo":         spec.Repo.String(),
		"issue_number": strconv.Itoa(spec.IssueNumber),
		"title":        strings.TrimSpace(spec.Title),
		"body":         body,
		"discussion":   discussion.String(),
	}), nil
}

// noDiscussion stands where an issue's discussion would be when it has none.
// It is a placeholder token rather than something said to the worker, so it
// is not a prompt of its own.
const noDiscussion = "(none)"

// ClusterContract is the description of a CLUSTERED task: the anchor issue's
// contract first and whole, then every member's, each verbatim.
//
// THE MEMBERS ARE CONTRACTS TOO, not a summary of them. The rule the anchor's
// contract follows — the worker reads what a person wrote, never a paraphrase —
// is the rule for every issue the task closes, because every one of them is
// closed by the pull request this worker opens.
func ClusterContract(set prompts.Renderer, anchor string, members []int, shared []string, memberContracts []string) (string, error) {
	if strings.TrimSpace(anchor) == "" {
		return "", &InvalidInputError{Field: "anchor_contract", Message: "a clustered task is its anchor issue first"}
	}
	if len(members) == 0 || len(members) != len(memberContracts) {
		return "", &InvalidInputError{Field: "member_contracts", Message: "every member issue carries its own contract"}
	}
	names := make([]string, 0, len(shared))
	for _, key := range shared {
		names = append(names, "`"+strings.TrimPrefix(strings.TrimPrefix(key, footprintFile), footprintPackage)+"`")
	}
	if len(names) == 0 {
		names = append(names, "the same code")
	}
	return set.MustRender("issue_contract_cluster", prompts.Vars{
		"anchor_contract":  strings.TrimSpace(anchor),
		"members":          issueList(members),
		"shared":           strings.Join(names, ", "),
		"member_contracts": strings.Join(memberContracts, "\n\n"),
	}), nil
}

// commentAttribution renders who wrote a comment and when.
func commentAttribution(comment ForgeComment) string {
	var parts []string
	if author := strings.TrimSpace(comment.Author); author != "" {
		parts = append(parts, "@"+author)
	}
	if !comment.Created.IsZero() {
		parts = append(parts, comment.Created.UTC().Format(time.RFC3339))
	}
	if len(parts) == 0 {
		return ""
	}
	return " — " + strings.Join(parts, ", ")
}
