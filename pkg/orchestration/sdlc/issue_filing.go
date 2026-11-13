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

package sdlc

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// =============================================================================
// Filing an issue — the operator's follow-up verb
// =============================================================================
//
// The lead splits an issue into follow-ups, and before this existed it could
// only do so on the forge directly, outside QuicBaaS: unaudited, and through a
// credential the factory does not hold. Filing goes through the binding's own
// forge adapter instead, so the issue is written with the project's token and
// the act lands on the platform audit trail.
//
// A PARENT IS A REFERENCE IN THE BODY, NOT A FORGE FEATURE. "Parent: #N" is
// written on every forge the module speaks to, and every one of them renders
// it as a cross-reference on the parent's timeline, so the link is visible
// from both ends without a second write that could fail on its own. It is
// deliberately not a dependency directive ("Blocked by:"): a follow-up split
// out of an issue does not wait for that issue to close.

// maxIssueTitleLength bounds a filed issue's title. Gitea refuses a title
// longer than 255 characters; refusing it here names the field instead of
// relaying a forge 422.
const maxIssueTitleLength = 255

// parentReferencePrefix opens the line that links a filed issue to its parent.
const parentReferencePrefix = "Parent: #"

// IssueSpec is what a forge adapter files: the rendered title, body and
// labels. The operator's request is an IssueFiling, which renders into this.
type IssueSpec struct {
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// IssueFiling is an operator's request to file an issue on a bound project's
// repository.
type IssueFiling struct {
	// Title is required.
	Title string `json:"title"`

	// Body is the issue's description. Optional.
	Body string `json:"body,omitempty"`

	// Labels are label names the repository already defines, e.g.
	// task:ready to hand the issue straight to the factory. Optional.
	Labels []string `json:"labels,omitempty"`

	// Parent is the number of the issue this one was split out of, in the
	// same repository. Zero files an issue with no parent.
	Parent int `json:"parent,omitempty"`
}

// Validate refuses a filing the forge would refuse or misread.
func (f IssueFiling) Validate() error {
	title := strings.TrimSpace(f.Title)
	switch {
	case title == "":
		return &InvalidInputError{Field: "title", Message: "an issue needs a title"}
	case len(title) > maxIssueTitleLength:
		return &InvalidInputError{Field: "title", Message: "must be at most " + strconv.Itoa(maxIssueTitleLength) + " characters"}
	case strings.ContainsAny(title, "\r\n"):
		return &InvalidInputError{Field: "title", Message: "must be a single line"}
	case f.Parent < 0:
		return &InvalidInputError{Field: "parent", Message: "must be a positive issue number, or zero for none"}
	}
	seen := make(map[string]struct{}, len(f.Labels))
	for i, label := range f.Labels {
		field := "labels[" + strconv.Itoa(i) + "]"
		if strings.TrimSpace(label) == "" {
			return &InvalidInputError{Field: field, Message: "must name a label"}
		}
		key := strings.ToLower(strings.TrimSpace(label))
		if _, dup := seen[key]; dup {
			return &InvalidInputError{Field: field, Message: strconv.Quote(label) + " is listed twice"}
		}
		seen[key] = struct{}{}
	}
	return nil
}

// Spec renders the filing into what the adapter writes: the title and labels
// trimmed, and the parent reference appended to the body.
func (f IssueFiling) Spec() IssueSpec {
	body := strings.TrimSpace(f.Body)
	if f.Parent > 0 {
		reference := parentReferencePrefix + strconv.Itoa(f.Parent)
		if body == "" {
			body = reference
		} else {
			body += "\n\n" + reference
		}
	}
	labels := make([]string, 0, len(f.Labels))
	for _, label := range f.Labels {
		labels = append(labels, strings.TrimSpace(label))
	}
	return IssueSpec{Title: strings.TrimSpace(f.Title), Body: body, Labels: labels}
}

// FileIssue validates the filing, checks its parent exists on repo and files
// it through forge.
//
// A PARENT THAT DOES NOT EXIST IS REFUSED, not written as a dangling
// reference: a follow-up pointing at nothing is a split whose origin is lost.
func FileIssue(ctx context.Context, forge ForgeBackend, repo RepoRef, filing IssueFiling) (*ForgeIssue, error) {
	if forge == nil {
		return nil, &InvalidInputError{Field: "forge", Message: "an issue is filed through the binding's forge adapter"}
	}
	if err := filing.Validate(); err != nil {
		return nil, err
	}
	if filing.Parent > 0 {
		if err := checkParentIssue(ctx, forge, repo, filing.Parent); err != nil {
			return nil, err
		}
	}
	return forge.CreateIssue(ctx, repo, filing.Spec())
}

// checkParentIssue refuses a parent number that names no issue on repo.
func checkParentIssue(ctx context.Context, forge ForgeBackend, repo RepoRef, number int) error {
	parent, err := forge.GetIssue(ctx, repo, number)
	if errors.Is(err, ErrNotFound) {
		return &InvalidInputError{Field: "parent", Message: "issue #" + strconv.Itoa(number) + " does not exist on " + repo.String()}
	}
	if err != nil {
		return err
	}
	if parent == nil {
		return &InvalidInputError{Field: "parent", Message: "issue #" + strconv.Itoa(number) + " does not exist on " + repo.String()}
	}
	return nil
}

// IssueFiledEvent is the audit record of one operator filing: who asked, on
// which project, what was asked for, and what the forge made of it.
type IssueFiledEvent struct {
	TenantID  string
	ProjectID string

	// Actor is the authenticated caller; empty when the call carried none.
	Actor string

	// Repo is the repository the issue was filed on.
	Repo RepoRef

	// Filing is what the operator asked for.
	Filing IssueFiling

	// Number is the filed issue's number; zero when filing failed.
	Number int

	// Err is why filing failed; nil on success.
	Err error
}

// IssueAuditor records every filing attempt on the audit trail, refused and
// failed ones included: an operator write that left no trace when it failed
// is one nobody can reconstruct.
type IssueAuditor interface {
	RecordIssueFiled(ctx context.Context, event IssueFiledEvent)
}
