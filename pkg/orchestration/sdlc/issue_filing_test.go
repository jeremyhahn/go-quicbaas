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
	"reflect"
	"strings"
	"testing"
)

// filingRepo is the repository every filing test files on.
var filingRepo = RepoRef{Owner: fixtureOwner, Repo: fixtureRepo}

func TestIssueFilingValidate_AcceptsAWellFormedFiling(t *testing.T) {
	filing := IssueFiling{Title: "Split: the codec half", Body: "Details.", Labels: []string{"task:ready"}, Parent: 12}
	if err := filing.Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
}

func TestIssueFilingValidate_RefusesBadInput(t *testing.T) {
	cases := map[string]struct {
		filing IssueFiling
		field  string
	}{
		"no title":          {IssueFiling{Title: "  "}, "title"},
		"an overlong title": {IssueFiling{Title: strings.Repeat("t", maxIssueTitleLength+1)}, "title"},
		"a two-line title":  {IssueFiling{Title: "one\ntwo"}, "title"},
		"a negative parent": {IssueFiling{Title: "t", Parent: -1}, "parent"},
		"a blank label":     {IssueFiling{Title: "t", Labels: []string{" "}}, "labels[0]"},
		"a label twice":     {IssueFiling{Title: "t", Labels: []string{"task:ready", "Task:Ready"}}, "labels[1]"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var invalid *InvalidInputError
			if err := tc.filing.Validate(); !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("Validate = %v, want InvalidInputError on %s", err, tc.field)
			}
		})
	}
}

func TestIssueFilingSpec_RendersTheParentReference(t *testing.T) {
	cases := map[string]struct {
		filing IssueFiling
		body   string
	}{
		"no parent keeps the body":         {IssueFiling{Title: " T ", Body: " body "}, "body"},
		"a parent is appended":             {IssueFiling{Title: "T", Body: "body", Parent: 7}, "body\n\nParent: #7"},
		"a parent alone is the whole body": {IssueFiling{Title: "T", Parent: 7}, "Parent: #7"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec := tc.filing.Spec()
			if spec.Title != "T" || spec.Body != tc.body {
				t.Fatalf("Spec = %+v, want title T and body %q", spec, tc.body)
			}
		})
	}
	if labels := (IssueFiling{Title: "T", Labels: []string{" task:ready "}}).Spec().Labels; !reflect.DeepEqual(labels, []string{"task:ready"}) {
		t.Fatalf("labels = %v, want trimmed [task:ready]", labels)
	}
}

func TestFileIssue_FilesThroughTheForge(t *testing.T) {
	forge := newFakeForge()
	forge.putIssue(12, "the parent")

	issue, err := FileIssue(context.Background(), forge, filingRepo, IssueFiling{
		Title: "Follow-up", Body: "Do the rest.", Labels: []string{"task:ready"}, Parent: 12,
	})
	if err != nil {
		t.Fatalf("FileIssue: %v", err)
	}
	if issue.Number != fakeFirstFiledIssue || issue.Title != "Follow-up" {
		t.Fatalf("issue = %+v, want the filed follow-up", issue)
	}
	if len(forge.filed) != 1 || forge.filed[0].Body != "Do the rest.\n\nParent: #12" ||
		!reflect.DeepEqual(forge.filed[0].Labels, []string{"task:ready"}) {
		t.Fatalf("filed = %+v, want one issue linked to #12 and labelled task:ready", forge.filed)
	}
}

func TestFileIssue_RefusesBadInputBeforeWriting(t *testing.T) {
	forge := newFakeForge()
	cases := map[string]struct {
		forge  ForgeBackend
		filing IssueFiling
	}{
		"no forge":           {nil, IssueFiling{Title: "t"}},
		"no title":           {forge, IssueFiling{}},
		"a missing parent":   {forge, IssueFiling{Title: "t", Parent: 404}},
		"a negative parent":  {forge, IssueFiling{Title: "t", Parent: -2}},
		"a duplicated label": {forge, IssueFiling{Title: "t", Labels: []string{"a", "a"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := FileIssue(context.Background(), tc.forge, filingRepo, tc.filing); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("FileIssue = %v, want ErrInvalidInput", err)
			}
		})
	}
	if len(forge.filed) != 0 {
		t.Fatalf("filed = %+v, want nothing written for refused input", forge.filed)
	}
}

func TestFileIssue_SurfacesAForgeFailure(t *testing.T) {
	forge := newFakeForge()
	forge.createIssueErr = backendError("CreateIssue", errors.New("forge down"))
	if _, err := FileIssue(context.Background(), forge, filingRepo, IssueFiling{Title: "t"}); !errors.Is(err, ErrBackend) {
		t.Fatalf("FileIssue = %v, want ErrBackend", err)
	}
	forge = newFakeForge()
	forge.getIssueErr = backendError("GetIssue", errors.New("forge down"))
	if _, err := FileIssue(context.Background(), forge, filingRepo, IssueFiling{Title: "t", Parent: 3}); !errors.Is(err, ErrBackend) {
		t.Fatalf("FileIssue with an unreadable parent = %v, want ErrBackend", err)
	}
}

func TestCollaborators_ListsTheOwnerFirstThenSortedCollaborators(t *testing.T) {
	forge := newFakeForge()
	forge.collaborators = []ForgeUser{{Login: "zed"}, {Login: "Alice", FullName: "Alice A"}, {Login: fixtureOwner}, {Login: "alice"}}
	users, err := Collaborators(context.Background(), forge, filingRepo)
	if err != nil {
		t.Fatalf("Collaborators: %v", err)
	}
	want := []ForgeUser{{Login: fixtureOwner}, {Login: "Alice", FullName: "Alice A"}, {Login: "zed"}}
	if !reflect.DeepEqual(users, want) {
		t.Fatalf("users = %+v, want %+v", users, want)
	}
}

func TestCollaborators_RefusesAndSurfacesFailures(t *testing.T) {
	if _, err := Collaborators(context.Background(), nil, filingRepo); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Collaborators(nil forge) = %v, want ErrInvalidInput", err)
	}
	forge := newFakeForge()
	forge.collaboratorsErr = backendError("ListCollaborators", errors.New("forge down"))
	if _, err := Collaborators(context.Background(), forge, filingRepo); !errors.Is(err, ErrBackend) {
		t.Fatalf("Collaborators = %v, want ErrBackend", err)
	}
}

func TestCheckRulers(t *testing.T) {
	users := []ForgeUser{{Login: fixtureOwner}, {Login: "lead-agent"}}
	if err := CheckRulers([]string{"Lead-Agent", fixtureOwner}, users); err != nil {
		t.Fatalf("CheckRulers = %v, want the owner and a collaborator admitted", err)
	}
	if err := CheckRulers(nil, users); err != nil {
		t.Fatalf("CheckRulers(nil) = %v, want nil", err)
	}
	err := CheckRulers([]string{"lead-agent", "stranger"}, users)
	var refused *RulerNotCollaboratorError
	if !errors.As(err, &refused) || refused.Login != "stranger" || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CheckRulers = %v, want RulerNotCollaboratorError naming stranger", err)
	}
}
