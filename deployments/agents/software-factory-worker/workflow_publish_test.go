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

package softwarefactoryworker

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These tests run the worker-image workflow's "already published?" step
// (build-publish, id: existing) with a fake docker on PATH. Only a registry
// answer of "not found" may count as unpublished: any other lookup failure
// must fail the step, so the build and push steps never run and a published
// tag is never overwritten. No network, registry or engine is touched.

const workflowPath = "../../../.github/workflows/worker-image.yml"

type workflowFile struct {
	On struct {
		WorkflowDispatch struct {
			Inputs map[string]any `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Name        string            `yaml:"name"`
		If          string            `yaml:"if"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
			If   string `yaml:"if"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// loadWorkflow returns the raw worker-image workflow and its parse.
func loadWorkflow(t *testing.T) (string, workflowFile) {
	t.Helper()
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", workflowPath, err)
	}
	return string(raw), wf
}

// The workflow is one job that holds only GITHUB_TOKEN with contents: read and
// packages: write: no registry secret, and no job may write issues.
func TestWorkflowBuildPublish_OnlyJob(t *testing.T) {
	raw, wf := loadWorkflow(t)
	if len(wf.Jobs) != 1 {
		names := make([]string, 0, len(wf.Jobs))
		for name := range wf.Jobs {
			names = append(names, name)
		}
		t.Fatalf("jobs = %v, want exactly [build-publish]", names)
	}
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	wantIf := "${{ github.event_name == 'schedule' || github.event_name == 'workflow_dispatch' }}"
	if job.If != wantIf {
		t.Errorf("build-publish if = %q, want %q", job.If, wantIf)
	}
	wantPerms := map[string]string{"contents": "read", "packages": "write"}
	if len(job.Permissions) != len(wantPerms) {
		t.Errorf("build-publish permissions = %v, want %v", job.Permissions, wantPerms)
	}
	for k, v := range wantPerms {
		if job.Permissions[k] != v {
			t.Errorf("build-publish permissions = %v, want %v", job.Permissions, wantPerms)
			break
		}
	}
	inputs := wf.On.WorkflowDispatch.Inputs
	if len(inputs) != 2 || inputs["claude_code_version"] == nil || inputs["codex_version"] == nil {
		t.Errorf("workflow_dispatch inputs = %v, want only claude_code_version and codex_version", inputs)
	}
	for _, s := range []string{"REGISTRY_TOKEN", "REGISTRY_USER", "secrets["} {
		if strings.Contains(raw, s) {
			t.Errorf("%s contains %q", workflowPath, s)
		}
	}
	if regexp.MustCompile(`(?m)^\s*issues:`).MatchString(raw) {
		t.Errorf("%s has an issues: permission key", workflowPath)
	}
	for _, ref := range regexp.MustCompile(`secrets\.[A-Za-z0-9_]*`).FindAllString(raw, -1) {
		if ref != "secrets.GITHUB_TOKEN" {
			t.Errorf("%s references %s; only secrets.GITHUB_TOKEN is allowed", workflowPath, ref)
		}
	}
}

// Both secret scans run before the push, under the push's own condition, fail
// on any finding, and carry no severity filter, skip or ignore file.
func TestWorkflowBuildPublish_SecretScansGateThePush(t *testing.T) {
	_, wf := loadWorkflow(t)
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	idx := map[string]int{}
	push := -1
	for i, step := range job.Steps {
		if step.ID == "secret-scan" || step.ID == "layer-scan" {
			idx[step.ID] = i
		}
		if push < 0 && strings.Contains(step.Run, "make push-worker-image") {
			push = i
		}
	}
	if push < 0 {
		t.Fatalf("build-publish has no step that runs make push-worker-image")
	}
	for _, id := range []string{"secret-scan", "layer-scan"} {
		i, ok := idx[id]
		if !ok {
			t.Errorf("build-publish has no step with id: %s", id)
			continue
		}
		step := job.Steps[i]
		if i >= push {
			t.Errorf("step %s (#%d) does not come before the push step (#%d)", id, i, push)
		}
		if step.If != job.Steps[push].If {
			t.Errorf("step %s if = %q, want the push step's %q", id, step.If, job.Steps[push].If)
		}
		for _, s := range []string{"--scanners secret", "--exit-code 1"} {
			if !strings.Contains(step.Run, s) {
				t.Errorf("step %s run: lacks %q", id, s)
			}
		}
		for _, s := range []string{"--severity", "--skip-dirs", "--skip-files", "--secret-config", "--ignorefile"} {
			if strings.Contains(step.Run, s) {
				t.Errorf("step %s run: contains %q", id, s)
			}
		}
	}
	if i, ok := idx["layer-scan"]; ok {
		for _, s := range []string{"docker save", ".Layers[]"} {
			if !strings.Contains(job.Steps[i].Run, s) {
				t.Errorf("step layer-scan run: lacks %q", s)
			}
		}
	}
}

// existingStepScript returns the run: script of build-publish's step with
// id: existing.
func existingStepScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("parse %s: %v", workflowPath, err)
	}
	job, ok := wf.Jobs["build-publish"]
	if !ok {
		t.Fatalf("%s has no build-publish job", workflowPath)
	}
	for _, step := range job.Steps {
		if step.ID != "existing" {
			continue
		}
		if strings.TrimSpace(step.Run) == "" {
			t.Fatalf("build-publish step id: existing has no run: script")
		}
		return step.Run
	}
	t.Fatalf("build-publish has no step with id: existing")
	return ""
}

const fakeDockerScript = `#!/bin/sh
printf '%s' "$FAKE_DOCKER_STDOUT"
printf '%s' "$FAKE_DOCKER_STDERR" >&2
exit "$FAKE_DOCKER_EXIT"
`

func TestWorkflowBuildPublish_OnlyNotFoundCountsAsUnpublished(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script := existingStepScript(t)

	const (
		repo   = "ghcr.io/example/img"
		tag    = "1.0.0-cc1.2.3-cx4.5.6"
		digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)

	cases := []struct {
		name      string
		stdout    string
		stderr    string
		exit      string
		needJQ    bool
		wantOK    bool
		wantLines []string
	}{
		{
			name:      "not found",
			stderr:    "ERROR: " + repo + ":" + tag + ": not found\n",
			exit:      "1",
			wantOK:    true,
			wantLines: []string{"exists=false"},
		},
		{
			name:      "manifest unknown",
			stderr:    "MANIFEST_UNKNOWN: manifest unknown\n",
			exit:      "1",
			wantOK:    true,
			wantLines: []string{"exists=false"},
		},
		{
			name:   "server error",
			stderr: "unexpected status from HEAD request: 503 Service Unavailable\n",
			exit:   "1",
		},
		{
			name:   "timeout",
			stderr: "failed to do request: context deadline exceeded\n",
			exit:   "1",
		},
		{
			name:   "denied",
			stderr: "denied: denied\n",
			exit:   "1",
		},
		{
			name: "empty stderr",
			exit: "1",
		},
		{
			name:      "published",
			stdout:    `{"digest":"` + digest + `"}`,
			exit:      "0",
			needJQ:    true,
			wantOK:    true,
			wantLines: []string{"exists=true", "digest=" + digest},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needJQ {
				if _, err := exec.LookPath("jq"); err != nil {
					t.Skip("jq is not on PATH")
				}
			}
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDockerScript), 0o755); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "github_output")
			if err := os.WriteFile(output, nil, 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(bash, "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"REPO="+repo,
				"TAG="+tag,
				"GITHUB_OUTPUT="+output,
				"FAKE_DOCKER_STDOUT="+tc.stdout,
				"FAKE_DOCKER_STDERR="+tc.stderr,
				"FAKE_DOCKER_EXIT="+tc.exit,
			)
			combined, runErr := cmd.CombinedOutput()

			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(got)), "\n")

			if tc.wantOK {
				if runErr != nil {
					t.Fatalf("step failed: %v\n%s", runErr, combined)
				}
				for _, want := range tc.wantLines {
					found := false
					for _, line := range lines {
						if line == want {
							found = true
						}
					}
					if !found {
						t.Errorf("GITHUB_OUTPUT has no %q line:\n%s", want, got)
					}
				}
				return
			}

			if runErr == nil {
				t.Fatalf("step exited 0 on a lookup error, so the build and push would run:\n%s", combined)
			}
			for _, line := range lines {
				if strings.HasPrefix(line, "exists=") {
					t.Errorf("GITHUB_OUTPUT has %q after a lookup error:\n%s", line, got)
				}
			}
		})
	}
}
