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

package orchestration

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// TestWorkflowPrompts_IsRegisteredUnderItsKind proves the engine's set is
// reachable the way a resolver will reach it.
func TestWorkflowPrompts_IsRegisteredUnderItsKind(t *testing.T) {
	set, err := prompts.Embedded(SetWorkflow)
	if err != nil {
		t.Fatalf("the workflow set must be registered: %v", err)
	}
	if set != WorkflowPrompts() {
		t.Fatal("the registered set and the package's own handle must be one value")
	}
	if _, err := prompts.Embedded("workflow-that-does-not-exist"); !errors.Is(err, prompts.ErrUnknownSet) {
		t.Fatalf("an unregistered kind must be refused, got %v", err)
	}
}

// TestWorkflowPromptDefaults_EveryFileIsDeclared refuses dead markdown: a
// file a person can edit and nothing will ever send.
func TestWorkflowPromptDefaults_EveryFileIsDeclared(t *testing.T) {
	if unused := prompts.UnusedDefaults(workflowPrompts.Spec(), workflowDefaults); len(unused) > 0 {
		t.Fatalf("no specification reads these files: %v", unused)
	}
	for _, name := range workflowPrompts.Names() {
		if _, shipped := workflowDefaults[name]; !shipped {
			t.Fatalf("prompt %q is declared with no markdown behind it", name)
		}
	}
}

// TestWorkflowPromptDefaults_EveryPromptSaysWhatItIsFor keeps the editing
// surface usable: a prompt with no description is a text box with no label.
func TestWorkflowPromptDefaults_EveryPromptSaysWhatItIsFor(t *testing.T) {
	for _, prompt := range workflowPrompts.Spec().Prompts {
		if strings.TrimSpace(prompt.Description) == "" {
			t.Errorf("prompt %q has no description", prompt.Name)
		}
		for _, variable := range prompt.Variables {
			if strings.TrimSpace(variable.Description) == "" {
				t.Errorf("prompt %q variable %q has no description", prompt.Name, variable.Name)
			}
		}
	}
}

// TestWorkflowPromptDefaults_EveryFileEndsWithOneNewline holds the files to
// the rule the loader depends on.
func TestWorkflowPromptDefaults_EveryFileEndsWithOneNewline(t *testing.T) {
	entries, err := fs.ReadDir(workflowDefaultsFS, "promptdefaults/workflow")
	if err != nil {
		t.Fatalf("read the shipped defaults: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, prompts.FileExtension) {
			t.Fatalf("promptdefaults/workflow/%s is not markdown; the loader ignores it", name)
		}
		raw, readErr := fs.ReadFile(workflowDefaultsFS, "promptdefaults/workflow/"+name)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		content := string(raw)
		if !strings.HasSuffix(content, "\n") {
			t.Errorf("promptdefaults/workflow/%s does not end with a newline", name)
		}
		if strings.Contains(content, "\r") {
			t.Errorf("promptdefaults/workflow/%s carries a carriage return", name)
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("promptdefaults/workflow/%s is blank", name)
		}
	}
}

// TestWorkflowPrompts_RefuseABodyThatDroppedWhatTheEngineParses proves the
// markers on the machine-read prompts are enforced: a reviewer system prompt
// edited to stop asking for the approved field would produce replies
// parseReviewResponse cannot read.
func TestWorkflowPrompts_RefuseABodyThatDroppedWhatTheEngineParses(t *testing.T) {
	cases := []struct {
		prompt string
		drop   string
	}{
		{"reviewer_system", "new_work_items"},
		{"planner_system", "JSON"},
		{"validation_prompt", "passed"},
		{"resume_priming", "READY"},
	}
	for _, tc := range cases {
		t.Run(tc.prompt, func(t *testing.T) {
			spec, declared := workflowPrompts.Spec().Prompt(tc.prompt)
			if !declared {
				t.Fatalf("the set declares no %s", tc.prompt)
			}
			body, err := workflowPrompts.Body(tc.prompt)
			if err != nil {
				t.Fatalf("read %s: %v", tc.prompt, err)
			}
			if err := prompts.Validate(SetWorkflow, spec, body); err != nil {
				t.Fatalf("the shipped %s must pass its own contract: %v", tc.prompt, err)
			}
			edited := strings.ReplaceAll(body, tc.drop, "something_else")
			err = prompts.Validate(SetWorkflow, spec, edited)
			var missing *prompts.MissingMarkerError
			if !errors.As(err, &missing) {
				t.Fatalf("dropping %q from %s must be refused, got %v", tc.drop, tc.prompt, err)
			}
			if missing.Marker != tc.drop {
				t.Fatalf("the refusal must name %q, got %q", tc.drop, missing.Marker)
			}
		})
	}
}
